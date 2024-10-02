package sqlog

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"sync"
)

const bbcap = 1 << 16 // 65536

type Encoder func(w io.Writer, opts *slog.HandlerOptions) slog.Handler

type HandlerConfig struct {
	Encoder Encoder // O slog.Handler usado para o encode do json
	Options *slog.HandlerOptions
}

// abriga a lógica de escrita do log
type writer struct {
	buffer  *bytes.Buffer
	encoder slog.Handler
}

type handler struct {
	mu       sync.Mutex
	writers  sync.Pool
	ingester *Ingester
	config   *HandlerConfig
	handlers []slog.Handler // Fanout
}

func newHandler(ingester *Ingester, config *HandlerConfig) *handler {
	if config == nil {
		config = &HandlerConfig{}
	}

	if config.Encoder == nil {
		config.Encoder = func(w io.Writer, opts *slog.HandlerOptions) slog.Handler {
			return slog.NewJSONHandler(w, opts)
		}
	}

	if config.Options == nil {
		config.Options = &slog.HandlerOptions{
			AddSource: false,
			Level:     slog.LevelInfo,
		}
	}

	h := &handler{
		config:   config,
		ingester: ingester,
	}

	encoder := config.Encoder

	h.writers.New = func() any {
		buf := bytes.NewBuffer(make([]byte, 0, 1024))
		return &writer{buffer: buf, encoder: encoder(buf, config.Options)}
	}

	return h
}

func (h *handler) fanout(handlers ...slog.Handler) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.handlers = append(h.handlers, handlers...)
}

func (h *handler) Handle(ctx context.Context, record slog.Record) error {

	for _, h2 := range h.handlers {
		if h2.Enabled(ctx, record.Level) {
			h2.Handle(ctx, record.Clone())
		}
	}

	if h.Enabled(ctx, record.Level) {
		w := h.writers.Get().(*writer)
		w.buffer.Reset()

		record.Time = record.Time.UTC()
		if err := w.encoder.Handle(ctx, record); err != nil {
			return err
		}

		if w.buffer.Len() > 0 {
			if err := h.ingester.Ingest(record.Time, int8(record.Level), bytes.Clone(w.buffer.Bytes())); err != nil {
				return err
			}
		}

		if w.buffer.Cap() <= bbcap {
			h.writers.Put(w)
		}
	}

	return nil
}

// enabled reports whether l is greater than or equal to the
// minimum level.
func (h *handler) Enabled(ctx context.Context, l slog.Level) bool {
	minLevel := slog.LevelInfo
	if h.config.Options.Level != nil {
		minLevel = h.config.Options.Level.Level()
	}
	if l >= minLevel {
		return true
	}

	for _, h2 := range h.handlers {
		if h2.Enabled(ctx, l) {
			return true
		}
	}

	return false
}

// WithAttrs returns a new Handler whose attributes consist of
// both the receiver's attributes and the arguments.
func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	o := &handler{
		config:   h.config,
		ingester: h.ingester,
	}
	parentPool := &h.writers
	o.writers.New = func() any {
		w := parentPool.Get().(*writer)
		w.encoder = w.encoder.WithAttrs(attrs)
		return w
	}

	for _, h2 := range h.handlers {
		o.handlers = append(o.handlers, h2.WithAttrs(attrs))
	}
	return o
}

// WithGroup returns a new Handler with the given group appended to
// the receiver's existing groups.
func (h *handler) WithGroup(name string) slog.Handler {
	o := &handler{
		config:   h.config,
		ingester: h.ingester,
	}
	parentPool := &h.writers
	o.writers.New = func() any {
		w := parentPool.Get().(*writer)
		w.encoder = w.encoder.WithGroup(name)
		return w
	}

	for _, h2 := range h.handlers {
		o.handlers = append(o.handlers, h2.WithGroup(name))
	}
	return o
}
