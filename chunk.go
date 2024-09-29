package litelog

import (
	"sync"
	"sync/atomic"
	"time"
)

// entry representa uma entrada de log formatada
type entry struct {
	time    time.Time
	level   int8
	content []byte
}

// chunk abriga dados de até 900 logs que serão persistidos na base
type chunk struct {
	mu      sync.Mutex
	id      int32       // o identificador desse chunk
	book    int32       // quantidade de escritas agendadas nesse chunk
	write   int32       // quantidade de escritas realizadas
	retries uint        // quantas vezes tentou persistir na base
	locked  atomic.Bool // indica que este chunk não aceita mais escritas
	next    *chunk      // ponteiro para o próximo chunk
	entries [900]*entry // os registros neste chunk
}

// init inicializa os próximos chunks
func (c *chunk) init(depth int) {
	if depth > 0 {

		if c.next == nil {
			c.mu.Lock()
			if c.next == nil {
				c.next = &chunk{}
				c.next.id = c.id + 1
			}
			c.mu.Unlock()
		}

		depth--
		if depth > 0 {
			c.next.init(depth)
		}
	}
}

// ttl obtém a idade do último log inserido neste chunk
func (c *chunk) ttl() time.Duration {
	if c.isEmpty() {
		return 0
	}
	index := c.write - 1
	if index < 0 {
		// não houve tempo de escrever ainda o primeiro item
		return 0
	}

	return time.Since(c.entries[index].time)
}

// full indica que esse chunk está cheio e o flush pode ser realizado
func (c *chunk) isFull() bool {
	return c.write == 900 || (c.book > 0 && c.write == c.book && c.locked.Load())
}

// empty indica que não houve tentativa de escrita
func (c *chunk) isEmpty() bool {
	return c.book == 0
}

// bloqueia a escrita nesse chunk.
// A partir desse ponto as escritas irão ocorrer no próximo chunk
// Este chunk estará pronto para o flush após a confirmação das escritas
func (c *chunk) lock() {
	c.locked.Store(true)
}

// list obtém a lista do registros escritos
func (c *chunk) list() []*entry {
	return c.entries[:c.write]
}

// offer tenta escrever o registro nesse chunk.
// Retorna o chunk que aceitou o registro.
// Se o chunk que aceitou o registro for o mesmo, retorna true no segundo parametro
func (c *chunk) offer(e *entry) (into *chunk, accepted bool) {
	if c.locked.Load() {
		c.init(1) // garante que o próximo foi iniciado
		oid, _ := c.next.offer(e)
		return oid, false
	}

	index := (atomic.AddInt32(&c.book, 1) - 1)
	if index > 899 {
		// chunk está cheio
		c.init(1) // garante que o próximo foi iniciado
		oid, _ := c.next.offer(e)
		return oid, false
	}

	// write safe
	c.entries[index] = e
	atomic.AddInt32(&c.write, 1)

	return c, true
}
