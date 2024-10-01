package sqlog

import (
	"errors"
	"log/slog"
	"sync/atomic"
	"time"
)

// ErrIngesterClosed This error is returned by methods of the `Client` interface when they are
// called after the Ingester was already closed.
var ErrIngesterClosed = errors.New("[sqlog] the ingester was already closed")

type IngesterConfig struct {

	// Tamnho do buffer de chunks (default 3)
	Chunks uint8

	// Quantidade máxima desejada de registros por chunk, antes de
	// persistir no storage (default|max 900)
	BatchSize uint16

	// A quantidade máxima de chunks com dados em memória (default 50)
	//
	// Evita o consumo infinito de memória em caso de falha catastrófica
	// na escrita de logs.
	//
	// Quando esse limite é atingido, os chunks antigos são eliminados
	MaxDirtyChunks int

	// Tenta persistir um chunk quantas vezes em caso de falha (default 3)
	FlushMaxRetry uint8

	// Se o Chunk atual ficar inativo por esse tempo, envia para
	// o Storage (default 3 segundos)
	FlushAfterSec int

	// Intervalo de manutenção dos chunks em milisegundos (default 100)
	IntervalCheckMs int32
}

type Ingester struct {
	flushBlock   *Chunk // chunk que será salvo na base de dados
	writeBlock   *Chunk // chunk que está recebendo registros de log
	writeBlockId int32  // Id do chunk que está sendo usado para escrever atualmente
	config       *IngesterConfig
	storage      Storage
	quit         chan struct{}
	shutdown     chan struct{}
}

func NewIngester(config *IngesterConfig, storage Storage) (*Ingester, error) {
	if config == nil {
		config = &IngesterConfig{}
	}

	if config.Chunks <= 0 {
		config.Chunks = 3
	}

	if config.MaxDirtyChunks <= int(config.Chunks) {
		config.MaxDirtyChunks = 50
	}

	if config.FlushAfterSec <= 0 {
		config.FlushAfterSec = 3
	}

	if config.FlushMaxRetry <= 0 {
		config.FlushMaxRetry = 3
	}

	if config.BatchSize <= 0 {
		config.BatchSize = 900
	}

	if config.BatchSize > 900 {
		config.BatchSize = 900
	}

	if config.IntervalCheckMs <= 0 {
		config.IntervalCheckMs = 100
	}

	root := NewChunk(int32(config.BatchSize))
	root.Init(config.Chunks)

	i := &Ingester{
		config:       config,
		writeBlockId: root.id,
		flushBlock:   root,
		writeBlock:   root,
		storage:      storage,
	}

	go i.routineCheck()

	return i, nil
}

func (i *Ingester) Ingest(t time.Time, level int8, content []byte) error {
	block, isFull := i.writeBlock.Put(&Entry{t, level, content})
	if isFull && atomic.CompareAndSwapInt32(&i.writeBlockId, i.writeBlockId, block.id) {
		// o chunk está cheio, aponta para o proximo
		i.writeBlock = block
	}
	return nil
}

// routineCheck
func (i *Ingester) routineCheck() {
	defer close(i.shutdown)

	d := time.Duration(i.config.IntervalCheckMs)
	tick := time.NewTicker(d)
	defer tick.Stop()

	for {
		select {

		case <-tick.C:

			block := i.flushBlock

			if !block.Empty() {

				if block.Full() {
					if err := i.storage.Flush(block); err != nil {
						block.retries++
						slog.Error("[sqlog] error writing chunk", slog.Any("error", err))

						if block.retries > uint(i.config.FlushMaxRetry) {
							block.Init(i.config.Chunks + 1)
							i.flushBlock = block.next
						}
					} else {
						block.Init(i.config.Chunks + 1)
						i.flushBlock = block.next
					}
				} else if int(block.TTL().Seconds()) > i.config.FlushAfterSec {
					// bloqueia a escrita no chunk para que possa ser persistido nos próximos ticks
					block.Lock()
					block.Init(i.config.Chunks)
				}

				// evita vazamento de memória
				if i.flushBlock.Depth() > i.config.MaxDirtyChunks {
					for {
						if i.flushBlock.Depth() > i.config.MaxDirtyChunks {
							block.Init(1)
							i.flushBlock = block.next
						} else {
							break
						}
					}
					i.flushBlock.Init(i.config.Chunks)
				}
			}
			tick.Reset(d)

		case <-i.quit:
			// faz o flush de todos os logs

			tick.Stop()

			chunk := i.flushBlock
			chunk.Lock()

			for {
				if chunk.Empty() {
					break
				}

				if chunk.Full() {
					// write
					i.storage.Flush(chunk)
					chunk = chunk.next
					chunk.Lock()
				} else {
					// aguarda para persistir esse chunk
					time.Sleep(10 * time.Millisecond)
				}
			}

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

// Close faz o flush dos dados pendentes e fecha o storage
func (i *Ingester) Close() (err error) {
	defer func() {
		// Always recover, a panic could be raised if `c`.quit was closed which
		// means the method was called more than once.
		if recover() != nil {
			err = ErrIngesterClosed
		}
	}()

	close(i.quit)

	<-i.shutdown

	return nil
}
