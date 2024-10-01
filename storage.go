package sqlog

// Storage contrato para storage
type Storage interface {

	// Close o storage deve realizar as limpezas durante o encerramento
	Close() error

	// Flush deve persistir o chunk
	Flush(chunk *Chunk) error
}

type StorageWithApi interface {
	Storage

	// Close o storage deve realizar as limpezas durante o encerramento
	Entries(input *EntriesInput) (*Output, error)

	// Flush deve persistir o chunk
	Ticks(input *TicksInput) (*Output, error)
}

// func (l *sqlog) Entries(input *EntriesInput) (*Output, error) {
// 	return l.storage.listEntries(input)
// }

// func (l *sqlog) Ticks(input *TicksInput) (*Output, error) {
// 	return l.storage.listTicks(input)
// }

type DummyStorage struct {
}

func (s *DummyStorage) Flush(chunk *Chunk) error {
	return nil
}

func (s *DummyStorage) Close() error {
	return nil
}
