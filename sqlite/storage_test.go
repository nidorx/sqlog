package sqlite

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"
	"unsafe"

	"math/rand"

	"github.com/nidorx/sqlog"
	"github.com/stretchr/testify/assert"
)

const (
	storageDir    = "testdata/logs"
	storagePrefix = "test"
)

func init() {
	sql.Register("sqlite3", &mockDriver{
		conns: make(map[string]*mockDriverConn),
	})
}

func Test_Sqlite_Simple(t *testing.T) {
	testClearDir(storageDir)
	defer testClearDir(storageDir)

	storage, err := New(&Config{
		Dir:    storageDir,
		Prefix: storagePrefix,
	})
	assert.Nil(t, err)

	log, err := sqlog.New(&sqlog.Config{Storage: storage})
	assert.Nil(t, err)
	defer log.Stop()

	logger := slog.New(log.Handler())

	for i := 0; i < 100; i++ {
		logger.Info(randString(256))
	}

	log.Stop()

	waitMax(3*time.Second, func() bool {
		return storage.closed.Load()
	})

	assert.Equal(t, 1, len(storage.dbs))
	db := storage.dbs[0]
	assert.Equal(t, db_closed, db.status)
	assert.Nil(t, db.db)
}

func Test_Sqlite_MaxFilesize(t *testing.T) {
	testClearDir(storageDir)
	defer testClearDir(storageDir)

	storage, err := New(&Config{
		Dir:                      storageDir,
		Prefix:                   storagePrefix,
		MaxFilesizeMB:            1,
		IntervalSizeCheckSec:     1000,
		IntervalScheduledTasksMs: 1000000,
		MaxChunkAgeSec:           1,
		CloseIdleSec:             1,
	})
	assert.Nil(t, err)
	defer storage.Close()

	chunk := sqlog.NewChunk(99)

	for i := 0; i < 1000; i++ {
		chunk.Put(&sqlog.Entry{
			Time:    time.Now(),
			Level:   0,
			Content: []byte(fmt.Sprintf(`{"msg":"%s"}`, randString(1024))),
		})
	}

	for ; !chunk.Empty(); chunk = chunk.Next() {
		storage.Flush(chunk)
	}

	time.Sleep(2 * time.Second)

	storage.doRoutineSizeCheck()

	assert.Equal(t, 2, len(storage.dbs))
	assert.Equal(t, 1, len(storage.liveDbs))

	storage.doRoutineScheduledTasks()

	db := storage.dbs[0]
	assert.Equal(t, db_closed, db.status)
	assert.Nil(t, db.db)
	assert.Equal(t, len(storage.liveDbs), 1)
}

func Test_Sqlite_TotalSizeCap(t *testing.T) {
	testClearDir(storageDir)
	defer testClearDir(storageDir)

	storage, err := New(&Config{
		Dir:                      storageDir,
		Prefix:                   storagePrefix,
		MaxFilesizeMB:            1,
		MaxSizeTotalMB:           3,
		IntervalSizeCheckSec:     1000,
		IntervalScheduledTasksMs: 1000000,
		MaxChunkAgeSec:           1,
		CloseIdleSec:             1,
	})
	assert.Nil(t, err)
	defer storage.Close()

	firstDb := storage.dbs[0] // remove

	for j := 0; j < 3; j++ {
		root := sqlog.NewChunk(100)
		chunk := root
		for i := 0; i < 1001; i++ {
			chunk, _ = chunk.Put(&sqlog.Entry{
				Time:    time.Now(), //.Add(time.Duration(-1) * time.Second),
				Level:   0,
				Content: []byte(fmt.Sprintf(`{"msg":"%s"}`, randString(1024))),
			})
		}
		for c := root; !c.Empty(); c = c.Next() {
			storage.Flush(c)
		}
		time.Sleep(1200 * time.Millisecond)
		storage.doRoutineSizeCheck()
	}

	time.Sleep(1200 * time.Millisecond)
	storage.doRoutineSizeCheck()
	storage.doRoutineScheduledTasks()

	assert.GreaterOrEqual(t, len(storage.dbs), 2)
	assert.Equal(t, 1, len(storage.liveDbs))

	assert.Equal(t, db_closed, storage.dbs[0].status)
	assert.Nil(t, storage.dbs[0].db)

	assert.Equal(t, len(storage.liveDbs), 1)

	assert.NoFileExists(t, firstDb.filePath)
	assert.FileExists(t, storage.dbs[1].filePath)

}

