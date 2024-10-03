package sqlog

import (
	"log/slog"
	"net/http"
	"os"
	"sync"
)

type Config struct {
	Storage  Storage
	Handler  *HandlerConfig
	Ingester *IngesterConfig
}

// Log SQLog interface
type Log interface {

	// Stop terminates any ongoing logging operations. It should be called
	// to release resources and stop log collection or processing.
	Stop()

	// Handler returns the primary log handler
	Handler() slog.Handler

	// Fanout distributes logs to multiple slog.Handler instances in parallel.
	// This allows logs to be processed by several handlers simultaneously.
	Fanout(...slog.Handler)

	// Ticks api
	Ticks(*TicksInput) (*Output, error)

	// Entries api
	Entries(*EntriesInput) (*Output, error)

	// Result scheduled result api
	Result(taskId int32) (*Output, error)

	// Cancel scheduled result
	Cancel(taskId int32) error

	// HttpHandler returns an http.Handler responsible for handling
	// HTTP requests related to the api
	HttpHandler() http.Handler

	// ServeHTTPTicks handles HTTP requests for Ticks api
	ServeHTTPTicks(w http.ResponseWriter, r *http.Request)

	// ServeHTTPEntries handles HTTP requests for Entries api
	ServeHTTPEntries(w http.ResponseWriter, r *http.Request)

	// ServeHTTPEntries handles HTTP requests for scheduled result api
	ServeHTTPResult(w http.ResponseWriter, r *http.Request)

	// ServeHTTPEntries handles HTTP requests to cancel scheduled result api
	ServeHTTPCancel(w http.ResponseWriter, r *http.Request)
}

type sqlog struct {
	close    sync.Once
	config   *Config
	handler  *handler
	storage  Storage
	ingester *Ingester
}

func New(config *Config) (*sqlog, error) {
	if config == nil {
		config = &Config{}
	}

	storage := config.Storage
	if storage == nil {
		storage = &DummyStorage{}
	}

	ingester, err := NewIngester(config.Ingester, storage)
	if err != nil {
		return nil, err
	}

	return &sqlog{
		config:   config,
		storage:  storage,
		ingester: ingester,
		handler:  newHandler(ingester, config.Handler),
	}, nil
}

func (l *sqlog) Handler() slog.Handler {
	return l.handler
}

func (l *sqlog) Fanout(handlers ...slog.Handler) {
	l.handler.fanout(handlers...)
}

func (l *sqlog) Stop() {
	l.close.Do(func() {
		if slog.Default().Handler() == l.handler {
			// we will no longer be able to write the log
			slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
		}
		if err := l.ingester.Close(); err != nil {
			slog.Warn(
				"[sqlog] error closing",
				slog.Any("error", err),
			)
		}
	})
}
