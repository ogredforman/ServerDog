package wardogservers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ogredforman/ServerDog/internal/source"
)

const fixtureDir = "../../../testdata/wardogservers"

// snapshotETag is the ETag the fixture server returns with snapshot.json (recorded upstream).
const snapshotETag = `W/"2275313daac9684fa8986840949b896b63d513549f1587e86b8fff925772b664"`

// exactStart is detail-exact.json's MatchStartUTC, int64:639277096481810000.
var exactStart = time.Unix(1792112848, 181_000_000).UTC()

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(fixtureDir, name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return b
}

// route is one canned response of the fixture server.
type route struct {
	status  int
	header  map[string]string
	fixture string // file in testdata/wardogservers; empty for no body
}

// fixtureServer serves canned routes by request path and records every request it receives.
type fixtureServer struct {
	*httptest.Server
	mu   sync.Mutex
	reqs []*http.Request
}

func newFixtureServer(t *testing.T, routes map[string]route) *fixtureServer {
	t.Helper()
	fs := &fixtureServer{}
	fs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fs.mu.Lock()
		fs.reqs = append(fs.reqs, r.Clone(context.Background()))
		fs.mu.Unlock()
		rt, ok := routes[r.URL.EscapedPath()]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if rt.status == 0 && r.URL.Path == "/v1/snapshot" && r.Header.Get("If-None-Match") == snapshotETag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		for k, v := range rt.header {
			w.Header().Set(k, v)
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		status := rt.status
		if status == 0 {
			status = http.StatusOK
		}
		w.WriteHeader(status)
		if rt.fixture != "" {
			w.Write(fixture(t, rt.fixture))
		}
	}))
	t.Cleanup(fs.Close)
	return fs
}

func (fs *fixtureServer) requests() []*http.Request {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return append([]*http.Request(nil), fs.reqs...)
}

func newClient(fs *fixtureServer) *Client {
	return New(Options{BaseURL: fs.URL + "/", Version: "1.2.3"})
}

