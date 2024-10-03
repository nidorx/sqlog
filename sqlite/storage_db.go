package sqlite

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nidorx/sqlog"
)

var (
	// SQLite performance optimizations and tips
	// https://sqlite.org/forum/forum
	// https://www.powersync.com/blog/sqlite-optimizations-for-ultra-high-performance
	// https://phiresky.github.io/blog/2020/sqlite-performance-tuning/
	// https://stackoverflow.com/questions/1711631/improve-insert-per-second-performance-of-sqlite
	// https://sqlite.org/pragma.html#pragma_journal_mode
	// https://wiki.mozilla.org/Performance/Avoid_SQLite_In_Your_Next_Firefox_Feature
	// https://www.deconstructconf.com/2019/dan-luu-files
	StorageSQLiteOptions = map[string]string{
		"_journal_mode": "WAL",    // Write-ahead logging for better performance
		"_synchronous":  "NORMAL", // Balance between performance and safety
		"_cache_size":   "409600", // 4MB
	}

	sqlCreateTable = `CREATE TABLE IF NOT EXISTS entries (
		epoch_secs LONG,
		nanos INTEGER,
		level INTEGER,
		content BLOB
	)`

	sqlCreateIndex = `CREATE INDEX IF NOT EXISTS entries_epoch_desc ON entries(epoch_secs DESC)`

	sqlInsert       = []byte(`INSERT INTO entries(epoch_secs, nanos, level, content) VALUES `)
	sqlInsertValues = []byte(`(?,?,?,?)`)
)

const (
	db_closed   int32 = iota // Database is closed
	db_loading               // Initializing the database
	db_open                  // Database is open
	db_closing               // Closing the database
	db_removing              // Removing the database
)

type storageDb struct {
	mu             sync.Mutex // Mutex for checkpoint and flush operations
	live           bool       // Indicates if the database is live and receiving logs
	size           int64      // Size of the database in bytes
	status         int32      // Connection status (closed, loading, open, closing, removing)
	epochStart     int64      // Epoch of the oldest entry in this database
	newEpochStart  int64      // When accepting an old log, adjust file name when closing the DB
	epochEnd       int64      // Epoch of the newest entry in this database
	lastQueryEpoch int64      // Last usage timestamp of this storage (query)
	maxChunkAgeSec int64      // Maximum allowed chunk age
	fileDir        string     // Directory of the database file
	filePath       string     // Path to the database file
	filePrefix     string     // Prefix for the database file name
	db             *sql.DB    // SQLite connection object
	taskCount      int32      // Number of scheduled tasks
	taskMap        sync.Map   // Map of scheduled tasks
}

// schedule schedules a query execution on this instance
func (s *storageDb) schedule(id int32, task *dbTask) {
	s.taskMap.Store(id, task)
	atomic.AddInt32(&s.taskCount, 1)
}

// cancel cancels an asynchronous process
func (s *storageDb) cancel(id int32) bool {
	_, loaded := s.taskMap.LoadAndDelete(id)
	if loaded {
		atomic.AddInt32(&s.taskCount, -1)
	}
	return loaded
}

// tasks returns the number of scheduled queries for this database
func (s *storageDb) tasks() int32 {
	return s.taskCount
}

// execute executa os proximos callbacks nesse banco de dados
func (s *storageDb) execute(f func(id int32, task *dbTask) bool) {
	s.taskMap.Range(func(key, value any) bool {
		atomic.AddInt32(&s.taskCount, -1)
		s.taskMap.Delete(key)

		id, isIdOk := key.(int32)
		task, isTaskOk := value.(*dbTask)
		if isIdOk && isTaskOk {
			return f(id, task)
		} else {
			return f(-1, nil)
		}
	})
}

// isOpen checks if the database is open
func (s *storageDb) isOpen() bool {
	return atomic.LoadInt32(&s.status) == db_open
}

// lastQuerySec returns the time elapsed since the last use of this database
func (s *storageDb) lastQuerySec() int64 {
	return time.Now().Unix() - s.lastQueryEpoch
}

