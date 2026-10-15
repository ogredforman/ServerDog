// Package web serves the embedded static site and published data over HTTP (SPEC §8 HTTP serving).
package web

import (
	"embed"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ogredforman/ServerDog/internal/clock"
	"github.com/ogredforman/ServerDog/internal/publish"
)

//go:embed static
var static embed.FS

// Cache-Control values from SPEC §7.4 and §8 HTTP serving.
const (
	cacheIndex    = "public, max-age=60, s-maxage=300"
	cacheNotFound = "no-store"
	minDataMaxAge = 5 // seconds; floor for servers.json max-age
)

// Site is the site's HTTP handler. Every response sets an explicit Cache-Control.
type Site struct {
	clk     clock.Clock
	dataDir string
	mux     *http.ServeMux

	mu        sync.Mutex
	published bool
	next      time.Time     // nextUpdateAt of the last publish
	refresh   time.Duration // refreshSeconds of the last publish
}

// New returns a Site serving servers.json from dataDir. servers.json returns 404 until
// Published is called, so a file left over from an earlier run is never served as current.
func New(clk clock.Clock, dataDir string) *Site {
	s := &Site{clk: clk, dataDir: dataDir, mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /{$}", serveIndex)
	s.mux.HandleFunc("GET /data/"+publish.FileName, s.serveData)
	s.mux.HandleFunc("/", notFound)
	return s
}

// Published records that f has been written to dataDir, so it can be served with a
// Cache-Control that expires at its nextUpdateAt.
func (s *Site) Published(f publish.File) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.published = true
	s.next = time.Unix(f.NextUpdateAt, 0)
	s.refresh = time.Duration(f.RefreshSeconds) * time.Second
}

func (s *Site) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Site) serveData(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	published, next, refresh := s.published, s.next, s.refresh
	s.mu.Unlock()
	if !published {
		notFound(w, r)
		return
	}
	f, err := os.Open(filepath.Join(s.dataDir, publish.FileName))
	if err != nil {
		notFound(w, r)
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		w.Header().Set("Cache-Control", cacheNotFound)
		http.Error(w, "data unavailable", http.StatusInternalServerError)
		return
	}
	n := maxAge(s.clk.Now(), next, refresh)
	w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d, s-maxage=%d", n, n))
	w.Header().Set("Content-Type", "application/json")
	http.ServeContent(w, r, publish.FileName, fi.ModTime(), f)
}

// maxAge is the whole seconds from now until next, kept between 5 and refresh (SPEC §7.4).
func maxAge(now, next time.Time, refresh time.Duration) int64 {
	n := int64(next.Sub(now) / time.Second)
	if hi := int64(refresh / time.Second); n > hi {
		n = hi
	}
	if n < minDataMaxAge {
		n = minDataMaxAge
	}
	return n
}

func notFound(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", cacheNotFound)
	http.NotFound(w, r)
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
