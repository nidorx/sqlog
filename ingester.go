package sqlog

import (
	"errors"
	"log/slog"
	"sync/atomic"
	"time"
)

// ErrIngesterClosed is returned by methods of the `Client` interface
// when they are called after the Ingester has already been closed.
var ErrIngesterClosed = errors.New("[sqlog] the ingester was already closed")

// IngesterConfig contains configuration parameters for the ingester.
type IngesterConfig struct {
	// Chunks is the size of the chunk buffer (default 3).
	Chunks uint8

	// ChunkSize defines the maximum number of log records per chunk before
	// being persisted in storage (default|max 900).
	ChunkSize uint16

	// MaxChunkSizeBytes sets the maximum desired chunk size in bytes.
	// If this size is exceeded, the chunk will be sent to storage (default 0).
	MaxChunkSizeBytes int64

	// MaxDirtyChunks specifies the maximum number of chunks with data in memory (default 50).
	// This prevents unlimited memory consumption in the event of a catastrophic failure
	// in writing logs.
	MaxDirtyChunks int

	// MaxFlushRetry defines the number of retry attempts to persist a chunk in case of failure (default 3).
	MaxFlushRetry int

	// FlushAfterSec defines how long a chunk can remain inactive before being sent to storage (default 3 seconds).
	FlushAfterSec int

	// IntervalCheckMs sets the interval for chunk maintenance in milliseconds (default 100 ms).
	IntervalCheckMs int32
}

// Ingester is the interface that represents the behavior of the log ingester.
type Ingester interface {
	// Close terminates the ingester and flushes any remaining log data.
	Close() (err error)

	// Ingest adds a new log entry with the given timestamp, level, and content.
	Ingest(t time.Time, level int8, content []byte) error
}

// ingester is the implementation of the Ingester interface, responsible for managing
// log chunks and ensuring they are flushed to storage.
type ingester struct {
	flushChunk   *Chunk          // The chunk that will be saved to the database
	writeChunk   *Chunk          // The chunk currently receiving log entries
	writeChunkId int32           // ID of the currently active write chunk
	config       *IngesterConfig // Configuration options for the ingester
	storage      Storage         // The storage backend used to persist chunks
	quit         chan struct{}   // Channel used to signal termination
	shutdown     chan struct{}   // Channel used to signal shutdown completion
}

// NewIngester creates a new ingester with the given configuration and storage.
// If no configuration is provided, default values are used.
func NewIngester(config *IngesterConfig, storage Storage) (*ingester, error) {
	if config == nil {
		config = &IngesterConfig{}
	}

	// Set default values for config if necessary
	if config.Chunks <= 0 {
		config.Chunks = 3
	}

	if config.MaxDirtyChunks <= int(config.Chunks) {
		config.MaxDirtyChunks = 50
	}

	if config.FlushAfterSec <= 0 {
		config.FlushAfterSec = 3
	}

	if config.MaxFlushRetry <= 0 {
		config.MaxFlushRetry = 3
	}

	if config.ChunkSize <= 0 || config.ChunkSize > 900 {
		config.ChunkSize = 900
	}

	if config.IntervalCheckMs <= 0 {
		config.IntervalCheckMs = 100
	}

	root := NewChunk(int32(config.ChunkSize))
	root.Init(config.Chunks)

	i := &ingester{
		config:       config,
		writeChunkId: root.id,
		flushChunk:   root,
		writeChunk:   root,
		storage:      storage,
		quit:         make(chan struct{}),
		shutdown:     make(chan struct{}),
	}

	// Start the routine to regularly check chunk states
	go i.routineCheck()

	return i, nil
}

// Ingest adds a new log entry to the active write chunk. If the chunk becomes full,
// the ingester switches to a new chunk.
func (i *ingester) Ingest(t time.Time, level int8, content []byte) error {
	lastWriteId := i.writeChunkId
	chunk, isFull := i.writeChunk.Put(&Entry{t, level, content})
	if isFull && atomic.CompareAndSwapInt32(&i.writeChunkId, lastWriteId, chunk.id) {
		// The chunk is full, switch to the next one
		i.writeChunk = chunk
	}
	return nil
}

