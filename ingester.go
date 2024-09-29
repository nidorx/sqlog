package sqlog

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

type ingesterImpl struct {
	store       *storageImpl
	writeHeadId int32
	flushHead   *chunk // chunk que será salvo na base de dados
	writeHead   *chunk // chunk que está recebendo registros de log
	// These two channels are used to synchronize the Ingester shutting down when
	// `close` is called.
	// The first channel is closed to signal the backend goroutine that it has
	// to stop, then the second one is closed by the backend goroutine to signal
	// that it has finished flushing all queued messages.
	quit     chan struct{}
	shutdown chan struct{}
}

func newIngester(config *Config, store *storageImpl) (*ingesterImpl, error) {

	c := &chunk{}
	c.init(5)

	i := &ingesterImpl{
		writeHeadId: c.id,
		flushHead:   c,
		writeHead:   c,
		store:       store,
	}

	go i.loop()

	return i, nil
}

func (i *ingesterImpl) ingest(t time.Time, level int8, content []byte) error {

	into, accepted := i.writeHead.offer(&entry{
		time:    t,
		level:   level,
		content: content,
	})
	if !accepted && atomic.CompareAndSwapInt32(&i.writeHeadId, i.writeHeadId, into.id) {
		// o chunk está cheio, aponta para o proximo
		i.writeHead = into
	}

	return nil
}

// Batch loop.
func (i *ingesterImpl) loop() {
	defer close(i.shutdown)

	wg := &sync.WaitGroup{}
	defer wg.Wait()

	tick := time.NewTicker(interval)
	defer tick.Stop()

	for {
		select {

		case <-tick.C:

			chunk := i.flushHead
			if !chunk.isEmpty() {
				if chunk.isFull() {
					// write
					if err := i.store.flush(chunk); err != nil {
						chunk.retries++
						slog.Error("[sqlog] error writing chunk", slog.Any("error", err))

						if chunk.retries > 3 {
							i.flushHead = chunk.next
							i.flushHead.init(5)
						}
					} else {
						i.flushHead = chunk.next
						i.flushHead.init(5)
					}
				} else if chunk.ttl().Seconds() > 4 {
					// bloqueia a escrita no chunk para que possa ser persistido nos próximos ticks
					chunk.lock()
					i.flushHead.init(5)
				}
			}

		case <-i.quit:

			// faz o flush de todos os logs
			tick.Stop()

			chunk := i.flushHead
			chunk.lock()

			for {
				if chunk.isEmpty() {
					return
				}

				if chunk.isFull() {
					// write
					i.store.flush(chunk)
					chunk = chunk.next
					chunk.lock()
				} else {
					time.Sleep(10 * time.Millisecond)
				}
			}
		}
	}
}

func (i *ingesterImpl) close() (err error) {
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
