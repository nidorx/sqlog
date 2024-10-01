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
	ChunkSize uint16

	// O tamanho máximo em bytes desejado de um chunk.
	//
	// Se ultrapassar esse valor será enviado para o storage (default 0)
	MaxChunkSizeMB int

	// A quantidade máxima de chunks com dados em memória (default 50)
	//
	// Evita o consumo infinito de memória em caso de falha catastrófica
	// na escrita de logs.
	//
	// Quando esse limite é atingido, os chunks antigos são eliminados
	MaxDirtyChunks int

	// Tenta persistir um chunk quantas vezes em caso de falha (default 3)
	MaxFlushRetry uint8

	// Se o Chunk atual ficar inativo por esse tempo, envia para
	// o Storage (default 3 segundos)
	FlushAfterSec int

	// Intervalo de manutenção dos chunks em milisegundos (default 100)
	IntervalCheckMs int32
}

type Ingester struct {
	flushChunk   *Chunk // chunk que será salvo na base de dados
	writeChunk   *Chunk // chunk que está recebendo registros de log
	writeChunkId int32  // Id do chunk que está sendo usado para escrever atualmente
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

	if config.MaxFlushRetry <= 0 {
		config.MaxFlushRetry = 3
	}

	if config.ChunkSize <= 0 {
		config.ChunkSize = 900
	}

	if config.ChunkSize > 900 {
		config.ChunkSize = 900
	}

	if config.IntervalCheckMs <= 0 {
		config.IntervalCheckMs = 100
	}

	root := NewChunk(int32(config.ChunkSize))
	root.Init(config.Chunks)

	i := &Ingester{
		config:       config,
		writeChunkId: root.id,
		flushChunk:   root,
		writeChunk:   root,
		storage:      storage,
	}

	go i.routineCheck()

	return i, nil
}

func (i *Ingester) Ingest(t time.Time, level int8, content []byte) error {
	lastWriteId := i.writeChunkId
	chunk, isFull := i.writeChunk.Put(&Entry{t, level, content})
	if isFull && atomic.CompareAndSwapInt32(&i.writeChunkId, lastWriteId, chunk.id) {
		// o chunk está cheio, aponta para o proximo
		i.writeChunk = chunk
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

			for ; i.flushChunk.Ready(); i.flushChunk = i.flushChunk.Next() {
				chunk := i.flushChunk
				if chunk.Empty() {
					break
				}

				if chunk.Ready() {
					if err := i.storage.Flush(chunk); err != nil {
						chunk.retries++
						slog.Error("[sqlog] error writing chunk", slog.Any("error", err))

						if chunk.retries > uint(i.config.MaxFlushRetry) {
							chunk.Init(i.config.Chunks + 1)
						} else {
							break
						}
					} else {
						chunk.Init(i.config.Chunks + 1)
					}
				} else {
					if int(chunk.TTL().Seconds()) > i.config.FlushAfterSec ||
						(i.config.MaxChunkSizeMB > 0 && chunk.Size() > int64(i.config.MaxChunkSizeMB)*1000000) {
						// bloqueia a escrita no chunk para que possa ser persistido na próxima execuçao
						chunk.Lock()
						chunk.Init(i.config.Chunks)
					}
					break
				}
			}

			// limita consumo de memória
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

			// if !block.Empty() {

			// 	if block.Ready() {
			// 		if err := i.storage.Flush(block); err != nil {
			// 			block.retries++
			// 			slog.Error("[sqlog] error writing chunk", slog.Any("error", err))

			// 			if block.retries > uint(i.config.FlushMaxRetry) {
			// 				block.Init(i.config.Chunks + 1)
			// 				i.flushBlock = block.next
			// 			}
			// 		} else {
			// 			block.Init(i.config.Chunks + 1)
			// 			i.flushBlock = block.next
			// 		}
			// 	} else if int(block.TTL().Seconds()) > i.config.FlushAfterSec {
			// 		// bloqueia a escrita no chunk para que possa ser persistido nos próximos ticks
			// 		block.Lock()
			// 		block.Init(i.config.Chunks)
			// 	}

			// 	// evita vazamento de memória
			// 	if i.flushBlock.Depth() > i.config.MaxDirtyChunks {
			// 		for {
			// 			if i.flushBlock.Depth() > i.config.MaxDirtyChunks {
			// 				block.Init(1)
			// 				i.flushBlock = block.next
			// 			} else {
			// 				break
			// 			}
			// 		}
			// 		i.flushBlock.Init(i.config.Chunks)
			// 	}
			// }
			tick.Reset(d)

		case <-i.quit:
			// faz o flush de todos os logs

			tick.Stop()

			chunk := i.flushChunk
			chunk.Lock()

			for {
				if chunk.Empty() {
					break
				}

				if chunk.Ready() {
					// write
					i.storage.Flush(chunk)
					chunk = chunk.Next()
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
