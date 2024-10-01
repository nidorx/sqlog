package sqlog

import (
	"log/slog"
	"net/http"
	"os"
)

type Config struct {
	Storage  Storage
	Handler  *HandlerConfig
	Ingester *IngesterConfig
}

type Log interface {
	Close() error
	Handler() slog.Handler
	HttpHandler() http.Handler
	Ticks(*TicksInput) (*Output, error)
	Entries(*EntriesInput) (*Output, error)
	ServeHTTPTicks(w http.ResponseWriter, r *http.Request)
	ServeHTTPEntries(w http.ResponseWriter, r *http.Request)
}

type sqlog struct {
	config   *Config
	handler  slog.Handler
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

	// storage, err := newStorage(config.Storage)
	// if err != nil {
	// 	return nil, err
	// }

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

func (l *sqlog) Close() error {
	if slog.Default().Handler() == l.handler {
		// we will no longer be able to write the log
		slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	}
	return l.ingester.Close()
}
