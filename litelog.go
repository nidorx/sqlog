package litelog

import (
	"log/slog"
	"net/http"
)

type Config struct {
	Path          string            // Database folder
	SQLiteOptions map[string]string // https://github.com/mattn/go-sqlite3?tab=readme-ov-file#connection-string
	MaxCapacity   int
	BatchSize     int
	Options       *slog.HandlerOptions
}

type Log interface {
	Close() error
	Handler() slog.Handler
	HttpHandler() http.Handler
	ServeHTTPTicks(w http.ResponseWriter, r *http.Request)
	ServeHTTPEntries(w http.ResponseWriter, r *http.Request)
}

type litelog struct {
	config   *Config
	handler  slog.Handler
	ingester *ingester
	store    *store
}

func New(config *Config) (Log, error) {

	if len(config.SQLiteOptions) == 0 {
		config.SQLiteOptions = SQLiteOptionsDefault
	}

	if config.Path == "" {
		config.Path = "./logs"
	}

	if config.BatchSize <= 0 {
		config.BatchSize = 240
	}

	if config.MaxCapacity <= 0 {
		config.MaxCapacity = 1048576 // 1GB
	}

	store, err := newStore(config.Path, config.BatchSize, config.SQLiteOptions)
	if err != nil {
		return nil, err
	}

	ingester, err := newIngester(config, store)
	if err != nil {
		return nil, err
	}

	return &litelog{
		config:   config,
		ingester: ingester,
		store:    store,
		handler:  newHandler(ingester, config.Options),
	}, nil
}

func (l *litelog) Handler() slog.Handler {
	return l.handler
}

func (l *litelog) Close() error {
	l.ingester.close()
	l.store.close()
	return nil
}
