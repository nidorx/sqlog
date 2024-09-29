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
	"time"
)

var (

	// @TODO: https://antonz.org/json-virtual-columns/
	sqlCreateTable = `CREATE TABLE IF NOT EXISTS entries (
		epoch_secs LONG,
		nanos INTEGER,
		level INTEGER,
		content BLOB,
		
	)`

	sqlCreateIndex = `CREATE INDEX IF NOT EXISTS entries_epoch_desc ON entries(epoch_secs DESC)`

	sqlInsert       = []byte(`INSERT INTO entries(epoch_secs, nanos, level, content) VALUES `)
	sqlInsertValues = []byte(`(?,?,?,?)`)
)

type storageState uint

const (
	s_closed   storageState = iota // The database is closed
	s_loading                      // Initializing the database
	s_open                         // The database is open
	s_closing                      // Closing the database
	s_removing                     // Removing the database
)

type storageDb struct {
	mu         sync.Mutex
	file       string
	state      storageState
	epochStart int64
	epochEnd   int64
	size       int64
	db         *sql.DB
	last       time.Time // last time this storage was used (query, flush)
	// entries    int64
}

// ttl tempo decorrido desde a ultima utilização desse db
func (d *storageDb) ttl() time.Duration {
	return time.Since(d.last)
}

// https://til.simonwillison.net/sqlite/database-file-size
// SELECT page_count * page_size as size FROM pragma_page_count(), pragma_page_size();

// connect realiza a conexão com a base de dados
func (d *storageDb) connect(options map[string]string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.state == s_open {
		return nil
	}
	d.state = s_loading

	// file:test.db?cache=shared&mode=memory
	connString := "file:" + d.file
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
		d.state = s_closed
		return err
	}

	if _, err := db.Exec(sqlCreateTable); err != nil {
		db.Close()
		d.state = s_closed
		return err
	}

	if _, err := db.Exec(sqlCreateIndex); err != nil {
		db.Close()
		d.state = s_closed
		return err
	}
	d.db = db
	d.state = s_open
	return nil
}

// flush saves the chunk records to this database.
func (s *storageDb) flush(chunk *chunk) error {
	values := []any{}

	sql := bytes.NewBuffer(make([]byte, 0, 1952))
	sql.Write(sqlInsert)

	for i, e := range chunk.list() {
		if e == nil {
			continue
		}
		if i > 0 {
			sql.WriteByte(',')
		}
		sql.Write(sqlInsertValues)
		values = append(values, e.time.Unix(), e.time.Nanosecond(), e.level, e.content)
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
		return nil
	}
}

func newDb(dir, prefix string, start time.Time) *storageDb {

	epochStart := start.UTC().Unix()
	name := fmt.Sprintf("%s_%d.db", prefix, epochStart)

	return &storageDb{
		file:       path.Join(dir, name),
		size:       0, // live db will updated during execution
		state:      s_closed,
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

		epochs := strings.Split(strings.TrimPrefix(name, prefix), "_")

		epochStart, err = strconv.ParseInt(epochs[0], 10, 64)
		if err != nil {
			return errors.New("[sqlog] invalid database name [" + filepath + "]")
		}

		if len(epochs) > 1 {
			epochEnd, err = strconv.ParseInt(epochs[1], 10, 64)
			if err != nil {
				return errors.New("[sqlog] invalid database name [" + filepath + "]")
			}
		}

		dbs = append(dbs, &storageDb{
			file:       path.Join(dir, name),
			size:       info.Size(), // live db will updated during execution
			state:      s_closed,
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
