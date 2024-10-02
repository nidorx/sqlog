package main

import (
	"embed"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/nidorx/sqlog/sqlite"

	"github.com/nidorx/sqlog"
)

var (
	dev = true

	log sqlog.Log

	//go:embed public/*
	webFiles embed.FS
)

func init() {

	// SQLite storage
	storage, err := sqlite.New(&sqlite.Config{
		Dir:             "logs",
		Prefix:          "demo",
		MaxFilesizeMB:   1,
		TotalSizeCapMB:  5,
		MaxOpenedDB:     2,
		MaxRunningTasks: 5,
		CloseIdleSec:    10,
	})
	if err != nil {
		panic(err)
	}

	config := &sqlog.Config{
		Ingester: &sqlog.IngesterConfig{
			Chunks:    3,
			ChunkSize: 20,
		},
		Storage: storage,
	}

	if l, err := sqlog.New(config); err != nil {
		panic(err)
	} else {
		log = l
		slog.SetDefault(slog.New(l.Handler()))
	}
}

func main() {
	defer log.Stop()

	logHttpHandler := log.HttpHandler()
	staticHttpHandler := getStaticHandler()

	httpHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path

		if idx := strings.LastIndex(p, "/logs"); idx >= 0 {

			logHttpHandler.ServeHTTP(w, r)

		} else if p == "/debug" {

			l := slog.LevelInfo
			args := []any{}
			msg := "debug log form screen"
			for k, v := range r.URL.Query() {
				if k == "msg" {
					msg = strings.Join(v, ",")
				} else if k == "level" {
					switch strings.Join(v, ",") {
					case "INFO":
						l = slog.LevelInfo
					case "WARN":
						l = slog.LevelWarn
					case "ERROR":
						l = slog.LevelError
					}
				} else {
					args = append(args, slog.Any(k, strings.Join(v, ",")))
				}
			}

			slog.Log(r.Context(), l, msg, args...)

		} else {
			staticHttpHandler.ServeHTTP(w, r)
		}
	})

	http.ListenAndServe(":8080", httpHandler)
}

func getStaticHandler() http.Handler {
	var wfs http.FileSystem

	if dev {
		PWD, _ := os.Getwd()
		wfs = http.Dir(path.Join(PWD, "/public"))
	} else {
		subfs, _ := fs.Sub(webFiles, "public")
		wfs = http.FS(subfs)
	}
	return http.FileServer(wfs)
}
