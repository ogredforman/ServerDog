// Package web serves the embedded static site and published data over HTTP (SPEC §8 HTTP serving).
package web

import (
	"embed"
	"net/http"
)

//go:embed static
var static embed.FS

// Cache-Control values from SPEC §8 HTTP serving.
const (
	cacheIndex    = "public, max-age=60, s-maxage=300"
	cacheNotFound = "no-store"
)

// Handler returns the site's HTTP handler. Every response sets an explicit Cache-Control.
func Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", serveIndex)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", cacheNotFound)
		http.NotFound(w, r)
	})
	return mux
}

func serveIndex(w http.ResponseWriter, r *http.Request) {
	body, err := static.ReadFile("static/index.html")
	if err != nil {
		w.Header().Set("Cache-Control", cacheNotFound)
		http.Error(w, "index unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", cacheIndex)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(body)
}
