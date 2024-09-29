package sqlog

import (
	"database/sql"
	"errors"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// sql.Register("sqlite3_with_go_func", &sqlite3.SQLiteDriver{
// 	ConnectHook: func(conn *sqlite3.SQLiteConn) error {
// 		return conn.RegisterFunc("regexp", regex, true)
// 	},
// })

var (
	// https://phiresky.github.io/blog/2020/sqlite-performance-tuning/
	// https://stackoverflow.com/questions/1711631/improve-insert-per-second-performance-of-sqlite
	// https://sqlite.org/pragma.html#pragma_journal_mode
	// https://wiki.mozilla.org/Performance/Avoid_SQLite_In_Your_Next_Firefox_Feature
	// https://www.deconstructconf.com/2019/dan-luu-files
	StorageSQLiteOptions = map[string]string{
		"_journal":    "WAL",
		"_cache_size": "409600",
	}
)

type StorageConfig struct {
	Dir            string            // Database folder (default "./logs")
	Prefix         string            // Database name prefix (default "sqlog")
	SQLiteOptions  map[string]string // https://github.com/mattn/go-sqlite3?tab=readme-ov-file#connection-string
	MaxFilesizeMB  int64             // Each time the current log file reaches MaxFilesize, it will be archived (default 20).
	TotalSizeCapMB int64             // The total size of all archive files. Oldest archives are deleted asynchronously when the total size cap is exceeded (default 1000).
}

// storageImpl connection to a sqlite database
// https://ferrous-systems.com/blog/lock-free-ring-buffer/
type storageImpl struct {
	db       *sql.DB
	dbs      []*storageDb // todos os banco de dados desse storage
	live     []*storageDb // os banco de dados que ainda estão salvando dados
	config   *StorageConfig
	quit     chan struct{}
	shutdown chan struct{}
}

func newStorage(config *StorageConfig) (*storageImpl, error) {

	if config == nil {
		config = &StorageConfig{}
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

	// int(math.Min(float64(batchInsertSize/4), 990)),

	// file := path.Join(dir, "/standalone.db")

	// // file:test.db?cache=shared&mode=memory
	// connString := "file:" + file
	// if len(options) > 0 {
	// 	connString += "?"
	// 	i := 0
	// 	for k, v := range options {
	// 		if i > 0 {
	// 			connString += "&"
	// 		}
	// 		connString += k + "=" + v
	// 		i++
	// 	}
	// }

	// db, err := sql.Open("sqlite3", connString)
	// if err != nil {
	// 	return nil, err
	// }

	// if _, err := db.Exec(sqlCreateTable); err != nil {
	// 	return nil, err
	// }

	// if _, err := db.Exec(sqlCreateIndex); err != nil {
	// 	return nil, err
	// }

	s := &storageImpl{
		config: config,
		dbs:    dbs,
		live:   []*storageDb{live},
	}
	// db:           db,

	go s.loop()

	return s, nil
}

// loop tarefas de gerenciamento do storage
func (s *storageImpl) loop() {
	defer close(s.shutdown)

	wg := &sync.WaitGroup{}
	defer wg.Wait()

	tick := time.NewTicker(interval)
	defer tick.Stop()

	for {
		select {

		case <-tick.C:
			// fecha os banco de dados não usados (exeto o atual)
			// faz o arquivamento de banco de dados
			// executa consultas agendadas (quando os banco de dados estiverem disponíveis)
		case <-s.quit:
			return
		}
	}
}

// flush saves the chunk records to the current live database.
func (s *storageImpl) flush(chunk *chunk) error {

	var db *storageDb

	firstEpoch := chunk.first().Unix()
	lastEpoch := chunk.last().Unix()
	for _, d := range s.live {
		if d.epochStart >= firstEpoch && (d.epochEnd == 0 || d.epochEnd > lastEpoch) {
			db = d
			break
		}
	}
	if db == nil {
		db = s.live[len(s.live)-1]
	}

	switch db.state {
	case s_closed:
	case s_loading:
	case s_closing:
		if err := db.connect(s.config.SQLiteOptions); err != nil {
			return err
		}
	case s_removing:
		return nil
	}

	return db.flush(chunk)
}

func (s *storageImpl) close() {
	// defer s.db.Close()
	s.vacuum()
}

// vacuum the database.
func (s *storageImpl) vacuum() error {
	// Do a full vacuum of the live repository.  This
	// should be fairly fast as it's deliberately size constrained.

	// 1) maintain an in-memory operation-queue, so you can copy the DB when idle,
	// vacuum as long as necessary on the copy, and then switch to the vacuumed copy
	// after replaying the queue.
	// (SQLite allows only a single writer, so statement-replay is safe, unlike
	// concurrent-writer databases in some cases since you can't recreate the DB's
	// row-visibility logic)
	// https://news.ycombinator.com/item?id=23521079

	// Use dbstat to find out what fraction of the pages in a database are sequential
	// if there's a significant degree of fragmentation, then vacuum.
	// https://www.sqlite.org/dbstat.html
	// _, err := s.db.Exec("VACUUM")
	// return err

	return nil
}
