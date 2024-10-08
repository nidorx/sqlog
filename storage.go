package sqlog

// Storage storage contract
type Storage interface {
	Close() error             // Close storage must perform cleaning during shutdown
	Flush(chunk *Chunk) error // Flush the chunk must persist
}

// StorageWithApi contract for storage that allows search
type StorageWithApi interface {
	Storage

	// Fetches information about all series within the range.
	Ticks(input *TicksInput) (*Output, error)

	// Fetches a page of results (seek method or keyset pagination).
	// The sorting is reversed, with the oldest result coming first.
	Entries(input *EntriesInput) (*Output, error)
	Result(taskId int32) (*Output, error)
	Cancel(taskId int32) error
}

type DummyStorage struct {
}

func (s *DummyStorage) Flush(chunk *Chunk) error {
	return nil
}

func (s *DummyStorage) Close() error {
	return nil
}
