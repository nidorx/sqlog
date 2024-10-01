package sqlog

import (
	"sync"
	"sync/atomic"
	"time"
)

var epoch = time.Time{}.UTC()

// Entry representa uma entrada de log formatada
type Entry struct {
	Time    time.Time
	Level   int8
	Content []byte
}

// Chunk abriga dados de até 900 logs que serão persistidos na base
type Chunk struct {
	mu      sync.Mutex
	id      int32       // O identificador desse chunk
	cap     int32       // O tamanho do batch configurado
	book    int32       // Quantidade de escritas agendadas nesse chunk
	write   int32       // Quantidade de escritas realizadas
	retries uint        // Quantas vezes tentou persistir na base
	locked  atomic.Bool // Indica que este chunk não aceita mais escritas
	next    *Chunk      // Ponteiro para o próximo chunk
	entries [900]*Entry // Os registros neste chunk
}

// Depth obtém a quantidade de chunks não vazios
func (c *Chunk) Depth() int {
	if c.Empty() {
		return 0
	}
	if c.next == nil {
		return 1
	}
	return 1 + c.next.Depth()
}

// Init inicializa os próximos chunks
func (c *Chunk) Init(depth uint8) {
	if depth > 0 {

		if c.next == nil {
			c.mu.Lock()
			if c.next == nil {
				c.next = &Chunk{cap: c.cap}
				c.next.id = c.id + 1
			}
			c.mu.Unlock()
		}

		depth--
		if depth > 0 {
			c.next.Init(depth)
		}
	}
}

// First obtém o time do primeiro registro nesse chunk
func (c *Chunk) First() time.Time {
	index := c.write - 1
	if index < 0 || c.Empty() {
		return epoch
	}
	return c.entries[index].Time
}

// Last obtém o time do último registro desse chunk
func (c *Chunk) Last() time.Time {
	index := c.write - 1
	if index < 0 || c.Empty() {
		return epoch
	}
	return c.entries[0].Time
}

// TTL obtém a idade do último log inserido neste chunk
func (c *Chunk) TTL() time.Duration {
	last := c.Last()
	if last.IsZero() {
		return 0
	}
	return time.Since(last)
}

// full indica que esse chunk está cheio e o flush pode ser realizado
func (c *Chunk) Full() bool {
	return c.write == c.cap || (c.book > 0 && c.write == c.book && c.locked.Load())
}

// empty indica que não houve tentativa de escrita
func (c *Chunk) Empty() bool {
	return c.book == 0
}

// bloqueia a escrita nesse chunk.
// A partir desse ponto as escritas irão ocorrer no próximo chunk
// Este chunk estará pronto para o flush após a confirmação das escritas
func (c *Chunk) Lock() {
	c.locked.Store(true)
}

// List obtém a lista do registros escritos
func (c *Chunk) List() []*Entry {
	return c.entries[:c.write]
}

// Put tenta escrever o registro nesse chunk.
// Retorna o chunk que aceitou o registro.
// Se o chunk que aceitou o registro for o mesmo, retorna true no segundo parametro
func (c *Chunk) Put(e *Entry) (into *Chunk, isFull bool) {
	if c.locked.Load() {
		c.Init(1) // garante que o próximo foi iniciado
		oid, _ := c.next.Put(e)
		return oid, true
	}

	index := (atomic.AddInt32(&c.book, 1) - 1)
	if index > (c.cap - 1) {
		// chunk está cheio
		c.Init(1) // garante que o próximo foi iniciado
		oid, _ := c.next.Put(e)
		return oid, true
	}

	// write safe
	c.entries[index] = e
	atomic.AddInt32(&c.write, 1)

	return c, false
}
