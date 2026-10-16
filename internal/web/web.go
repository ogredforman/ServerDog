// Package web serves the embedded static site and published data over HTTP (SPEC §8 HTTP serving).
package web

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ogredforman/ServerDog/internal/clock"
	"github.com/ogredforman/ServerDog/internal/publish"
)

//go:embed static
var static embed.FS

// assetTypes are the static asset extensions served, with a fixed Content-Type so the
// result doesn't depend on the host's MIME registry.
var assetTypes = map[string]string{
	".js":    "text/javascript; charset=utf-8",
	".css":   "text/css; charset=utf-8",
	".woff2": "font/woff2",
	".txt":   "text/plain; charset=utf-8", // the font license
}

// assetDirs are the embedded directories served, each under its own URL prefix ("/js/…").
var assetDirs = []string{"js", "css", "fonts"}

// Cache-Control values from SPEC §7.4 and §8 HTTP serving.
const (
	cacheIndex     = "public, max-age=60, s-maxage=300"
	cacheAsset     = cacheIndex // assets requested without their current ?v= hash
	cacheVersioned = "public, max-age=31536000, immutable"
	cacheNoStore   = "no-store"
	minDataMaxAge  = 5 // seconds; floor for servers.json max-age
)

// importMapMarker in index.html is replaced at startup with an import map that versions
// every JS module, so relative imports between modules get ?v= URLs too.
const importMapMarker = "<!-- importmap -->"

// Site is the site's HTTP handler. Every response sets an explicit Cache-Control.
type Site struct {
	clk     clock.Clock
	dataDir string
	mux     *http.ServeMux
	index   []byte            // index.html with ?v= asset URLs and the import map filled in
	hashes  map[string]string // embedded asset name ("static/js/app.js") → content hash

	mu         sync.Mutex
	published  bool
	next       time.Time     // nextUpdateAt of the last publish
	refresh    time.Duration // refreshSeconds of the last publish
	snapshotAt time.Time     // snapshotAt of the last publish
	stale      bool          // stale flag of the last publish
}

// New returns a Site serving servers.json from dataDir. servers.json returns 404 until
// Published is called, so a file left over from an earlier run is never served as current.
// Asset hashes and the served index.html are computed once here from the embedded files;
// it panics only if those embedded files are broken, which the package tests catch.
func New(clk clock.Clock, dataDir string) *Site {
	hashes, err := assetHashes(static)
	if err != nil {
		panic(fmt.Sprintf("web: hash embedded assets: %v", err))
	}
	raw, err := static.ReadFile("static/index.html")
	if err != nil {
		panic(fmt.Sprintf("web: read embedded index.html: %v", err))
	}
	index, err := versionIndex(raw, hashes)
	if err != nil {
		panic(fmt.Sprintf("web: version index.html: %v", err))
	}
	s := &Site{clk: clk, dataDir: dataDir, mux: http.NewServeMux(), index: index, hashes: hashes}
	s.mux.HandleFunc("GET /{$}", s.serveIndex)
	s.mux.HandleFunc("GET /data/"+publish.FileName, s.serveData)
	for _, dir := range assetDirs {
		s.mux.HandleFunc("GET /"+dir+"/{path...}", s.serveAsset)
	}
	s.mux.HandleFunc("GET /healthz", s.serveHealth)
	s.mux.HandleFunc("/", notFound)
	return s
}

// Published records that f has been written to dataDir, so it can be served with a
// Cache-Control that expires at its nextUpdateAt, and reported by /healthz.
func (s *Site) Published(f publish.File) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.published = true
	s.next = time.Unix(f.NextUpdateAt, 0)
	s.refresh = time.Duration(f.RefreshSeconds) * time.Second
	s.snapshotAt = time.Unix(f.SnapshotAt, 0)
	s.stale = f.Stale
}

func (s *Site) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Default for responses the mux writes itself, such as its clean-path redirects.
	// Handlers replace it.
	w.Header().Set("Cache-Control", cacheNoStore)
	s.mux.ServeHTTP(w, r)
}

// assetHashes returns a short sha256 content hash for every servable embedded asset.
func assetHashes(fsys fs.FS) (map[string]string, error) {
	hashes := map[string]string{}
	for _, dir := range assetDirs {
		root := "static/" + dir
		err := fs.WalkDir(fsys, root, func(name string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if _, ok := assetTypes[path.Ext(name)]; !ok || d.IsDir() {
				return nil
			}
			body, err := fs.ReadFile(fsys, name)
			if err != nil {
				return fmt.Errorf("read %s: %w", name, err)
			}
			sum := sha256.Sum256(body)
			hashes[name] = hex.EncodeToString(sum[:])[:12]
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("walk %s: %w", root, err)
		}
	}
	return hashes, nil
}

// versionIndex rewrites each quoted asset URL in index.html to its ?v= form and replaces
// importMapMarker with an import map from every JS module URL to its versioned URL.
func versionIndex(index []byte, hashes map[string]string) ([]byte, error) {
	html := string(index)
	if !strings.Contains(html, importMapMarker) {
		return nil, fmt.Errorf("missing %q", importMapMarker)
	}
	imports := map[string]string{}
	for name, hash := range hashes {
		url := strings.TrimPrefix(name, "static")
		versioned := url + "?v=" + hash
		html = strings.ReplaceAll(html, `"`+url+`"`, `"`+versioned+`"`)
		if path.Ext(name) == ".js" {
			imports[url] = versioned
		}
	}
	// json.Marshal sorts map keys, so the output is stable.
	m, err := json.Marshal(map[string]any{"imports": imports})
	if err != nil {
		return nil, fmt.Errorf("encode import map: %w", err)
	}
	html = strings.Replace(html, importMapMarker, `<script type="importmap">`+string(m)+`</script>`, 1)
	return []byte(html), nil
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
		w.Header().Set("Cache-Control", cacheNoStore)
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

// serveHealth reports the age of the last published snapshot (null before the first
// publish) and its stale flag. Never cached.
func (s *Site) serveHealth(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	published, snapshotAt, stale := s.published, s.snapshotAt, s.stale
	s.mu.Unlock()
	var age *int64
	if published {
		n := max(int64(s.clk.Now().Sub(snapshotAt)/time.Second), 0)
		age = &n
	}
	body, err := json.Marshal(struct {
		SnapshotAgeSeconds *int64 `json:"snapshotAgeSeconds"`
		Stale              bool   `json:"stale"`
	}{age, stale})
	if err != nil {
		w.Header().Set("Cache-Control", cacheNoStore)
		http.Error(w, "health unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", cacheNoStore)
	w.Header().Set("Content-Type", "application/json")
	w.Write(body)
}

func notFound(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", cacheNoStore)
	http.NotFound(w, r)
}

func (s *Site) serveIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", cacheIndex)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(s.index)
}

// serveAsset serves a file of a known type from the embedded static FS. Directories and
// anything else return 404. Only a request carrying the file's current ?v= hash is cached
// as immutable, so a versioned URL from an older index.html never pins newer content.
func (s *Site) serveAsset(w http.ResponseWriter, r *http.Request) {
	name := path.Join("static", path.Clean("/"+r.URL.Path))
	ctype, ok := assetTypes[path.Ext(name)]
	if !ok {
		notFound(w, r)
		return
	}
	body, err := fs.ReadFile(static, name)
	if err != nil {
		notFound(w, r)
		return
	}
	cache := cacheAsset
	if v := r.URL.Query().Get("v"); v != "" && v == s.hashes[name] {
		cache = cacheVersioned
	}
	w.Header().Set("Cache-Control", cache)
	w.Header().Set("Content-Type", ctype)
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(body))
}