func Test_Sqlite_WALCheckpoint(t *testing.T) {
	testClearDir(storageDir)
	defer testClearDir(storageDir)

	storage, err := New(&Config{
		Dir:                      storageDir,
		Prefix:                   storagePrefix,
		MaxFilesizeMB:            1,
		MaxSizeTotalMB:           3,
		IntervalSizeCheckSec:     1000,
		IntervalScheduledTasksMs: 1000000,
		IntervalWalCheckpointSec: 1,
		MaxChunkAgeSec:           1,
		CloseIdleSec:             1,
	})
	assert.Nil(t, err)
	defer storage.Close()

	db := storage.dbs[0]
	walFile := db.filePath + "-wal"

	for j := 0; j < 1; j++ {
		root := sqlog.NewChunk(99)
		chunk := root
		for i := 0; i < 1001; i++ {
			chunk, _ = chunk.Put(&sqlog.Entry{
				Time:    time.Now(), //.Add(time.Duration(-1) * time.Second),
				Level:   0,
				Content: []byte(fmt.Sprintf(`{"msg":"%s"}`, randString(1024))),
			})
		}
		for c := root; !c.Empty(); c = c.Next() {
			storage.Flush(c)
		}
		time.Sleep(1 * time.Second)
		storage.doRoutineSizeCheck()
	}

	time.Sleep(1 * time.Second)
	storage.doRoutineSizeCheck()
	storage.doRoutineScheduledTasks()

	// log, err := sqlog.New(&sqlog.Config{Storage: storage})
	// assert.Nil(t, err)
	// defer log.Stop()

	// logger := slog.New(log.Handler())

	// for i := 0; i < 1000; i++ {
	// 	logger.Info(randString(1024))
	// }

	// waitMax(5*time.Second, func() bool {
	// 	return testGetFileSize(walFile) > 0
	// })

	// waitMax(5*time.Second, func() bool {
	// 	return testGetFileSize(walFile) == 0
	// })

	assert.Equal(t, int64(0), testGetFileSize(walFile))
	assert.Greater(t, testGetFileSize(db.filePath), int64(1000*1024))
}

func testClearDir(dir string) {
	if err := os.RemoveAll(dir); err != nil {
		panic(err)
	}
}

func waitMax(max time.Duration, condition func() bool) {
	init := time.Now()
	for {
		if condition() || time.Since(init) > max {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
}

var src = rand.NewSource(time.Now().UnixNano())

const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
const (
	letterIdxBits = 6                    // 6 bits to represent a letter index
	letterIdxMask = 1<<letterIdxBits - 1 // All 1-bits, as many as letterIdxBits
	letterIdxMax  = 63 / letterIdxBits   // # of letter indices fitting in 63 bits
)

// https://stackoverflow.com/a/31832326/2204014
func randString(n int) string {
	b := make([]byte, n)
	// A src.Int63() generates 63 random bits, enough for letterIdxMax characters!
	for i, cache, remain := n-1, src.Int63(), letterIdxMax; i >= 0; {
		if remain == 0 {
			cache, remain = src.Int63(), letterIdxMax
		}
		if idx := int(cache & letterIdxMask); idx < len(letterBytes) {
			b[i] = letterBytes[idx]
			i--
		}
		cache >>= letterIdxBits
		remain--
	}

	return *(*string)(unsafe.Pointer(&b))
}