// updateSize updates the size of the database
// https://til.simonwillison.net/sqlite/database-file-size
// https://www.powersync.com/blog/sqlite-optimizations-for-ultra-high-performance
func (s *storageDb) updateSize() error {
	stm, rows, err := s.query(`
		SELECT 
			page_count * page_size as total_size, 
			freelist_count * page_size as freelist_size 
		FROM  pragma_page_count(), pragma_freelist_count(), pragma_page_size()
	`, nil,
	)
	if err != nil {
		return err
	}
	defer stm.Close()
	defer rows.Close()

	var (
		totalSize    int64
		freelistSize int64
	)

	if rows.Next() {
		if err = rows.Scan(&totalSize, &freelistSize); err != nil {
			return err
		}
	}

	atomic.StoreInt64(&s.size, totalSize-freelistSize)
	return nil
}

// closeSafe checks if the database can be safely closed
func (s *storageDb) closeSafe() bool {
	if s.lastQuerySec() < 2 {
		return false
	}
	return s.close()
}

// remove deletes the database file
func (s *storageDb) remove() {
	if s.close() && atomic.CompareAndSwapInt32(&s.status, db_closed, db_removing) {
		if err := os.Remove(s.filePath); err != nil {
			slog.Warn(
				"[sqlog] error removing database",
				slog.String("file", s.filePath),
				slog.Any("error", err),
			)
		}
	}
}

// connect establishes the connection to the database
func (s *storageDb) connect(options map[string]string) error {
	if atomic.CompareAndSwapInt32(&s.status, db_closed, db_loading) {

		// file:test.db?cache=shared&mode=memory
		connString := "file:" + s.filePath
		if len(options) > 0 {
			connString += "?"
			i := 0
			for k, v := range options {
				if i > 0 {
					connString += "&"
				}
				connString += k + "=" + v
				i++
			}
		}

		db, err := sql.Open("sqlite3", connString)
		if err != nil {
			atomic.StoreInt32(&s.status, db_closed)
			return err
		}

		if _, err := db.Exec(sqlCreateTable); err != nil {
			db.Close()
			atomic.StoreInt32(&s.status, db_closed)
			return err
		}

		if _, err := db.Exec(sqlCreateIndex); err != nil {
			db.Close()
			atomic.StoreInt32(&s.status, db_closed)
			return err
		}

		s.db = db
		atomic.StoreInt64(&s.lastQueryEpoch, time.Now().Unix())
		atomic.StoreInt32(&s.status, db_open)
	}
	return nil
}

// query prepares and executes a query on the database
func (s *storageDb) query(sql string, args []any) (*sql.Stmt, *sql.Rows, error) {
	if s.db == nil {
		return nil, nil, errors.New("db is closed")
	}

	stm, err := s.db.Prepare(sql)
	if err != nil {
		return nil, nil, err
	}

	rows, err := stm.Query(args...)
	if err != nil {
		stm.Close()
		return nil, nil, err
	}

	atomic.StoreInt64(&s.lastQueryEpoch, time.Now().Unix())

	return stm, rows, nil
}

