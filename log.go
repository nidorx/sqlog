package sqlog

import (
	"log/slog"
	"net/http"
)

type Config struct {
	Storage   *StorageConfig
	Handler   *HandlerConfig
	BatchSize int
}

type Log interface {
	Close() error
	Handler() slog.Handler
	HttpHandler() http.Handler
	ServeHTTPTicks(w http.ResponseWriter, r *http.Request)
	ServeHTTPEntries(w http.ResponseWriter, r *http.Request)
}

type logImpl struct {
	config   *Config
	handler  slog.Handler
	storage  *storageImpl
	ingester *ingesterImpl
}

func New(config *Config) (Log, error) {
	if config == nil {
		config = &Config{}
	}

	if config.BatchSize <= 0 {
		config.BatchSize = 240
	}

	storage, err := newStorage(config.Storage)
	if err != nil {
		return nil, err
	}

	ingester, err := newIngester(config, storage)
	if err != nil {
		return nil, err
	}

	return &logImpl{
		config:   config,
		storage:  storage,
		ingester: ingester,
		handler:  newHandler(ingester, config.Handler),
	}, nil
}

func (l *logImpl) Handler() slog.Handler {
	return l.handler
}

func (l *logImpl) Close() error {
	l.ingester.close()
	l.storage.close()
	return nil
}
