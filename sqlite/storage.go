package sqlite

import (
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/nidorx/sqlog"

	_ "github.com/mattn/go-sqlite3"
)

type Config struct {
	Dir           string            // Directory for the database files (default "./logs")
	Prefix        string            // Prefix for the database name (default "sqlog")
	SQLiteOptions map[string]string // SQLite connection string options (https://github.com/mattn/go-sqlite3#connection-string)

	// Allows defining a custom expression processor.
	ExprBuilder func(expression string) (*Expr, error)

	// Allows the database to accept older logs.
	// Useful for log migration or receiving delayed logs from integrations.
	// (Default: 3600 seconds = 1 hour)
	//
	// @TODO: Implement a solution to enable moving logs to the correct time range.
	MaxChunkAgeSec int64

	// When the current log file reaches this size (in MB), it will be archived.
	// (Default: 20MB).
	MaxFilesizeMB int32

	// The total size of all archived files. Once this cap is exceeded,
	// the oldest archive files will be deleted asynchronously.
	// (Default: 1000MB).
	TotalSizeCapMB int32

	// The maximum number of databases that can be opened simultaneously.
	MaxOpenedDB int32

	// The maximum number of goroutines for scheduled task processing.
	// (Default: 200).
	MaxRunningTasks int32

	// Time (in seconds) before closing idle databases.
	// (Default: 30 seconds).
	CloseIdleSec int64

	// Interval (in seconds) for storage maintenance checks.
	// (Default: 5 seconds).
	IntervalSizeCheckSec int32

	// Interval (in milliseconds) for scheduled task maintenance.
	// (Default: 100ms).
	IntervalScheduledTasksMs int32

	// Interval (in seconds) to execute a WAL checkpoint.
	// Set to 0 or less to disable.
	//
	// See https://www.sqlite.org/wal.html#ckpt
	IntervalWalCheckpointSec int32

	// WAL checkpoint modes: PASSIVE, FULL, RESTART, or TRUNCATE.
	// (Default: TRUNCATE).
	//
	// See https://www.sqlite.org/wal.html#ckpt
	WalCheckpointMode string
}

// Storage represents a connection to a SQLite database.
// Reference: https://ferrous-systems.com/blog/lock-free-ring-buffer/
type storage struct {
	sqlog.Storage
	sqlog.StorageWithApi
	mu             sync.Mutex
	dbs            []*storageDb // All databases managed by this storage
	liveDbs        []*storageDb // Currently active databases receiving logs
	config         *Config      //
	taskIdSeq      int32        // Last task ID
	taskMap        sync.Map     // Stores task execution outputs
	numActiveTasks int32        // Tracks the number of active goroutines for scheduled tasks
	quit           chan struct{}
	shutdown       chan struct{}
}

// New initializes a new storage instance.
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

	if config.ExprBuilder == nil {
		config.ExprBuilder = ExpBuilderFn
	}

	if config.MaxChunkAgeSec <= 0 {
		config.MaxChunkAgeSec = 3600
	}

	dbs, err := initDbs(config.Dir, config.Prefix)
	if err != nil {
		return nil, err
	}

	if len(dbs) == 0 {
		dbs = append(dbs, newDb(config.Dir, config.Prefix, time.Now(), config.MaxChunkAgeSec))
	}

	// Initialize the active database (live)
	live := dbs[len(dbs)-1]
	if err := live.connect(config.SQLiteOptions); err != nil {
		return nil, errors.Join(errors.New("[sqlog] unable to start live db"), err)
	}
	live.live = true

	s := &storage{
		config:   config,
		dbs:      dbs,
		liveDbs:  []*storageDb{live},
		quit:     make(chan struct{}),
		shutdown: make(chan struct{}),
	}

	go s.routineSizeCheck()
	go s.routineScheduledTasks()

	if s.config.IntervalWalCheckpointSec > 0 {
		config.WalCheckpointMode = strings.ToUpper(config.WalCheckpointMode)
		switch config.WalCheckpointMode {
		case "PASSIVE", "FULL", "RESTART", "TRUNCATE":
		default:
			config.WalCheckpointMode = "TRUNCATE"
		}

		go s.routineWalCheckpoint()
	}

	return s, nil
}

// Flush saves the chunk records to the current live database.
func (s *storage) Flush(chunk *sqlog.Chunk) error {
	var (
		db         *storageDb
		epochStart = chunk.First()
		epochEnd   = chunk.Last()
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
		// Should not happen
		return errors.New("db is closed")
	}

	return db.flush(chunk)
}

// Close closes all databases and cleans up.
func (s *storage) Close() error {
	for _, db := range s.dbs {
		db.close()
	}
	return nil
}
