package sqlite

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"sync"
)

type mockDriver struct {
	mu    sync.Mutex
	conns map[string]*mockDriverConn
}

func (d *mockDriver) Open(dsn string) (driver.Conn, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if c, exists := d.conns[dsn]; exists {
		c.count++
		return c, nil
	}

	url, err := url.Parse(dsn)
	if err != nil {
		return nil, err
	}

	// simulate sqlite wal
	file, err := os.OpenFile(url.Opaque, os.O_CREATE|os.O_APPEND, 0755)
	if err != nil {
		return nil, err
	}

	wal, err := os.OpenFile(url.Opaque+"-wal", os.O_CREATE, 0755)
	if err != nil {
		return nil, err
	}

	d.conns[dsn] = &mockDriverConn{
		count:  1,
		dsn:    dsn,
		file:   file,
		wal:    wal,
		driver: d,
	}
	return d.conns[dsn], nil
}

type mockDriverConn struct {
	mu     sync.Mutex
	tx     *mockTx
	count  int
	dsn    string
	file   *os.File
	wal    *os.File
	driver *mockDriver
}

func (c *mockDriverConn) Prepare(query string) (stmt driver.Stmt, err error) {
	return &mockStmt{query: query, conn: c}, nil
}

func (c *mockDriverConn) Begin() (tx driver.Tx, err error) {
	c.mu.Lock()
	c.tx = &mockTx{conn: c}
	return c.tx, nil
}

func (c *mockDriverConn) Close() error {
	c.driver.mu.Lock()
	defer c.driver.mu.Unlock()
	c.count--
	if c.count == 0 {
		delete(c.driver.conns, c.dsn)
		c.wal.Close()
		c.file.Close()
	}
	return nil
}

type mockTxEntry struct {
	epoch   int64
	nanos   int64
	level   int64
	content []byte
}

// Tx is a transaction.
type mockTx struct {
	conn   *mockDriverConn
	commit []func() error
}

func (t *mockTx) Commit() error {
	defer t.conn.mu.Unlock()
	for _, fn := range t.commit {
		if err := fn(); err != nil {
			return err
		}
	}
	t.conn.tx = nil
	return nil
}

func (t *mockTx) Rollback() error {
	defer t.conn.mu.Unlock()
	t.conn.tx = nil
	return nil
}

// Stmt is a prepared statement. It is bound to a [Conn] and not
// used by multiple goroutines concurrently.
type mockStmt struct {
	conn  *mockDriverConn
	query string
}

func (s *mockStmt) Close() error {
	return nil
}

func (s *mockStmt) NumInput() int {
	return -1
}

func (s *mockStmt) Exec(args []driver.Value) (driver.Result, error) {
	if strings.Contains(s.query, "INSERT INTO entries") { // (epoch_secs, nanos, level, content)
		if s.conn.tx == nil {
			return nil, errors.New("transaction required")
		}

		id := int64(0)
		rows := int64(0)

		var entries []*mockTxEntry

		for i := 0; i < len(args); i++ {
			entries = append(entries, &mockTxEntry{
				epoch:   args[i].(int64),
				nanos:   args[i+1].(int64),
				level:   args[i+2].(int64),
				content: args[i+3].([]byte),
			})
			id++
			rows++
			i += 3
		}

		s.conn.tx.commit = append(s.conn.tx.commit, func() error {
			for _, entry := range entries {
				_, err := s.conn.wal.WriteString(fmt.Sprintf("%d,%d,%d", entry.epoch, entry.nanos, entry.level))
				if err != nil {
					return err
				}
				_, err = s.conn.wal.Write(entry.content)
				if err != nil {
					return err
				}
				_, err = s.conn.wal.WriteString("\n")
				if err != nil {
					return err
				}
			}
			return nil
		})

		return &mockResult{last: id, rows: rows}, nil
	} else if s.query == "VACUUM" || strings.Contains(s.query, "PRAGMA wal_checkpoint") {
		// copy from wal to db
		if s.conn.tx != nil {
			// https://sqlite-users.sqlite.narkive.com/8rtWJfAH/vacuum-command-in-a-transaction
			return nil, errors.New("transaction not allowed")
		}
		s.conn.mu.Lock()
		defer s.conn.mu.Unlock()

		b, err := os.ReadFile(s.conn.wal.Name())
		if err != nil {
			return nil, err
		}
		_, err = s.conn.file.Write(b)
		if err != nil {
			return nil, err
		}

		if err := os.Truncate(s.conn.wal.Name(), 0); err != nil {
			return nil, err
		}
		return &mockResult{}, nil
	}
	return &mockResult{}, nil
}

func (s *mockStmt) Query(args []driver.Value) (driver.Rows, error) {
	if strings.Contains(s.query, "page_count * page_size") {
		s.conn.mu.Lock()
		defer s.conn.mu.Unlock()

		return &mockRows{
			columns: []string{"total_size", "freelist_size"},
			values: [][]any{{
				testGetFileSize(s.conn.wal.Name()) + testGetFileSize(s.conn.file.Name()),
				0,
			}},
		}, nil
	}
	return &mockRows{}, nil
}

type mockResult struct {
	last int64
	rows int64
}

func (r *mockResult) LastInsertId() (int64, error) {
	return r.last, nil
}

func (r *mockResult) RowsAffected() (int64, error) {
	return r.rows, nil
}

// Rows is an iterator over an executed query's results.
type mockRows struct {
	query   string
	index   int
	args    []driver.Value
	values  [][]any
	columns []string
}

func (r *mockRows) Columns() []string {
	return r.columns
}

func (r *mockRows) Close() error {
	return nil
}

func (r *mockRows) Next(dest []driver.Value) error {
	if r.index > len(r.values)-1 {
		return io.EOF
	}
	values := r.values[r.index]
	r.index++
	for i, _ := range r.columns {
		dest[i] = values[i]
	}
	return nil
}

func testGetFileSize(file string) int64 {
	info, _ := os.Stat(file)
	return info.Size()
}
