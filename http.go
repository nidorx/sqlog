package sqlog

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
)

var (
	// running "/demo/main.go"
	devDemo = false

	//go:embed web/*
	webFiles embed.FS
)

func (l *sqlog) HttpHandler() http.Handler {

	var wfs http.FileSystem

	if devDemo {
		pwd, _ := os.Getwd()
		pwd = strings.TrimSuffix(pwd, "demo") + "web"
		wfs = http.Dir(pwd)
	} else {
		subfs, _ := fs.Sub(webFiles, "web")
		wfs = http.FS(subfs)
	}

	staticHandler := http.FileServer(wfs)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path

		if idx := strings.LastIndex(p, "/api/"); idx >= 0 {
			p = p[idx+len("/api/"):]
			switch p {
			case "ticks":
				l.ServeHTTPTicks(w, r)
			case "entries":
				l.ServeHTTPEntries(w, r)
			}
		} else {
			switch path.Ext(p) {
			case ".html", ".css", ".js":
				p = path.Base(p)
			default:
				p = ""
			}

			r.URL.Path = p
			staticHandler.ServeHTTP(w, r)
		}
	})
}

// ServeHTTPTicks tick api
func (l *sqlog) ServeHTTPTicks(w http.ResponseWriter, r *http.Request) {
	var (
		q      = r.URL.Query()
		levels []string
	)

	if level := q.Get("level"); level != "" {
		levels = strings.Split(level, ",")
	}

	list, err := l.Ticks(&TicksInput{
		Expr:        q.Get("expr"),
		Level:       levels,
		EpochEnd:    getInt64(q, "epoch"),
		IntervalSec: getInt(q, "interval"),
		MaxResult:   getInt(q, "limit"),
	})
	sendJson(w, list, err)
}

// ServeHTTPEntries entries api. (seek method or keyset pagination {before, after})
func (l *sqlog) ServeHTTPEntries(w http.ResponseWriter, r *http.Request) {
	var (
		q      = r.URL.Query()
		levels []string
	)

	if level := q.Get("level"); level != "" {
		levels = strings.Split(level, ",")
	}

	entries, err := l.Entries(&EntriesInput{
		Expr:       q.Get("expr"),
		Level:      levels,
		Direction:  q.Get("dir"), // before, after
		EpochStart: getInt64(q, "epoch"),
		NanosStart: getInt(q, "nanos"),
		MaxResult:  getInt(q, "limit"),
	})
	sendJson(w, entries, err)
}

func sendJson(w http.ResponseWriter, data any, err error) {
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(400)
		json.NewEncoder(w).Encode(map[string]any{
			"error": err.Error(),
		})
	} else {
		json.NewEncoder(w).Encode(data)
	}
}

func getInt(q url.Values, key string) int {
	v, _ := strconv.Atoi(q.Get(key))
	return v
}

func getInt64(q url.Values, key string) int64 {
	v, _ := strconv.ParseInt(q.Get(key), 10, 64)
	return v
}
