package litelog

import (
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// ErrClosed This error is returned by methods of the `Client` interface when they are
// called after the Ingester was already closed.
var ErrClosed = errors.New("the Ingester was already closed")

// This constant sets the default flush interval used by Ingester instances if
// none was explicitly set.
const interval = 100 * time.Millisecond

type ingester struct {
	store        *store
	writeChunkId int32
	flushChunk   *chunk // chunk que será salvo na base de dados
	writeChunk   *chunk // chunk que está recebendo registros de log
	// These two channels are used to synchronize the Ingester shutting down when
	// `close` is called.
	// The first channel is closed to signal the backend goroutine that it has
	// to stop, then the second one is closed by the backend goroutine to signal
	// that it has finished flushing all queued messages.
	quit     chan struct{}
	shutdown chan struct{}
}

func newIngester(config *Config, store *store) (*ingester, error) {

	c := &chunk{}
	c.init(5)

	i := &ingester{
		writeChunkId: c.id,
		flushChunk:   c,
		writeChunk:   c,
		store:        store,
	}

	go i.loop()

	return i, nil
}

func (i *ingester) ingest(t time.Time, level int8, content []byte) error {

	into, accepted := i.writeChunk.offer(&entry{
		time:    t,
		level:   level,
		content: content,
	})
	if !accepted && atomic.CompareAndSwapInt32(&i.writeChunkId, i.writeChunkId, into.id) {
		// o chunk está cheio, aponta para o proximo
		i.writeChunk = into
	}

	return nil
}

// Batch loop.
func (i *ingester) loop() {
	defer close(i.shutdown)

	wg := &sync.WaitGroup{}
	defer wg.Wait()

	tick := time.NewTicker(interval)
	defer tick.Stop()

	for {
		select {

		case <-tick.C:

			chunk := i.flushChunk
			if !chunk.isEmpty() {
				if chunk.isFull() {
					// write
					if err := i.store.write(chunk); err != nil {
						chunk.retries++
						slog.Error("[litelog] error writing chunk", slog.Any("error", err))

						if chunk.retries > 3 {
							i.flushChunk = chunk.next
							i.flushChunk.init(5)
						}
					} else {
						i.flushChunk = chunk.next
						i.flushChunk.init(5)
					}
				} else if chunk.ttl().Seconds() > 4 {
					// bloqueia a escrita no chunk para que possa ser persistido nos próximos ticks
					chunk.lock()
					i.flushChunk.init(5)
				}
			}

		case <-i.quit:

			// faz o flush de todos os logs
			tick.Stop()

			chunk := i.flushChunk
			chunk.lock()

			for {
				if chunk.isEmpty() {
					return
				}

				if chunk.isFull() {
					// write
					i.store.write(chunk)
					chunk = chunk.next
					chunk.lock()
				} else {
					time.Sleep(10 * time.Millisecond)
				}
			}
		}
	}
}

func (i *ingester) close() (err error) {
	defer func() {
		// Always recover, a panic could be raised if `c`.quit was closed which
		// means the method was called more than once.
		if recover() != nil {
			err = ErrClosed
		}
	}()
	close(i.quit)
	<-i.shutdown

	return nil
}