func TestDecodeTicks(t *testing.T) {
	tests := []struct {
		in      string
		want    source.MatchStart
		wantErr bool
	}{
		{"int64:639277096481810000", source.MatchStart{State: source.Exact, At: exactStart}, false},
		{"int64:621355968010000000", source.MatchStart{State: source.Exact, At: time.Unix(1, 0).UTC()}, false},
		{"int64:-1", source.MatchStart{State: source.NotStarted}, false},
		{"int64:0", source.MatchStart{}, true},
		{"int64:-2", source.MatchStart{}, true},
		{"int64:abc", source.MatchStart{}, true},
		{"int64:", source.MatchStart{}, true},
		{"int:639277096481810000", source.MatchStart{}, true},
		{"639277096481810000", source.MatchStart{}, true},
		{"", source.MatchStart{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := decodeTicks(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if got.State != tt.want.State || !got.At.Equal(tt.want.At) {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

// Display names: the provider's map codenames as seen in-game, and the mode from the experience.
func TestDisplayNames(t *testing.T) {
	maps := []struct{ variant, want string }{
		{"Bakurani", "Bakurani"},
		{"Madrid", "Ozeti"},
		{"Detroit", "Zestafona"},
		{"Tbilisi", "Tbilisi"}, // unknown codename: raw
		{"", ""},
	}
	for _, c := range maps {
		if got := mapName(c.variant); got != c.want {
			t.Errorf("mapName(%q) = %q, want %q", c.variant, got, c.want)
		}
	}
	modes := []struct{ experience, want string }{
		{"Bakurani_KOTH_02", "KOTH"},
		{"Madrid_KOTH_01+KOTH_InfantryOnly", "KOTH"},
		{"Detroit_KOTH_01+KOTH_InfantryOnly+KOTH_Hardcore", "KOTH"},
		{"Ozeti_TDM_01", "TDM"},
		{"KOTH", "KOTH"},
		{"KOTH+KOTH_Hardcore", "KOTH"},
		{"", ""},
	}
	for _, c := range modes {
		if got := modeName(c.experience); got != c.want {
			t.Errorf("modeName(%q) = %q, want %q", c.experience, got, c.want)
		}
	}
}

func TestSnapshotMapsFixture(t *testing.T) {
	fs := newFixtureServer(t, map[string]route{
		"/v1/snapshot": {header: map[string]string{"ETag": snapshotETag}, fixture: "snapshot.json"},
	})
	snap, err := newClient(fs).Snapshot(context.Background(), "")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if snap.ID != "e19ec70bc5230aabe96757c3512ae0b18f0251800732ff4be10d3ff72c59a7b0" ||
		snap.Tag != snapshotETag ||
		!snap.FetchedAt.Equal(time.Date(2026, 10, 16, 1, 17, 51, 359_000_000, time.UTC)) ||
		snap.RefreshEvery != 180*time.Second || snap.Stale || !snap.Complete {
		t.Errorf("snapshot meta = %+v", snap)
	}
	if len(snap.Servers) != 6 {
		t.Fatalf("got %d servers, want 6", len(snap.Servers))
	}
	want := source.Server{
		ID:         "7a1cec1a-a7b0-4b6d-99a8-a085596ac09d",
		InstanceID: "009b8513-9c83-49da-b51f-e880bc1936fc",
		Name:       "#2 EU - CodeFourGaming - 50v50 Infantry - discord.gg/c4g",
		Region:     "eu-central",
		Type:       source.Community,
		Players:    98,
		MaxPlayers: 100,
		Map:        source.Map{Variant: "Madrid", Base: "Kavkazi", Name: "Ozeti"},
		Mode:       source.Mode{Experience: "Madrid_KOTH_01+KOTH_InfantryOnly", GameMode: "GM_Session_C", Modifiers: []string{"KOTH_InfantryOnly"}, Name: "KOTH"},
		Rulesets:   []string{"infantry-only"},
		ObservedAt: time.Date(2026, 10, 16, 1, 16, 56, 253_000_000, time.UTC),
	}
	if got := snap.Servers[2]; !reflect.DeepEqual(got, want) {
		t.Errorf("server[2] =\n %+v\nwant\n %+v", got, want)
	}
	if s := snap.Servers[0]; s.Type != source.Official || s.ID != "635219" || s.MatchStart != nil {
		t.Errorf("server[0] = %+v, want official 635219 without MatchStart", s)
	}
	if s := snap.Servers[4]; !s.PasswordProtected || s.Players != 0 || s.MaxPlayers != 60 {
		t.Errorf("server[4] = %+v, want password-protected empty 0/60", s)
	}
}

func TestSnapshotConditional(t *testing.T) {
	fs := newFixtureServer(t, map[string]route{
		"/v1/snapshot": {header: map[string]string{"ETag": snapshotETag}, fixture: "snapshot.json"},
	})
	c := newClient(fs)
	if _, err := c.Snapshot(context.Background(), snapshotETag); !errors.Is(err, source.ErrNotModified) {
		t.Fatalf("Snapshot(current tag) err = %v, want ErrNotModified", err)
	}
	if _, err := c.Snapshot(context.Background(), `W/"older"`); err != nil {
		t.Fatalf("Snapshot(old tag) err = %v, want a full snapshot", err)
	}
	if _, err := c.Snapshot(context.Background(), ""); err != nil {
		t.Fatalf("Snapshot(no tag) err = %v", err)
	}
	reqs := fs.requests()
	wantINM := []string{snapshotETag, `W/"older"`, ""}
	for i, r := range reqs {
		if got := r.Header.Get("If-None-Match"); got != wantINM[i] {
			t.Errorf("request %d If-None-Match = %q, want %q", i, got, wantINM[i])
		}
	}
}

func TestMatchStart(t *testing.T) {
	tests := []struct {
		fixture string
		want    source.MatchStart
	}{
		{"detail-exact.json", source.MatchStart{State: source.Exact, At: exactStart}},
		{"detail-not-started.json", source.MatchStart{State: source.NotStarted}},
		{"detail-missing.json", source.MatchStart{State: source.Unknown}},
	}
	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			fs := newFixtureServer(t, map[string]route{"/v1/servers/635219": {fixture: tt.fixture}})
			got, err := newClient(fs).MatchStart(context.Background(), "635219")
			if err != nil {
				t.Fatalf("MatchStart: %v", err)
			}
			if got.State != tt.want.State || !got.At.Equal(tt.want.At) {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestMatchStartEscapesServerID(t *testing.T) {
	fs := newFixtureServer(t, map[string]route{"/v1/servers/a%2Fb%3Fc": {fixture: "detail-exact.json"}})
	if _, err := newClient(fs).MatchStart(context.Background(), "a/b?c"); err != nil {
		t.Fatalf("MatchStart: %v", err)
	}
}

func TestThrottled(t *testing.T) {
	date := "Fri, 16 Oct 2026 01:18:39 GMT"
	tests := []struct {
		name   string
		status int
		header map[string]string
		want   time.Duration
	}{
		{"429 seconds", http.StatusTooManyRequests, map[string]string{"Retry-After": "42"}, 42 * time.Second},
		{"503 seconds", http.StatusServiceUnavailable, map[string]string{"Retry-After": "120"}, 120 * time.Second},
		{"429 http date", http.StatusTooManyRequests, map[string]string{"Retry-After": "Fri, 16 Oct 2026 01:19:09 GMT", "Date": date}, 30 * time.Second},
		{"429 http date in the past", http.StatusTooManyRequests, map[string]string{"Retry-After": "Fri, 16 Oct 2026 01:18:00 GMT", "Date": date}, time.Second},
		{"429 zero", http.StatusTooManyRequests, map[string]string{"Retry-After": "0"}, time.Second},
		{"429 missing", http.StatusTooManyRequests, nil, DefaultRetryAfter},
		{"503 garbage", http.StatusServiceUnavailable, map[string]string{"Retry-After": "soon"}, DefaultRetryAfter},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := route{status: tt.status, header: tt.header}
			fs := newFixtureServer(t, map[string]route{"/v1/snapshot": rt, "/v1/servers/635219": rt})
			c := newClient(fs)
			_, snapErr := c.Snapshot(context.Background(), "")
			_, detailErr := c.MatchStart(context.Background(), "635219")
			for _, err := range []error{snapErr, detailErr} {
				var ra source.RetryAfterError
				if !errors.As(err, &ra) {
					t.Fatalf("err = %v, want RetryAfterError", err)
				}
				if ra.After != tt.want {
					t.Errorf("After = %s, want %s", ra.After, tt.want)
				}
			}
		})
	}
}

func TestErrors(t *testing.T) {
	tests := []struct {
		name string
		rt   route
	}{
		{"malformed json", route{fixture: "malformed.json"}},
		{"server error", route{status: http.StatusInternalServerError}},
		{"not found", route{status: http.StatusNotFound}},
		{"unsolicited 304", route{status: http.StatusNotModified}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := newFixtureServer(t, map[string]route{"/v1/snapshot": tt.rt, "/v1/servers/635219": tt.rt})
			c := newClient(fs)
			_, snapErr := c.Snapshot(context.Background(), "")
			_, detailErr := c.MatchStart(context.Background(), "635219")
			for _, err := range []error{snapErr, detailErr} {
				var ra source.RetryAfterError
				if err == nil || errors.Is(err, source.ErrNotModified) || errors.As(err, &ra) {
					t.Errorf("err = %v, want a plain error", err)
				}
			}
		})
	}
}

func TestSnapshotRejectsMissingMeta(t *testing.T) {
	fs := newFixtureServer(t, map[string]route{"/v1/snapshot": {fixture: "detail-exact.json"}})
	if _, err := newClient(fs).Snapshot(context.Background(), ""); err == nil {
		t.Fatal("Snapshot of a body without snapshot meta succeeded")
	}
}

func TestUserAgentOnEveryRequest(t *testing.T) {
	fs := newFixtureServer(t, map[string]route{
		"/v1/snapshot":       {fixture: "snapshot.json"},
		"/v1/servers/635219": {fixture: "detail-exact.json"},
	})
	c := New(Options{BaseURL: fs.URL, Version: "1.2.3", Contact: "ops@example.test"})
	c.Snapshot(context.Background(), "")
	c.MatchStart(context.Background(), "635219")
	reqs := fs.requests()
	if len(reqs) != 2 {
		t.Fatalf("got %d requests, want 2", len(reqs))
	}
	for _, r := range reqs {
		if got, want := r.Header.Get("User-Agent"), "ServerDog/1.2.3 (+https://servers.dog; ops@example.test)"; got != want {
			t.Errorf("%s User-Agent = %q, want %q", r.URL.Path, got, want)
		}
	}
}

func TestUserAgent(t *testing.T) {
	tests := []struct{ version, contact, want string }{
		{"1.0.0", "", "ServerDog/1.0.0 (+https://servers.dog)"},
		{"", "", "ServerDog/dev (+https://servers.dog)"},
		{"1.0.0", "me@example.test", "ServerDog/1.0.0 (+https://servers.dog; me@example.test)"},
	}
	for _, tt := range tests {
		if got := UserAgent(tt.version, tt.contact); got != tt.want {
			t.Errorf("UserAgent(%q, %q) = %q, want %q", tt.version, tt.contact, got, tt.want)
		}
	}
}

func TestTimeout(t *testing.T) {
	if got := New(Options{}).http.Timeout; got != DefaultTimeout {
		t.Errorf("default timeout = %s, want %s", got, DefaultTimeout)
	}
	if got := New(Options{}).base; got != DefaultBaseURL {
		t.Errorf("default base = %q", got)
	}
	if !strings.HasPrefix(DefaultBaseURL, "https://") {
		t.Errorf("DefaultBaseURL %q is not https", DefaultBaseURL)
	}
}