// flush saves the chunk records to this database
func (s *storageDb) flush(chunk *sqlog.Chunk) error {
	values := []any{}

	sql := bytes.NewBuffer(make([]byte, 0, 1952))
	sql.Write(sqlInsert)

	maxChunkAge := s.epochStart - s.maxChunkAgeSec

	for i, e := range chunk.List() {
		if e == nil {
			continue
		}
		if i > 0 {
			sql.WriteByte(',')
		}
		sql.Write(sqlInsertValues)
		epoch := e.Time.Unix()
		if epoch < maxChunkAge {
			continue
		}
		values = append(values, epoch, e.Time.Nanosecond(), e.Level, e.Content)
	}

	if len(values) == 0 {
		// should never happen
		slog.Warn("[sqlog] trying to flush an empty chunk")
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if tx, err := s.db.Begin(); err != nil {
		return err
	} else if stmt, err := tx.Prepare(sql.String()); err != nil {
		tx.Rollback()
		return err
	} else if _, err = stmt.Exec(values...); err != nil {
		stmt.Close()
		tx.Rollback()
		return err
	} else {
		stmt.Close()
		tx.Commit()

		s.epochEnd = max(chunk.Last(), s.epochEnd)
		if chunkEpochStart := chunk.First(); chunkEpochStart < s.newEpochStart {
			// db will renamed during close
			s.newEpochStart = chunkEpochStart
		}
		return nil
	}
}

// See https://www.sqlite.org/wal.html#ckpt
func (s *storageDb) checkpoint(mode string) {
	if s.isOpen() {
		s.mu.Lock()
		defer s.mu.Unlock()

		// "PASSIVE", "FULL", "RESTART", "TRUNCATE"
		// https://www.sqlite.org/pragma.html#pragma_wal_checkpoint
		s.db.Exec("PRAGMA wal_checkpoint(?)", mode)
	}
}

func (s *storageDb) close() bool {
	if atomic.CompareAndSwapInt32(&s.status, db_open, db_closing) {

		if s.live {
			if err := s.vacuum(); err != nil {
				slog.Warn(
					"[sqlog] error vacuum database",
					slog.String("path", s.filePath),
					slog.Any("error", err),
				)
			}
		}

		s.db.Close()
		s.db = nil

		if s.newEpochStart < s.epochStart {
			// need to rename DB
			newPath := path.Join(s.fileDir, fmt.Sprintf("%s_%d.db", s.filePrefix, s.newEpochStart))
			if err := os.Rename(s.filePath, newPath); err != nil {
				slog.Warn(
					"[sqlog] error renaming database",
					slog.String("path", s.filePath),
					slog.String("newpath", newPath),
					slog.Any("error", err),
				)
			}
		}

		atomic.StoreInt32(&s.status, db_closed)
	}
	return atomic.LoadInt32(&s.status) == db_closed
}

// vacuum the database.
func (s *storageDb) vacuum() error {
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
	_, err := s.db.Exec("VACUUM")
	return err
}

func newDb(dir, prefix string, start time.Time, maxChunkAgeSec int64) *storageDb {

	epochStart := start.Unix()
	name := fmt.Sprintf("%s_%d.db", prefix, epochStart)

	return &storageDb{
		fileDir:        dir,
		filePath:       path.Join(dir, name),
		filePrefix:     prefix,
		size:           0, // live db will updated during execution
		status:         db_closed,
		epochStart:     epochStart,
		newEpochStart:  epochStart,
		maxChunkAgeSec: maxChunkAgeSec,
		epochEnd:       0,
	}
}

func initDbs(dir, prefix string) (dbs []*storageDb, err error) {

	if err = os.MkdirAll(dir, 0755); err != nil {
		return
	}

	err = filepath.Walk(dir, func(filepath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		name := info.Name()
		if !strings.HasPrefix(name, prefix) || path.Ext(name) != ".db" {
			return nil
		}

		var (
			epochStart int64
			epochEnd   int64
		)

		epochs := strings.Split(strings.TrimSuffix(strings.TrimPrefix(name, prefix+"_"), ".db"), "_")

		epochStart, err = strconv.ParseInt(epochs[0], 10, 64)
		if err != nil {
			slog.Warn("[sqlog] invalid database name", slog.String("filepath", filepath), slog.Any("err", err))
			return nil
		}

		if len(epochs) > 1 {
			epochEnd, err = strconv.ParseInt(epochs[1], 10, 64)
			if err != nil {
				slog.Warn("[sqlog] invalid database name", slog.String("filepath", filepath), slog.Any("err", err))
				return nil
			}
		}

		dbs = append(dbs, &storageDb{
			filePath:   path.Join(dir, name),
			size:       info.Size(), // live db will updated during execution
			status:     db_closed,
			epochStart: epochStart,
			epochEnd:   epochEnd,
		})

		return nil
	})
	if err != nil {
		err = errors.Join(errors.New("[sqlog] error initializing the storage"), err)
	}

	if len(dbs) > 1 {
		sortDbs(dbs)
	}

	return
}

func sortDbs(dbs []*storageDb) {
	sort.SliceStable(dbs, func(i, j int) bool {
		ae := dbs[i].epochEnd
		be := dbs[j].epochEnd
		if ae == 0 && be == 0 {
			return dbs[i].epochStart < dbs[j].epochStart
		}

		if be == 0 {
			return true
		}

		if ae == 0 {
			return false
		}

		return ae > be
	})
}
