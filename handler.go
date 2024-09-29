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
	writers  sync.Pool
	ingester *ingesterImpl
}

func newHandler(ingester *ingesterImpl, config *HandlerConfig) *handler {
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
		ingester: ingester,
	}

	encoder := config.Encoder

	h.writers.New = func() any {
		buf := bytes.NewBuffer(make([]byte, 0, 1024))
		return &writer{buffer: buf, encoder: encoder(buf, config.Options)}
	}

	return h
}

func (h *handler) Handle(ctx context.Context, r slog.Record) error {
	w := h.writers.Get().(*writer)
	w.buffer.Reset()

	r.Time = r.Time.UTC()
	if err := w.encoder.Handle(ctx, r); err != nil {
		return err
	}

	if w.buffer.Len() > 0 {
		if err := h.ingester.ingest(r.Time, int8(r.Level), bytes.Clone(w.buffer.Bytes())); err != nil {
			return err
		}
	}

	if w.buffer.Cap() <= bbcap {
		h.writers.Put(w)
	}

	return nil
}

func (h *handler) Enabled(ctx context.Context, level slog.Level) bool {
	// @TODO: implementar
	return true
}

func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	// @TODO: implementar
	return h
}

func (h *handler) WithGroup(name string) slog.Handler {
	// @TODO: implementar
	return h
}
