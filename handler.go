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
	Encoder Encoder // The slog.Handler used for JSON encoding
	Options *slog.HandlerOptions
}

// Handles the logic for writing logs
type writer struct {
	buffer  *bytes.Buffer
	encoder slog.Handler
}

type handler struct {
	mu       sync.Mutex
	writers  sync.Pool
	ingester Ingester
	config   *HandlerConfig
	handlers []slog.Handler // Fanout handlers
}

// Creates a new handler with the given ingester and configuration
func newHandler(ingester Ingester, config *HandlerConfig) *handler {
	if config == nil {
		config = &HandlerConfig{}
	}

	// Sets the default encoder to JSON if none is provided
	if config.Encoder == nil {
		config.Encoder = func(w io.Writer, opts *slog.HandlerOptions) slog.Handler {
			return slog.NewJSONHandler(w, opts)
		}
	}

	// Sets default handler options if none are provided
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

	// Creates a new writer with a buffer and encoder
	h.writers.New = func() any {
		buf := bytes.NewBuffer(make([]byte, 0, 1024))
		return &writer{buffer: buf, encoder: encoder(buf, config.Options)}
	}

	return h
}

// Adds handlers to the fanout list
func (h *handler) fanout(handlers ...slog.Handler) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.handlers = append(h.handlers, handlers...)
}

// Processes a log record, encoding it and sending it to the ingester
func (h *handler) Handle(ctx context.Context, record slog.Record) error {

	// Sends the log record to fanout handlers
	for _, h2 := range h.handlers {
		if h2.Enabled(ctx, record.Level) {
			h2.Handle(ctx, record.Clone())
		}
	}

	// Processes the log if it's enabled for this handler
	if h.enabledSelf(record.Level) {
		w := h.writers.Get().(*writer)
		w.buffer.Reset()

		// Ensures the log time is in UTC
		record.Time = record.Time.UTC()
		if err := w.encoder.Handle(ctx, record); err != nil {
			return err
		}

		// Ingests the log if there is data to write
		if w.buffer.Len() > 0 {
			if err := h.ingester.Ingest(record.Time, int8(record.Level), bytes.Clone(w.buffer.Bytes())); err != nil {
				return err
			}
		}

		// Reuses the writer if its buffer capacity is below the limit
		if w.buffer.Cap() <= bbcap {
			h.writers.Put(w)
		}
	}

	return nil
}

// enabledSelf checks if the log level is greater than or equal to the minimum level
func (h *handler) enabledSelf(l slog.Level) bool {
	minLevel := slog.LevelInfo
	if h.config.Options.Level != nil {
		minLevel = h.config.Options.Level.Level()
	}
	return l >= minLevel
}

// Enabled checks whether the log level meets the minimum requirement
func (h *handler) Enabled(ctx context.Context, l slog.Level) bool {
	if h.enabledSelf(l) {
		return true
	}

	// Also check if any fanout handler is enabled
	for _, h2 := range h.handlers {
		if h2.Enabled(ctx, l) {
			return true
		}
	}

	return false
}

// WithAttrs returns a new Handler that includes the provided attributes
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

	// Propagates attributes to fanout handlers
	for _, h2 := range h.handlers {
		o.handlers = append(o.handlers, h2.WithAttrs(attrs))
	}
	return o
}

// WithGroup returns a new Handler with the provided group name
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

	// Propagates the group to fanout handlers
	for _, h2 := range h.handlers {
		o.handlers = append(o.handlers, h2.WithGroup(name))
	}
	return o
}