// routineCheck is responsible for periodically checking the status of chunks,
// flushing them if necessary, and managing shutdown procedures.
func (i *ingester) routineCheck() {
	defer close(i.shutdown)

	d := time.Duration(i.config.IntervalCheckMs)
	tick := time.NewTicker(d)
	defer tick.Stop()

	for {
		select {
		case <-tick.C:
			// Perform a routine check of chunk states
			i.doRoutineCheck()
			tick.Reset(d)

		case <-i.quit:
			// Flush all pending logs when termination is requested
			tick.Stop()

			chunk := i.flushChunk
			chunk.Lock()

			t := 0

			// Attempt to flush all chunks
			for {
				if chunk.Empty() {
					break
				}

				if chunk.Ready() {
					// If the chunk is ready to be written to storage, flush it
					if err := i.storage.Flush(chunk); err != nil {
						chunk.retries++
						slog.Error("[sqlog] error writing chunk", slog.Any("error", err))

						// If retries exceed the limit, move to the next chunk
						if chunk.retries > i.config.MaxFlushRetry {
							chunk = chunk.Next()
							chunk.Lock()
						} else {
							time.Sleep(10 * time.Millisecond)
						}
					} else {
						chunk = chunk.Next()
						chunk.Lock()
					}
				} else {
					// Unexpected state, continue checking next chunk
					t++
					if t > 3 {
						chunk = chunk.Next()
						chunk.Lock()
						continue
					}
					t = 0
					chunk.Lock()
					time.Sleep(2 * time.Millisecond)
				}
			}

			i.flushChunk = chunk

			// Close the storage after flushing all logs
			if err := i.storage.Close(); err != nil {
				slog.Warn(
					"[sqlog] error closing storage",
					slog.Any("error", err),
				)
			}

			return
		}
	}
}

// doRoutineCheck handles the periodic maintenance of chunks, flushing them if they
// meet the conditions for size or age, and ensuring memory usage stays within limits.
func (i *ingester) doRoutineCheck() {
	for {
		chunk := i.flushChunk
		if chunk.Empty() {
			break
		}

		// Flush the chunk if it's ready to be persisted
		if chunk.Ready() {
			if err := i.storage.Flush(chunk); err != nil {
				chunk.retries++
				slog.Error("[sqlog] error writing chunk", slog.Any("error", err))

				if chunk.retries > i.config.MaxFlushRetry {
					chunk.Init(i.config.Chunks + 1)
				} else {
					break
				}
			} else {
				chunk.Init(i.config.Chunks + 1)
			}
		} else {
			// If the chunk is inactive for too long or exceeds the size limit, prepare it for flushing
			if int(chunk.TTL().Seconds()) > i.config.FlushAfterSec {
				chunk.Lock() // Lock the chunk for flushing in the next routine
				chunk.Init(i.config.Chunks)
			} else if i.config.MaxChunkSizeBytes > 0 && chunk.Size() > i.config.MaxChunkSizeBytes {
				chunk.Lock() // Lock the chunk for flushing in the next routine
				chunk.Init(i.config.Chunks)
			}
			break
		}
		i.flushChunk = i.flushChunk.Next()
	}

	// Limit memory consumption by discarding old chunks if necessary
	if !i.flushChunk.Empty() && i.flushChunk.Depth() > i.config.MaxDirtyChunks {
		for {
			if i.flushChunk.Depth() > i.config.MaxDirtyChunks {
				i.flushChunk = i.flushChunk.Next()
			} else {
				break
			}
		}
		i.flushChunk.Init(i.config.Chunks)
	}
}

// Close flushes any pending log data and closes the storage.
func (i *ingester) Close() (err error) {
	defer func() {
		// Always recover, as a panic could be raised if `i.quit` was closed,
		// indicating the method was called more than once.
		if rec := recover(); rec != nil {
			err = ErrIngesterClosed
		}
	}()

	close(i.quit)

	<-i.shutdown

	return nil
}
