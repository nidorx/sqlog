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
	MaxChunkSizeBytes int64

	// A quantidade máxima de chunks com dados em memória (default 50)
	//
	// Evita o consumo infinito de memória em caso de falha catastrófica
	// na escrita de logs.
	//
	// Quando esse limite é atingido, os chunks antigos são eliminados
	MaxDirtyChunks int

	// Tenta persistir um chunk quantas vezes em caso de falha (default 3)
	MaxFlushRetry int

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
		quit:         make(chan struct{}),
		shutdown:     make(chan struct{}),
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

			i.doRoutineCheck()
			tick.Reset(d)

		case <-i.quit:
			// faz o flush de todos os logs

			tick.Stop()

			chunk := i.flushChunk
			chunk.Lock()

			t := 0

			for {
				if chunk.Empty() {
					break
				}

				if chunk.Ready() {
					if err := i.storage.Flush(chunk); err != nil {
						chunk.retries++
						slog.Error("[sqlog] error writing chunk", slog.Any("error", err))

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
					// should never happen (maybe single-event upset (SEU) :) )
					// see chunk.Ready() and chunk.Put()
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

func (i *Ingester) doRoutineCheck() {
	for {
		chunk := i.flushChunk
		if chunk.Empty() {
			break
		}

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
			if int(chunk.TTL().Seconds()) > i.config.FlushAfterSec {
				chunk.Lock() // bloqueia a escrita no chunk para que possa ser persistido na próxima execuçao
				chunk.Init(i.config.Chunks)
			} else if i.config.MaxChunkSizeBytes > 0 && chunk.Size() > i.config.MaxChunkSizeBytes {
				chunk.Lock() // bloqueia a escrita no chunk para que possa ser persistido na próxima execuçao
				chunk.Init(i.config.Chunks)
			}
			break
		}
		i.flushChunk = i.flushChunk.Next()
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

}

// Close faz o flush dos dados pendentes e fecha o storage
func (i *Ingester) Close() (err error) {
	defer func() {
		// Always recover, a panic could be raised if `c`.quit was closed which
		// means the method was called more than once.
		if rec := recover(); rec != nil {
			err = ErrIngesterClosed
		}
	}()

	close(i.quit)

	<-i.shutdown

	return nil
}
