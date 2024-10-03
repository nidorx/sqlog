package sqlog

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type testStorage struct {
	close func() error
	flush func(chunk *Chunk) error
}

func (m *testStorage) Flush(chunk *Chunk) error {
	if m.flush != nil {
		return m.flush(chunk)
	}
	return nil
}

func (m *testStorage) Close() error {
	if m.close != nil {
		return m.close()
	}
	return nil
}

func Test_IngesterFlushAfterSec(t *testing.T) {

	var chunk *Chunk

	storage := &testStorage{
		flush: func(c *Chunk) error {
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
		return (chunk != nil)
	})

	assert.NotNil(t, chunk, "Storage.Flush not called")
}

func Test_IngesterMaxChunkSizeBytes(t *testing.T) {

	var chunk *Chunk

	storage := &testStorage{
		flush: func(c *Chunk) error {
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
		return (chunk != nil)
	})

	assert.NotNil(t, chunk, "Storage.Flush not called")
}

func Test_IngesterMaxFlushRetry(t *testing.T) {

	var chunk *Chunk

	storage := &testStorage{
		flush: func(c *Chunk) error {
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
		return (chunk != nil && chunk.retries > 1)
	})

	assert.Equal(t, chunk.retries, 2)
}

func Test_IngesterMaxDirtyChunks(t *testing.T) {

	var (
		lastChunk *Chunk
		chunks    []*Chunk
	)

	storage := &testStorage{
		flush: func(c *Chunk) error {
			lastChunk = c
			if c != lastChunk {
				chunks = append(chunks, c)
			}
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
		return ingester.flushChunk.id == 10
	})

	ingester.Close()

	lastChunk = ingester.flushChunk

	// lastChunk.id = 10 = (numEntries/ChunkSize)
	assert.Equal(t, int32(10), lastChunk.id)
}

func Test_IngesterClose(t *testing.T) {
	storage := new(testStorage)
	config := &IngesterConfig{}
	ingester, _ := NewIngester(config, storage)
	defer ingester.Close()

	err := ingester.Close()
	assert.NoError(t, err)
}

func Test_IngesterCloseFlush(t *testing.T) {

	var (
		chunk  *Chunk
		closed bool
	)

	storage := &testStorage{
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

func Test_IngesterCloseMaxFlushRetry(t *testing.T) {

	var (
		chunk  *Chunk
		closed bool
	)

	storage := &testStorage{
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
	assert.Equal(t, chunk.retries, 2)
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
