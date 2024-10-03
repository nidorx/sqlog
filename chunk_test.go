package sqlog

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func Test_Chunk_Depth(t *testing.T) {
	chunk := NewChunk(1)
	assert.Equal(t, 0, chunk.Depth(), "Empty chunk should have depth 0")

	chunk.Put(&Entry{Time: time.Now()})
	assert.Equal(t, 1, chunk.Depth(), "Chunk with one entry should have depth 1")

	chunk.Init(1)
	chunk.Put(&Entry{Time: time.Now()})
	assert.Equal(t, 2, chunk.Depth(), "Chunk with next initialized chunk should have depth 2")
}

func Test_Chunk_Init(t *testing.T) {
	chunk := &Chunk{}
	chunk.Init(2)
	assert.NotNil(t, chunk.Next(), "Next chunk should be initialized")
	assert.Equal(t, int32(1), chunk.Next().ID(), "ID of the next chunk should be 1")
}

func Test_Chunk_First(t *testing.T) {
	chunk := NewChunk(2)
	assert.Equal(t, int64(0), chunk.First(), "Empty chunk should return zero time for First")

	entry := &Entry{Time: time.Now()}
	chunk.Put(entry)
	assert.Equal(t, entry.Time.Unix(), chunk.First(), "Chunk with one entry should return the timestamp of the first entry")
}

func Test_Chunk_Last(t *testing.T) {
	chunk := NewChunk(2)
	assert.Equal(t, int64(0), chunk.Last(), "Empty chunk should return zero time for Last")

	entry := &Entry{Time: time.Now()}
	chunk.Put(entry)
	assert.Equal(t, entry.Time.Unix(), chunk.Last(), "Chunk with one entry should return the timestamp of the last entry")
}

func Test_Chunk_TTL(t *testing.T) {
	chunk := NewChunk(2)
	assert.Equal(t, time.Duration(0), chunk.TTL(), "TTL of empty chunk should be zero")

	entry := &Entry{Time: time.Now()}
	chunk.Put(entry)
	time.Sleep(1 * time.Second)
	assert.Greater(t, chunk.TTL(), time.Duration(0), "TTL of chunk with one entry should be greater than zero")
}

func Test_Chunk_Full(t *testing.T) {
	chunk := NewChunk(2)
	assert.False(t, chunk.Ready(), "Empty chunk should not be full")

	chunk.Put(&Entry{Time: time.Now()})
	chunk.Put(&Entry{Time: time.Now()})
	assert.True(t, chunk.Ready(), "Chunk should be full when it reaches capacity")
}

func Test_Chunk_Empty(t *testing.T) {
	chunk := NewChunk(2)
	assert.True(t, chunk.Empty(), "Newly created chunk should be empty")

	chunk.Put(&Entry{Time: time.Now()})
	assert.False(t, chunk.Empty(), "Chunk with one entry should not be empty")
}

func Test_Chunk_Lock(t *testing.T) {
	chunk := NewChunk(2)
	chunk.Lock()

	assert.True(t, chunk.Locked(), "Chunk should be locked after calling Lock")

	new, isFull := chunk.Put(&Entry{Time: time.Now()})
	assert.True(t, isFull, "Chunk should be full after calling Lock")
	assert.Equal(t, new, chunk.Next(), "Chunk.Put should return the next chunk when Locked")
	assert.NotEqual(t, chunk, new, "Chunk.Put should return the next chunk when Locked")
}

func Test_Chunk_List(t *testing.T) {
	chunk := NewChunk(5)
	assert.Empty(t, chunk.List(), "Empty chunk should return an empty list")

	entry := &Entry{Time: time.Now()}
	chunk.Put(entry)

	list := chunk.List()
	assert.Len(t, list, 1, "Chunk with one entry should return list with 1 item")
	assert.Equal(t, entry, list[0], "The first item in the list should be the inserted entry")
}

func Test_Chunk_Put(t *testing.T) {
	chunk := NewChunk(1)

	entry := &Entry{Time: time.Now()}
	resultChunk, isFull := chunk.Put(entry)
	assert.Equal(t, chunk, resultChunk, "The current chunk should accept the entry")
	assert.False(t, isFull, "Chunk should not be full after the first insertion")

	// Test when the chunk is full
	entry2 := &Entry{Time: time.Now()}
	resultChunk, isFull = chunk.Put(entry2)
	assert.NotEqual(t, chunk, resultChunk, "A new chunk should accept the entry after the current chunk is full")
	assert.True(t, isFull, "Original chunk should be full after the second insertion")
}
