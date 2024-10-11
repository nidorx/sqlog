package sqlog

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type testMockStorage struct {
	close func() error
	flush func(chunk *Chunk) error
}

func (m *testMockStorage) Flush(chunk *Chunk) error {
	if m.flush != nil {
		return m.flush(chunk)
	}
	return nil
}

func (m *testMockStorage) Close() error {
	if m.close != nil {
		return m.close()
	}
	return nil
}

func Test_Ingester_FlushAfterSec(t *testing.T) {

	var (
		mu    sync.Mutex
		chunk *Chunk
	)

	storage := &testMockStorage{
		flush: func(c *Chunk) error {
			mu.Lock()
			defer mu.Unlock()
			chunk = c
			return nil
		},
	}

	config := &IngesterConfig{
		Chunks:        3,
		FlushAfterSec: 1,
	}

	ingester, _ := NewIngester(config, storage)
	defer ingester.Close()

	ingester.Ingest(time.Now(), 0, []byte(`{"msg":"test"}`))

	waitMax(3*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return (chunk != nil)
	})

	assert.NotNil(t, chunk, "Storage.Flush not called")
}

func Test_Ingester_MaxChunkSizeBytes(t *testing.T) {

	var (
		mu    sync.Mutex
		chunk *Chunk
	)

	storage := &testMockStorage{
		flush: func(c *Chunk) error {
			mu.Lock()
			defer mu.Unlock()
			chunk = c
			return nil
		},
	}

	config := &IngesterConfig{
		Chunks:            3,
		FlushAfterSec:     50,
		MaxChunkSizeBytes: 2,
	}

	ingester, _ := NewIngester(config, storage)
	defer ingester.Close()

	ingester.Ingest(time.Now(), 0, []byte(`{"msg":"test"}`))

	waitMax(3*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return (chunk != nil)
	})

	assert.NotNil(t, chunk, "Storage.Flush not called")
}

func Test_Ingester_MaxFlushRetry(t *testing.T) {

	var (
		mu    sync.Mutex
		chunk *Chunk
	)

	storage := &testMockStorage{
		flush: func(c *Chunk) error {
			mu.Lock()
			defer mu.Unlock()
			chunk = c
			return errors.New("test")
		},
	}

	config := &IngesterConfig{
		Chunks:        3,
		FlushAfterSec: 1,
		MaxFlushRetry: 1,
	}

	ingester, _ := NewIngester(config, storage)
	defer ingester.Close()

	ingester.Ingest(time.Now(), 0, []byte(`{"msg":"test"}`))

	waitMax(5*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return (chunk != nil && chunk.Retries() > 1)
	})

	assert.Equal(t, int32(2), chunk.Retries())
}

func Test_Ingester_MaxDirtyChunks(t *testing.T) {

	storage := &testMockStorage{
		flush: func(c *Chunk) error {
			return errors.New("test")
		},
	}

	config := &IngesterConfig{
		Chunks:         3,
		ChunkSize:      4,
		FlushAfterSec:  50,
		MaxDirtyChunks: 5,
		MaxFlushRetry:  50, // << misconfigured parameter
	}

	ingester, _ := NewIngester(config, storage)
	defer ingester.Close()

	numEntries := 40
	for i := 0; i < numEntries; i++ {
		ingester.Ingest(time.Now(), 0, []byte(`{"msg":"test"}`))
	}

	waitMax(5*time.Second, func() bool {
		return atomic.LoadInt32(&ingester.flushChunkId) == 10
	})

	ingester.Close()

	// lastChunk.id = 10 = (numEntries/ChunkSize)
	assert.Equal(t, int32(10), atomic.LoadInt32(&ingester.flushChunkId))
}

func Test_Ingester_Close(t *testing.T) {
	storage := new(testMockStorage)
	config := &IngesterConfig{}
	ingester, _ := NewIngester(config, storage)
	defer ingester.Close()

	err := ingester.Close()
	assert.NoError(t, err)
}

func Test_Ingester_Close_Flush(t *testing.T) {

	var (
		chunk  *Chunk
		closed bool
	)

	storage := &testMockStorage{
		flush: func(c *Chunk) error {
			chunk = c
			return nil
		},
		close: func() error {
			closed = true
			return nil
		},
	}

	config := &IngesterConfig{
		Chunks:        3,
		FlushAfterSec: 1,
	}

	ingester, _ := NewIngester(config, storage)
	defer ingester.Close()

	ingester.Ingest(time.Now(), 0, []byte(`{"msg":"test"}`))

	err := ingester.Close()
	assert.NoError(t, err)

	assert.NotNil(t, chunk, "Storage.Flush not called")
	assert.True(t, closed, "Storage.Close not called")
}

func Test_Ingester_Close_MaxFlushRetry(t *testing.T) {

	var (
		chunk  *Chunk
		closed bool
	)

	storage := &testMockStorage{
		flush: func(c *Chunk) error {
			chunk = c
			return errors.New("test")
		},
		close: func() error {
			closed = true
			return nil
		},
	}

	config := &IngesterConfig{
		Chunks:        3,
		FlushAfterSec: 1,
		MaxFlushRetry: 1,
	}

	ingester, _ := NewIngester(config, storage)
	defer ingester.Close()

	ingester.Ingest(time.Now(), 0, []byte(`{"msg":"test"}`))

	err := ingester.Close()
	assert.NoError(t, err)

	assert.True(t, closed, "Storage.Close not called")
	assert.Equal(t, int32(2), chunk.Retries())
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
