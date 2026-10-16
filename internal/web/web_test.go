package web

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ogredforman/ServerDog/internal/clock"
	"github.com/ogredforman/ServerDog/internal/publish"
)

var t0 = time.Unix(1_800_000_000, 0)

// newSite returns a Site over a temp data dir holding a servers.json with body.
func newSite(t *testing.T, body string) (*Site, *clock.Fake) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, publish.FileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	clk := clock.NewFake(t0)
	return New(clk, dir), clk
}

func TestHandler(t *testing.T) {
	const dataBody = `{"v":1}`
	tests := []struct {
		name         string
		published    bool
		method       string
		path         string
		wantStatus   int
		wantCache    string
		wantType     string
		wantBodyPart string
	}{
		{"index", false, http.MethodGet, "/", http.StatusOK, "public, max-age=60, s-maxage=300", "text/html; charset=utf-8", "Not affiliated with or endorsed by Bulkhead or Wardogs"},
		{"index HEAD", false, http.MethodHead, "/", http.StatusOK, "public, max-age=60, s-maxage=300", "text/html; charset=utf-8", ""},
		{"unknown path", false, http.MethodGet, "/nope", http.StatusNotFound, "no-store", "", ""},
		{"index POST", false, http.MethodPost, "/", http.StatusNotFound, "no-store", "", ""},
		{"data before first publish", false, http.MethodGet, "/data/servers.json", http.StatusNotFound, "no-store", "", ""},
		{"data", true, http.MethodGet, "/data/servers.json", http.StatusOK, "public, max-age=30, s-maxage=30", "application/json", dataBody},
		{"data HEAD", true, http.MethodHead, "/data/servers.json", http.StatusOK, "public, max-age=30, s-maxage=30", "application/json", ""},
		{"data POST", true, http.MethodPost, "/data/servers.json", http.StatusNotFound, "no-store", "", ""},
		{"js asset", false, http.MethodGet, "/js/logic/timer.js", http.StatusOK, "public, max-age=60, s-maxage=300", "text/javascript; charset=utf-8", "export function formatTimer"},
		{"js app HEAD", false, http.MethodHead, "/js/app.js", http.StatusOK, "public, max-age=60, s-maxage=300", "text/javascript; charset=utf-8", ""},
		{"js asset stale version", false, http.MethodGet, "/js/logic/timer.js?v=000000000000", http.StatusOK, "public, max-age=60, s-maxage=300", "text/javascript; charset=utf-8", "export function formatTimer"},
		{"healthz before first publish", false, http.MethodGet, "/healthz", http.StatusOK, "no-store", "application/json", `{"snapshotAgeSeconds":null,"stale":false}`},
		{"healthz POST", false, http.MethodPost, "/healthz", http.StatusNotFound, "no-store", "", ""},
		{"css asset", false, http.MethodGet, "/css/app.css", http.StatusOK, "public, max-age=60, s-maxage=300", "text/css; charset=utf-8", "--page"},
		{"font", false, http.MethodGet, "/fonts/barlow-500.woff2", http.StatusOK, "public, max-age=60, s-maxage=300", "font/woff2", "wOF2"},
		{"font license", false, http.MethodGet, "/fonts/OFL.txt", http.StatusOK, "public, max-age=60, s-maxage=300", "text/plain; charset=utf-8", "SIL Open Font License"},
		{"missing font", false, http.MethodGet, "/fonts/nope.woff2", http.StatusNotFound, "no-store", "", ""},
		{"index under css", false, http.MethodGet, "/css/index.html", http.StatusNotFound, "no-store", "", ""},
		{"missing js asset", false, http.MethodGet, "/js/nope.js", http.StatusNotFound, "no-store", "", ""},
		{"js directory", false, http.MethodGet, "/js/logic/", http.StatusNotFound, "no-store", "", ""},
		{"js unknown type", false, http.MethodGet, "/js/logic", http.StatusNotFound, "no-store", "", ""},
		{"unclean path redirect", false, http.MethodGet, "/js/../index.html", http.StatusTemporaryRedirect, "no-store", "", ""},
		{"other data file", true, http.MethodGet, "/data/tracker.json", http.StatusNotFound, "no-store", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _ := newSite(t, dataBody)
			if tt.published {
				s.Published(publish.File{NextUpdateAt: t0.Add(30 * time.Second).Unix(), RefreshSeconds: 60})
			}
			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))
			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if got := rec.Header().Get("Cache-Control"); got != tt.wantCache {
				t.Errorf("Cache-Control = %q, want %q", got, tt.wantCache)
			}
			if tt.wantType != "" {
				if got := rec.Header().Get("Content-Type"); got != tt.wantType {
					t.Errorf("Content-Type = %q, want %q", got, tt.wantType)
				}
			}
			if !strings.Contains(rec.Body.String(), tt.wantBodyPart) {
				t.Errorf("body does not contain %q", tt.wantBodyPart)
			}
		})
	}
}

