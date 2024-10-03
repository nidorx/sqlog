package sqlog

import (
	"sync"
	"sync/atomic"
	"time"
)

// Entry represents a formatted log entry
type Entry struct {
	Time    time.Time
	Level   int8
	Content []byte
}

// Chunk stores up to 900 log entries that will be persisted in the storage
type Chunk struct {
	mu         sync.Mutex
	id         int32       // The identifier of this chunk
	cap        int32       // Configured batch size
	book       int32       // Number of scheduled writes in this chunk
	write      int32       // Number of writes completed
	size       int64       // Size of content (in bytes)
	epochStart int64       // First epoch
	epochEnd   int64       // Last epoch
	retries    int         // Number of attempts to persist in the storage
	locked     atomic.Bool // Indicates if this chunk no longer accepts writes
	next       *Chunk      // Pointer to the next chunk
	entries    [900]*Entry // The log entries in this chunk
}

// NewChunk creates a new chunk with the specified capacity
func NewChunk(cap int32) *Chunk {
	return &Chunk{cap: min(max(1, cap), 900)}
}

// ID returns the identifier of this chunk
func (c *Chunk) ID() int32 {
	return c.id
}

// Next returns the next chunk in the sequence
func (c *Chunk) Next() *Chunk {
	c.Init(1) // ensures the next chunk is initialized
	return c.next
}

// Size returns the size of this chunk (in bytes)
func (c *Chunk) Size() int64 {
	return c.size
}

// Init initializes the next chunks
func (c *Chunk) Init(depth uint8) {
	if depth > 0 {
		if c.next == nil {
			c.mu.Lock()
			if c.cap <= 0 {
				c.cap = 900
			}
			if c.next == nil {
				c.next = &Chunk{cap: c.cap}
				c.next.id = c.id + 1
			}
			c.mu.Unlock()
		}

		depth--
		if depth > 0 {
			c.next.Init(depth)
		}
	}
}

// Depth retrieves the number of non-empty chunks
func (c *Chunk) Depth() int {
	if c.Empty() {
		return 0
	}
	if c.next == nil {
		return 1
	}
	return 1 + c.next.Depth()
}

// First retrieves the epoch of the first entry in this chunk
func (c *Chunk) First() int64 {
	return c.epochStart
}

// Last retrieves the epoch of the last entry in this chunk
func (c *Chunk) Last() int64 {
	return c.epochEnd
}

// TTL retrieves the age of the last log entry inserted in this chunk
func (c *Chunk) TTL() time.Duration {
	index := c.write - 1
	if index < 0 || c.Empty() {
		return 0
	}
	last := c.entries[index].Time
	if last.IsZero() {
		return 0
	}
	return time.Since(last)
}

// Ready indicates if this chunk is full and ready for a flush
func (c *Chunk) Ready() bool {
	return c.write == c.cap || (c.book > 0 && c.write == c.book && c.locked.Load())
}

// Empty indicates if no write attempts have been made
func (c *Chunk) Empty() bool {
	return c.book == 0
}

// Lock prevents further writes to this chunk.
// From this point on, writes will occur in the next chunk.
// This chunk will be ready for flushing after write confirmation.
func (c *Chunk) Lock() {
	c.locked.Store(true)
}

// Locked checks if this chunk is locked for writing
func (c *Chunk) Locked() bool {
	return c.locked.Load()
}

// List retrieves the list of written entries
func (c *Chunk) List() []*Entry {
	return c.entries[:c.write]
}

// Put attempts to write the log entry into this chunk.
// Returns the chunk that accepted the entry.
// If the chunk that accepted the entry is the same, it returns false in the second parameter.
func (c *Chunk) Put(e *Entry) (into *Chunk, isFull bool) {
	if c.locked.Load() {
		// chunk is locked
		i, _ := c.Next().Put(e)
		return i, true
	}

	index := (atomic.AddInt32(&c.book, 1) - 1)
	if index > (c.cap - 1) {
		// chunk is full
		i, _ := c.Next().Put(e)
		return i, true
	}

	defer atomic.AddInt32(&c.write, 1)

	// safe write
	c.entries[index] = e // @TODO: test to ensure it is safe

	entryEpoch := e.Time.Unix()
	if last := c.epochEnd; entryEpoch > last {
		atomic.CompareAndSwapInt64(&c.epochEnd, last, entryEpoch)
	}

	if first := c.epochStart; (first == 0 || entryEpoch < first) && !atomic.CompareAndSwapInt64(&c.epochStart, first, entryEpoch) {
		// unlikely to happen
		for i := 0; i < 3; i++ {
			first = c.epochStart
			if entryEpoch < first {
				if atomic.CompareAndSwapInt64(&c.epochStart, first, entryEpoch) {
					break
				}
			} else {
				break
			}
		}
	}

	atomic.AddInt64(&c.size, int64(len(e.Content)))

	return c, false
}
