package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