func TestDataMaxAge(t *testing.T) {
	tests := []struct {
		name    string
		elapsed time.Duration // fake clock advance after publish
		next    time.Duration // nextUpdateAt relative to t0
		refresh int64
		want    string
	}{
		{"inside range", 0, 42 * time.Second, 60, "public, max-age=42, s-maxage=42"},
		{"rounds down", 500 * time.Millisecond, 42 * time.Second, 60, "public, max-age=41, s-maxage=41"},
		{"counts down", 20 * time.Second, 42 * time.Second, 60, "public, max-age=22, s-maxage=22"},
		{"below floor", 0, 3 * time.Second, 60, "public, max-age=5, s-maxage=5"},
		{"past nextUpdateAt", 2 * time.Minute, 42 * time.Second, 60, "public, max-age=5, s-maxage=5"},
		{"above refresh", 0, 90 * time.Second, 60, "public, max-age=60, s-maxage=60"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, clk := newSite(t, `{}`)
			s.Published(publish.File{NextUpdateAt: t0.Add(tt.next).Unix(), RefreshSeconds: tt.refresh})
			clk.Advance(tt.elapsed)
			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data/servers.json", nil))
			if got := rec.Header().Get("Cache-Control"); got != tt.want {
				t.Errorf("Cache-Control = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDataMissingAfterPublish(t *testing.T) {
	s := New(clock.NewFake(t0), t.TempDir())
	s.Published(publish.File{NextUpdateAt: t0.Add(30 * time.Second).Unix(), RefreshSeconds: 60})
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data/servers.json", nil))
	if rec.Code != http.StatusNotFound || rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("got %d %q, want 404 no-store", rec.Code, rec.Header().Get("Cache-Control"))
	}
}

// assetRef matches a versioned asset URL in the served index.html.
var assetRef = regexp.MustCompile(`(/(?:js|css|fonts)/[A-Za-z0-9_./-]+)\?v=([0-9a-f]+)`)

func TestVersionedAssets(t *testing.T) {
	s, _ := newSite(t, `{}`)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	index := rec.Body.String()

	if strings.Contains(index, importMapMarker) {
		t.Error("index.html still contains the import map marker")
	}
	for _, want := range []string{`<script type="importmap">`, `src="/js/app.js?v=`, `"/js/logic/view.js":"/js/logic/view.js?v=`} {
		if !strings.Contains(index, want) {
			t.Errorf("index.html does not contain %q", want)
		}
	}

	// Every module, including ones only imported by other modules, is in the import map.
	refs := map[string]string{}
	for _, m := range assetRef.FindAllStringSubmatch(index, -1) {
		refs[m[1]] = m[2]
	}
	for _, url := range []string{"/js/app.js", "/js/logic/timer.js", "/js/logic/filters.js", "/js/logic/sort.js", "/js/logic/stale.js", "/js/logic/clock.js", "/js/logic/refresh.js", "/js/logic/prefs.js", "/js/logic/view.js", "/js/logic/regions.js", "/css/app.css", "/fonts/barlow-400.woff2", "/fonts/barlow-500.woff2", "/fonts/barlow-600.woff2"} {
		if _, ok := refs[url]; !ok {
			t.Errorf("index.html has no versioned URL for %s", url)
		}
	}

	// Each versioned URL serves content matching its hash, cached as immutable.
	for url, v := range refs {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url+"?v="+v, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s?v=%s: status %d", url, v, rec.Code)
			continue
		}
		if got := rec.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
			t.Errorf("%s?v=%s: Cache-Control = %q", url, v, got)
		}
		sum := sha256.Sum256(rec.Body.Bytes())
		if !strings.HasPrefix(hex.EncodeToString(sum[:]), v) {
			t.Errorf("%s?v=%s: hash does not match served content", url, v)
		}
	}
}

func TestVersionIndexMissingMarker(t *testing.T) {
	if _, err := versionIndex([]byte(`<script type="module" src="/js/app.js"></script>`), map[string]string{"static/js/app.js": "abc"}); err == nil {
		t.Error("versionIndex without the marker: want error")
	}
}

func TestHealthz(t *testing.T) {
	tests := []struct {
		name    string
		elapsed time.Duration // fake clock advance after publish
		stale   bool
		want    string
	}{
		{"fresh", 0, false, `{"snapshotAgeSeconds":20,"stale":false}`},
		{"ages with the clock", 95500 * time.Millisecond, false, `{"snapshotAgeSeconds":115,"stale":false}`},
		{"stale publish", 10 * time.Minute, true, `{"snapshotAgeSeconds":620,"stale":true}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, clk := newSite(t, `{}`)
			s.Published(publish.File{SnapshotAt: t0.Add(-20 * time.Second).Unix(), NextUpdateAt: t0.Add(40 * time.Second).Unix(), RefreshSeconds: 60, Stale: tt.stale})
			clk.Advance(tt.elapsed)
			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
			if rec.Code != http.StatusOK {
				t.Errorf("status = %d, want 200", rec.Code)
			}
			if got := rec.Header().Get("Cache-Control"); got != "no-store" {
				t.Errorf("Cache-Control = %q, want no-store", got)
			}
			if got := rec.Body.String(); got != tt.want {
				t.Errorf("body = %s, want %s", got, tt.want)
			}
		})
	}
}

// servedIndex returns the index.html that s serves.
func servedIndex(t *testing.T, s *Site) string {
	t.Helper()
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /: status %d", rec.Code)
	}
	return rec.Body.String()
}

// disclaimer is the non-affiliation text required in the navbar and the footer (SPEC §5, §8).
const disclaimer = "Unofficial fan site. Not affiliated with or endorsed by Bulkhead or Wardogs."

func TestDisclaimerInNavbarAndFooter(t *testing.T) {
	s, _ := newSite(t, `{}`)
	index := servedIndex(t, s)
	for _, part := range []struct{ open, close string }{
		{`<header id="navbar">`, `</header>`},
		{`<footer>`, `</footer>`},
	} {
		_, rest, ok := strings.Cut(index, part.open)
		section, _, ok2 := strings.Cut(rest, part.close)
		if !ok || !ok2 {
			t.Errorf("index.html has no %s…%s", part.open, part.close)
			continue
		}
		if !strings.Contains(section, disclaimer) {
			t.Errorf("%s does not contain the disclaimer", part.open)
		}
	}
	if !strings.Contains(index, "api.wardogservers.com") {
		t.Error("footer does not credit the data source")
	}
}

// thirdPartyRef matches an absolute or protocol-relative URL the browser would load (SPEC §9 Privacy).
var thirdPartyRef = regexp.MustCompile(`(?:src|href)\s*=\s*["']?(?:https?:)?//|url\(\s*["']?(?:https?:)?//|(?:from|import|fetch)\s*\(?\s*["'` + "`" + `](?:https?:)?//`)

func TestNoThirdPartyRequests(t *testing.T) {
	s, _ := newSite(t, `{}`)
	files := map[string]string{"/": servedIndex(t, s)}
	err := fs.WalkDir(static, "static", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !(strings.HasSuffix(path, ".js") || strings.HasSuffix(path, ".css")) {
			return err
		}
		b, err := fs.ReadFile(static, path)
		files[path] = string(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if m := thirdPartyRef.FindString(body); m != "" {
			t.Errorf("%s loads from another host: %q", name, m)
		}
	}
}

// maxFirstLoadBytes is the first-load budget for the static page, gzipped as Cloudflare serves it
// (SPEC §9 Page weight). servers.json has its own budget (size_test.go in publish).
const maxFirstLoadBytes = 150 * 1024

func TestFirstLoadWeight(t *testing.T) {
	s, _ := newSite(t, `{}`)
	index := servedIndex(t, s)
	total := gzipLen(t, []byte(index))
	seen := map[string]bool{} // fonts and app.js appear twice (preload, import map)
	for _, m := range assetRef.FindAllStringSubmatch(index, -1) {
		if seen[m[1]] {
			continue
		}
		seen[m[1]] = true
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, m[0], nil))
		body := rec.Body.Bytes()
		// woff2 is already compressed and is sent as is.
		if strings.HasSuffix(m[1], ".woff2") {
			total += len(body)
		} else {
			total += gzipLen(t, body)
		}
	}
	t.Logf("first load: %d bytes gzipped", total)
	if total > maxFirstLoadBytes {
		t.Errorf("first load is %d bytes gzipped, budget %d", total, maxFirstLoadBytes)
	}
}

func gzipLen(t *testing.T, b []byte) int {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(b); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Len()
}
