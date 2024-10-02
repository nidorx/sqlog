package memory

import "sqlog"

type MemoryConfig struct {
	// Each time the current log file reaches MaxFilesize,
	// it will be archived (default 20).
	MaxSizeMB int32

	// Fecha banco de dados que estão inativos (default 30)
	MaxAgeSec int64

	// Intervalo de manutençao do storage em segundos (default 5)
	IntervalSizeCheckSec int32
}

// MemoryStorage not optimized storage implementation that keeps logs in memory.
// Useful for systems with limited storage or for debugging
// logs without disk persistence
type MemoryStorage struct {
	sqlog.Storage
	sqlog.StorageWithApi
}

func (s *MemoryStorage) Flush(chunk *sqlog.Chunk) error {
	// var (
	// 	epochStart = chunk.First().Unix()
	// 	epochEnd   = chunk.Last().Unix()
	// )
	return nil
}

func (s *MemoryStorage) Close() error {
	return nil
}

// Ticks(input *TicksInput) (*Output, error)
// 	Entries(input *EntriesInput) (*Output, error)

func (s *MemoryStorage) Ticks(input *sqlog.TicksInput) (*sqlog.Output, error) {
	return nil, nil
}

func (s *MemoryStorage) Entries(input *sqlog.EntriesInput) (*sqlog.Output, error) {
	return nil, nil
}
