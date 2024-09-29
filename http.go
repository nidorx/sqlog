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
	devDemo = true // running "/demo/main.go"
	//go:embed web/*
	webFiles embed.FS
)

func (l *logImpl) HttpHandler() http.Handler {

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
func (l *logImpl) ServeHTTPTicks(w http.ResponseWriter, r *http.Request) {
	var (
		q           = r.URL.Query()
		expr        = q.Get("expr")
		level       = q.Get("level")
		levels      map[string]bool
		maxResult   = getInt(q, "limit")
		epochStart  = getInt64(q, "epoch")
		intervalSec = getInt(q, "interval")
	)

	if level != "" {
		levels = make(map[string]bool)
		for _, v := range strings.Split(level, ",") {
			levels[v] = true
		}
	}

	list, err := l.storage.listTicks(expr, levels, epochStart, intervalSec, maxResult)
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(400)
		json.NewEncoder(w).Encode(map[string]any{
			"error": err.Error(),
		})
	} else {
		json.NewEncoder(w).Encode(list)
	}
}

// ServeHTTPEntries entries api. (seek method or keyset pagination {before, after})
func (l *logImpl) ServeHTTPEntries(w http.ResponseWriter, r *http.Request) {
	var (
		q            = r.URL.Query()
		expr         = q.Get("expr")
		level        = q.Get("level")
		levels       map[string]bool
		direction    = q.Get("dir") // before, after
		epochEnd     = getInt64(q, "epoch")
		nanosEnd     = getInt(q, "nanos")
		limitResults = getInt(q, "limit")
	)

	if level != "" {
		levels = make(map[string]bool)
		for _, v := range strings.Split(level, ",") {
			levels[v] = true
		}
	}

	entries, err := l.storage.listEntries(expr, levels, direction, epochEnd, nanosEnd, limitResults)
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(400)
		json.NewEncoder(w).Encode(map[string]any{
			"error": err.Error(),
		})
	} else {
		json.NewEncoder(w).Encode(entries)
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
