package sqlog

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
)

var (

	// https://sqlite.org/forum/forum
	// https://www.powersync.com/blog/sqlite-optimizations-for-ultra-high-performance
	// https://phiresky.github.io/blog/2020/sqlite-performance-tuning/
	// https://stackoverflow.com/questions/1711631/improve-insert-per-second-performance-of-sqlite
	// https://sqlite.org/pragma.html#pragma_journal_mode
	// https://wiki.mozilla.org/Performance/Avoid_SQLite_In_Your_Next_Firefox_Feature
	// https://www.deconstructconf.com/2019/dan-luu-files
	StorageSQLiteOptions = map[string]string{
		"_journal_mode": "WAL",
		"_synchronous":  "NORMAL",
		"_cache_size":   "409600",
	}

	// @TODO: https://antonz.org/json-virtual-columns/
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
	db_closed   int32 = iota // The database is closed
	db_loading               // Initializing the database
	db_open                  // The database is open
	db_closing               // Closing the database
	db_removing              // Removing the database
)

type storageDb struct {
	mu             sync.Mutex
	file           string
	status         int32
	epochStart     int64
	epochEnd       int64
	size           int64
	db             *sql.DB
	live           bool     // É um banco de dados que está recebendo registro atualmente
	lastQueryEpoch int64    // last time this storage was used
	taskCount      int32    // last async id
	taskMap        sync.Map //
}

// schedule agenda a execuçao de uma query nessa instancia
func (s *storageDb) schedule(id int32, task *dbTask) {
	s.taskMap.Store(id, task)
	atomic.AddInt32(&s.taskCount, 1)
}

// cancel cancela um processamento asíncrono
func (s *storageDb) cancel(id int32) bool {
	_, loaded := s.taskMap.LoadAndDelete(id)
	return loaded
}

// tasks obtém o número de consultas agendadas para esse banco
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

func (s *storageDb) isOpen() bool {
	return atomic.LoadInt32(&s.status) == db_open
}

// lastSecs tempo decorrido desde a ultima utilização desse db
func (s *storageDb) lastQuerySec() int64 {
	return time.Now().Unix() - s.lastQueryEpoch
}

// updateSize
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

func (s *storageDb) checkpoint() {
	// s.db.Exec()
	// sqlite3.Check
	// numFrames, numFramesCheckpointed, err = c.db.Checkpoint(dbName, mode)
	// Checkpoint calls sqlite3_wal_checkpoint_v2 on the underlying connection.
	// switch mode {
	// default:
	// 	var buf [20]byte
	// 	return "SQLITE_CHECKPOINT_UNKNOWN(" + string(itoa(buf[:], int64(mode))) + ")"
	// case SQLITE_CHECKPOINT_PASSIVE:
	// 	return "SQLITE_CHECKPOINT_PASSIVE"
	// case SQLITE_CHECKPOINT_FULL:
	// 	return "SQLITE_CHECKPOINT_FULL"
	// case SQLITE_CHECKPOINT_RESTART:
	// 	return "SQLITE_CHECKPOINT_RESTART"
	// case SQLITE_CHECKPOINT_TRUNCATE:
	// 	return "SQLITE_CHECKPOINT_TRUNCATE"
	// }

	// var cDB *C.char
	// if dbName != "" {
	// 	// Docs say: "If parameter zDb is NULL or points to a zero length string",
	// 	// so they are equivalent here.
	// 	cDB = C.CString(dbName)
	// 	defer C.free(unsafe.Pointer(cDB))
	// }
	// var nLog, nCkpt C.int
	// res := C.sqlite3_wal_checkpoint_v2(db.db, cDB, C.int(mode), &nLog, &nCkpt)
	// return int(nLog), int(nCkpt), errCode(res)
}

func (s *storageDb) closeSafe() bool {
	if s.lastQuerySec() < 2 {
		return false
	}
	return s.close(false)
}

func (s *storageDb) remove() {
	if s.close(false) && atomic.CompareAndSwapInt32(&s.status, db_closed, db_removing) {
		if err := os.Remove(s.file); err != nil {
			slog.Warn(
				"[sqlog] error removing database",
				slog.String("file", s.file),
				slog.Any("error", err),
			)
		}
	}
}

func (s *storageDb) close(vacuum bool) bool {
	if atomic.CompareAndSwapInt32(&s.status, db_open, db_closing) {

		if vacuum {
			if err := s.vacuum(); err != nil {
				slog.Warn(
					"[sqlog] error vacuum database",
					slog.String("file", s.file),
					slog.Any("error", err),
				)
			}
		}

		s.db.Close()
		s.db = nil

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
	// _, err := s.db.Exec("VACUUM")
	// return err

	return nil
}

// connect realiza a conexão com a base de dados
func (s *storageDb) connect(options map[string]string) error {
	if atomic.CompareAndSwapInt32(&s.status, db_closed, db_loading) {

		// file:test.db?cache=shared&mode=memory
		connString := "file:" + s.file
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

// flush saves the chunk records to this database.
func (s *storageDb) flush(chunk *Chunk) error {
	values := []any{}

	sql := bytes.NewBuffer(make([]byte, 0, 1952))
	sql.Write(sqlInsert)

	epochEnd := s.epochEnd

	for i, e := range chunk.List() {
		if e == nil {
			continue
		}
		if i > 0 {
			sql.WriteByte(',')
		}
		sql.Write(sqlInsertValues)
		epoch := e.time.Unix()
		epochEnd = max(epochEnd, epoch)
		values = append(values, epoch, e.time.Nanosecond(), e.level, e.content)
	}

	if len(values) == 0 {
		// should never happen
		slog.Warn("[sqlog] trying to flush an empty chunk")
		return nil
	}

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
		atomic.StoreInt64(&s.epochEnd, epochEnd)
		return nil
	}
}

func newDb(dir, prefix string, start time.Time) *storageDb {

	epochStart := start.Unix()
	name := fmt.Sprintf("%s_%d.db", prefix, epochStart)

	return &storageDb{
		file:       path.Join(dir, name),
		size:       0, // live db will updated during execution
		status:     db_closed,
		epochStart: epochStart,
		epochEnd:   0,
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
			return errors.Join(errors.New("[sqlog] invalid database name ["+filepath+"]"), err)
		}

		if len(epochs) > 1 {
			epochEnd, err = strconv.ParseInt(epochs[1], 10, 64)
			if err != nil {
				return errors.Join(errors.New("[sqlog] invalid database name ["+filepath+"]"), err)
			}
		}

		dbs = append(dbs, &storageDb{
			file:       path.Join(dir, name),
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
		sort.SliceStable(dbs, func(i, j int) bool {
			return dbs[i].epochStart < dbs[j].epochStart
		})
	}

	return
}
