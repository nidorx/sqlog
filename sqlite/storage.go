package sqlite

import (
	"errors"
	"sqlog"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Config struct {
	Dir           string            // Database folder (default "./logs")
	Prefix        string            // Database name prefix (default "sqlog")
	SQLiteOptions map[string]string // https://github.com/mattn/go-sqlite3?tab=readme-ov-file#connection-string

	// Each time the current log file reaches MaxFilesize,
	// it will be archived (default 20).
	MaxFilesizeMB int32

	// The total size of all archive files. Oldest archives
	// are deleted asynchronously when the total size cap
	// is exceeded (default 1000).
	TotalSizeCapMB int32

	// Quantidade máxima de banco de dados abertos
	// simultaneamente
	MaxOpenedDB int32

	// Numero máximo de goroutines que serão disparados
	// para executar processamento agendado (default 200)
	MaxRunningTasks int32

	// Fecha banco de dados que estão inativos (default 30)
	CloseIdleSec int64

	// Intervalo de manutençao do storage em segundos (default 5)
	IntervalSizeCheckSec int32

	// Intervalo de manutenção das tarefas em milisegundos (default 100)
	IntervalScheduledTasksMs int32

	// Intervalo para executar o CHECKPOINT do WAL (default 9)
	// Defina valor <= 0 para desabilitar
	//
	// See https://www.sqlite.org/wal.html#ckpt
	IntervalWalCheckpointSec int32
}

// storage connection to a sqlite database
// https://ferrous-systems.com/blog/lock-free-ring-buffer/
type storage struct {
	sqlog.Storage
	sqlog.StorageWithApi
	mu        sync.Mutex
	dbs       []*storageDb // todos os banco de dados desse storage
	liveDbs   []*storageDb // os banco de dados que ainda estão salvando dados
	config    *Config      //
	taskIdSeq int32        // last task id
	taskMap   sync.Map     // saída da execução
	quit      chan struct{}
	shutdown  chan struct{}
}

func New(config *Config) (*storage, error) {

	if config == nil {
		config = &Config{}
	}

	if len(config.SQLiteOptions) == 0 {
		config.SQLiteOptions = StorageSQLiteOptions
	}

	if config.Dir == "" {
		config.Dir = "./logs"
	}

	config.Prefix = strings.TrimSpace(config.Prefix)
	if config.Prefix == "" {
		config.Prefix = "sqlog"
	} else {
		config.Prefix = strings.ToLower(strings.Join(strings.Fields(config.Prefix), "_"))
	}

	if config.MaxFilesizeMB <= 0 {
		config.MaxFilesizeMB = 20 // ~20MB
	}

	if config.TotalSizeCapMB <= 0 {
		config.TotalSizeCapMB = 1000 // ~1GB
	}

	if config.IntervalScheduledTasksMs <= 0 {
		config.IntervalScheduledTasksMs = 100
	}

	if config.MaxRunningTasks <= 0 {
		config.MaxRunningTasks = 500
	}

	if config.CloseIdleSec <= 0 {
		config.CloseIdleSec = 30
	}

	if config.IntervalSizeCheckSec <= 0 {
		config.IntervalSizeCheckSec = 5
	}

	dbs, err := initDbs(config.Dir, config.Prefix)
	if err != nil {
		return nil, err
	}

	if len(dbs) == 0 {
		dbs = append(dbs, newDb(config.Dir, config.Prefix, time.Now()))
	}

	// init live live
	live := dbs[len(dbs)-1]
	if err := live.connect(config.SQLiteOptions); err != nil {
		return nil, errors.Join(errors.New("[sqlog] unable to start live db"), err)
	}
	live.live = true

	s := &storage{
		config:  config,
		dbs:     dbs,
		liveDbs: []*storageDb{live},
	}

	go s.routineSizeCheck()
	go s.routineScheduledTasks()

	if s.config.IntervalWalCheckpointSec > 0 {
		go s.routineWalCheckpoint()
	}

	return s, nil
}

// Flush saves the chunk records to the current live database.
func (s *storage) Flush(chunk *sqlog.Chunk) error {

	var (
		db         *storageDb
		epochStart = chunk.First().Unix()
		epochEnd   = chunk.Last().Unix()
	)
	for _, d := range s.liveDbs {
		if d.epochStart <= epochStart && (d.epochEnd == 0 || d.epochEnd >= epochEnd) {
			db = d
			break
		}
	}
	if db == nil {
		db = s.liveDbs[len(s.liveDbs)-1]
	}

	if !db.isOpen() {
		// should not happen
		return errors.New("db is closed")
	}

	return db.flush(chunk)
}

func (s *storage) Close() error {
	for _, db := range s.dbs {
		db.close(true)
	}
	return nil
}
