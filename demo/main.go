package main

import (
	"embed"
	"io/fs"
	"litelog"
	"log/slog"
	"net/http"
	"os"
	"path"
	"strings"
)

var (
	sqlog litelog.Log

	dev = true

	//go:embed public/*
	webFiles embed.FS
)

func init() {
	if log, err := litelog.New(&litelog.Config{}); err != nil {
		panic(err)
	} else {
		sqlog = log
		slog.SetDefault(slog.New(log.Handler()))
	}
}

func main() {

	sqlogHttpHandler := sqlog.HttpHandler()
	staticHttpHandler := getStaticHandler()

	httpHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path

		if idx := strings.LastIndex(p, "/logs"); idx >= 0 {

			sqlogHttpHandler.ServeHTTP(w, r)

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
			// switch path.Ext(p) {
			// case ".html", ".css", ".js":
			// 	p = path.Base(p)
			// default:
			// 	p = ""
			// }

			// r.URL.Path = p
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
