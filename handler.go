package litelog

import (
	"bytes"
	"context"
	"log/slog"
	"sync"
)

const bbcap = 1 << 16

// abriga a lógica de escrita do log
type writer struct {
	buffer  *bytes.Buffer
	handler slog.Handler
}

// @TODO: usar o https://github.com/phuslu/log?
type handler struct {
	writers  sync.Pool
	ingester *ingester
}

func newHandler(ingester *ingester, opts *slog.HandlerOptions) *handler {
	if opts == nil {
		opts = &slog.HandlerOptions{
			AddSource: false,
			Level:     slog.LevelInfo,
		}
	}

	h := &handler{ingester: ingester}

	// @TODO: Custom writer
	h.writers.New = func() any {
		buf := bytes.NewBuffer(make([]byte, 0, 1024))
		return &writer{
			buffer:  buf,
			handler: slog.NewJSONHandler(buf, opts),
		}
	}

	return h
}

func (h *handler) Handle(ctx context.Context, r slog.Record) error {
	w := h.writers.Get().(*writer)
	w.buffer.Reset()

	r.Time = r.Time.UTC()
	if err := w.handler.Handle(ctx, r); err != nil {
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
