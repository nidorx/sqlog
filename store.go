package litelog

import (
	"bytes"
	"database/sql"
	"math"
	"os"
	"path"

	_ "github.com/mattn/go-sqlite3"
)

var (
	// https://github.com/xerial/sqlite-jdbc/blob/master/Usage.md#configure-connections
	// https://phiresky.github.io/blog/2020/sqlite-performance-tuning/
	// http://sqlite.1065341.n5.nabble.com/In-memory-only-WAL-file-td101283.html
	// https://stackoverflow.com/questions/1711631/improve-insert-per-second-performance-of-sqlite
	// https://sqlite.org/pragma.html#pragma_journal_mode
	// https://wiki.mozilla.org/Performance/Avoid_SQLite_In_Your_Next_Firefox_Feature
	// https://www.deconstructconf.com/2019/dan-luu-files
	SQLiteOptionsDefault = map[string]string{
		"_journal":    "WAL",
		"_cache_size": "409600",
	}

	sqlCreateTable = `CREATE TABLE IF NOT EXISTS entries (
		epoch_secs LONG,
		nanos INTEGER,
		level INTEGER,
		content BLOB
	)`

	sqlCreateIndex = `CREATE INDEX IF NOT EXISTS entries_epoch_desc ON entries(epoch_secs DESC)`

	sqlCreateView = `CREATE VIEW IF NOT EXISTS entries_view AS 
		SELECT 
			datetime(epoch_secs, 'unixepoch', 'utc') AS timestamp_utc,
			datetime(epoch_secs, 'unixepoch', 'localtime') AS timestamp_local,
			epoch_secs,
			nanos, 
			level, 
			content
		FROM entries`

	sqlInsert       = []byte(`INSERT INTO entries(epoch_secs, nanos, level, content) VALUES `)
	sqlInsertValues = []byte(`(?,?,?,?)`)
)

// store connection to a sqlite database
// https://ferrous-systems.com/blog/lock-free-ring-buffer/
type store struct {
	db           *sql.DB
	maxParamSize int
}

func newStore(filepath string, batchInsertSize int, options map[string]string) (*store, error) {

	// arquivo único de log, por enquanto
	if err := os.MkdirAll(filepath, 0755); err != nil {
		return nil, err
	}

	file := path.Join(filepath, "/standalone.db")

	// file:test.db?cache=shared&mode=memory
	connString := "file:" + file
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

	// sql.Register("sqlite3_with_go_func", &sqlite3.SQLiteDriver{
	// 	ConnectHook: func(conn *sqlite3.SQLiteConn) error {
	// 		return conn.RegisterFunc("regexp", regex, true)
	// 	},
	// })

	db, err := sql.Open("sqlite3", connString)
	if err != nil {
		return nil, err
	}

	if _, err := db.Exec(sqlCreateTable); err != nil {
		return nil, err
	}

	if _, err := db.Exec(sqlCreateIndex); err != nil {
		return nil, err
	}

	if _, err := db.Exec(sqlCreateView); err != nil {
		return nil, err
	}

	return &store{
		db:           db,
		maxParamSize: int(math.Min(float64(batchInsertSize/4), 990)),
	}, nil
}

func (s *store) write(chunk *chunk) error {

	values := []any{}

	sql := bytes.NewBuffer(make([]byte, 0, 1952))
	sql.Write(sqlInsert)

	for i, e := range chunk.list() {
		if i > 0 {
			sql.WriteByte(',')
		}
		sql.Write(sqlInsertValues)
		values = append(values, e.time.Unix(), e.time.Nanosecond(), e.level, e.content)
	}

	if tx, err := s.db.Begin(); err != nil {
		return err
	} else if stmt, err := tx.Prepare(sql.String()); err != nil {
		print(err.Error(), sql.String())
		tx.Rollback()
		return err
	} else if _, err = stmt.Exec(values...); err != nil {
		print(err.Error())
		stmt.Close()
		tx.Rollback()
		return err
	} else {
		stmt.Close()
		tx.Commit()
		return nil
	}
}

func (s *store) close() {
	defer s.db.Close()
	s.vacuum()
}

// vacuum the database.
func (s *store) vacuum() error {
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
