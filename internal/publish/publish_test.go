package publish

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ogredforman/ServerDog/internal/source"
	"github.com/ogredforman/ServerDog/internal/tracker"
)

var t0 = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

const (
	every     = time.Minute // snapshot RefreshEvery used throughout
	pollDelay = 10 * time.Second
)

func srv(id string, players int) source.Server {
	return source.Server{
		ID:         id,
		InstanceID: "inst-1",
		Name:       "Server " + id,
		Region:     "eu",
		Players:    players,
		MaxPlayers: 128,
		Map:        source.Map{Variant: "map-1", Base: "base", Name: "Map One"},
		Mode:       source.Mode{Experience: "exp-1", Name: "KOTH"},
	}
}

func snap(id string, at time.Time, servers ...source.Server) source.Snapshot {
	return source.Snapshot{ID: id, FetchedAt: at, RefreshEvery: every, Complete: true, Servers: servers}
}

func newTracker() *tracker.Tracker {
	return tracker.New(tracker.Options{StartingWindow: 20 * time.Minute, DropPct: 30, DropMin: 25})
}

func TestBuildSchema(t *testing.T) {
	s := srv("ABC", 10)
	s.Type = source.Community
	s.PasswordProtected = true
	s.Rulesets = []string{"hardcore"}
	f := Build(snap("s1", t0, s), newTracker(), t0.Add(5*time.Second), pollDelay)

	b, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	servers, ok := got["servers"].([]any)
	if !ok || len(servers) != 1 {
		t.Fatalf("servers = %#v, want one server", got["servers"])
	}
	delete(got, "servers")
	wantTop := map[string]any{
		"v":              float64(2),
		"generatedAt":    float64(t0.Unix() + 5),
		"nextUpdateAt":   float64(t0.Add(every + pollDelay).Unix()),
		"snapshotAt":     float64(t0.Unix()),
		"refreshSeconds": float64(60),
		"stale":          false,
		"complete":       true,
		"regions":        []any{"eu"},
	}
	if !reflect.DeepEqual(got, wantTop) {
		t.Errorf("top level = %#v\nwant %#v", got, wantTop)
	}
	wantServer := map[string]any{
		"id": "ABC", "t": "c", "r": "eu", "n": "Server ABC", "p": float64(10), "mp": float64(128),
		"pw": true, "map": "Map One", "mode": "KOTH", "rs": []any{"hardcore"},
		"ms": nil, "st": "unknown",
	}
	if !reflect.DeepEqual(servers[0], wantServer) {
		t.Errorf("server = %#v\nwant %#v", servers[0], wantServer)
	}
}

func TestBuildEmptyListsAreArrays(t *testing.T) {
	s := srv("ABC", 3)
	s.Region = ""
	b, err := json.Marshal(Build(snap("s1", t0, s), newTracker(), t0, pollDelay))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Regions json.RawMessage `json:"regions"`
		Servers []struct {
			RS json.RawMessage `json:"rs"`
		} `json:"servers"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if string(got.Regions) != "[]" || string(got.Servers[0].RS) != "[]" {
		t.Errorf("regions = %s, rs = %s; want [] and []", got.Regions, got.Servers[0].RS)
	}
}

func TestBuildStates(t *testing.T) {
	started := t0.Add(-10 * time.Minute)
	t1 := t0.Add(every)
	changed := srv("A", 10)
	changed.InstanceID = "inst-2"
	// confirmed observes A with an exact start, so a later change is unconfirmed.
	confirmed := func(tr *tracker.Tracker) {
		tr.Observe(snap("s1", t0, srv("A", 10)))
		tr.Apply("A", source.MatchStart{State: source.Exact, At: started}, t0)
	}

	tests := []struct {
		name  string
		setup func(tr *tracker.Tracker) source.Snapshot
		st    string
		ms    *int64
	}{
		{
			name: "exact after detail result",
			setup: func(tr *tracker.Tracker) source.Snapshot {
				confirmed(tr)
				return snap("s1", t0, srv("A", 10))
			},
			st: "exact", ms: unix(started),
		},
		{
			name: "estimate after unconfirmed change",
			setup: func(tr *tracker.Tracker) source.Snapshot {
				confirmed(tr)
				sn := snap("s2", t1, changed)
				tr.Observe(sn)
				return sn
			},
			st: "estimate", ms: unix(t1),
		},
		{
			name: "starting publishes the observed change",
			setup: func(tr *tracker.Tracker) source.Snapshot {
				confirmed(tr)
				sn := snap("s2", t1, changed)
				tr.Observe(sn)
				tr.Apply("A", source.MatchStart{State: source.NotStarted}, t1)
				return sn
			},
			st: "starting", ms: unix(t1),
		},
		{
			name: "unknown before any detail result",
			setup: func(tr *tracker.Tracker) source.Snapshot {
				sn := snap("s1", t0, srv("A", 10))
				tr.Observe(sn)
				return sn
			},
			st: "unknown",
		},
		{
			name: "empty server is unknown",
			setup: func(tr *tracker.Tracker) source.Snapshot {
				confirmed(tr)
				sn := snap("s2", t1, srv("A", 0))
				tr.Observe(sn)
				return sn
			},
			st: "unknown",
		},
		{
			name: "no join code is unknown",
			setup: func(tr *tracker.Tracker) source.Snapshot {
				sn := snap("s1", t0, srv("", 10))
				tr.Observe(sn)
				return sn
			},
			st: "unknown",
		},
		{
			name: "untracked server is unknown",
			setup: func(tr *tracker.Tracker) source.Snapshot {
				return snap("s1", t0, srv("A", 10))
			},
			st: "unknown",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tr := newTracker()
			sn := tc.setup(tr)
			f := Build(sn, tr, sn.FetchedAt.Add(time.Second), pollDelay)
			if len(f.Servers) != 1 {
				t.Fatalf("got %d servers, want 1", len(f.Servers))
			}
			got := f.Servers[0]
			if got.State != tc.st || !reflect.DeepEqual(got.MatchStart, tc.ms) {
				t.Errorf("st, ms = %q, %v; want %q, %v", got.State, deref(got.MatchStart), tc.st, deref(tc.ms))
			}
		})
	}
}

func TestBuildKeepsAllServersAndSortsRegions(t *testing.T) {
	a, b, c, d := srv("A", 5), srv("B", 0), srv("C", 7), srv("", 2)
	a.Region, b.Region, c.Region, d.Region = "us-east", "eu", "us-east", "asia"
	f := Build(snap("s1", t0, a, b, c, d), newTracker(), t0, pollDelay)

	if want := []string{"asia", "eu", "us-east"}; !reflect.DeepEqual(f.Regions, want) {
		t.Errorf("regions = %v, want %v", f.Regions, want)
	}
	var ids []string
	for _, s := range f.Servers {
		ids = append(ids, s.ID)
	}
	if want := []string{"A", "B", "C", ""}; !reflect.DeepEqual(ids, want) {
		t.Errorf("server IDs = %q, want snapshot order %q", ids, want)
	}
}

func TestStale(t *testing.T) {
	limit := 2*every + time.Minute
	tests := []struct {
		name          string
		providerStale bool
		age           time.Duration
		want          bool
	}{
		{"fresh", false, 0, false},
		{"provider says stale", true, 0, true},
		{"at the limit", false, limit, false},
		{"past the limit", false, limit + time.Second, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sn := snap("s1", t0)
			sn.Stale = tc.providerStale
			if got := Build(sn, newTracker(), t0.Add(tc.age), pollDelay).Stale; got != tc.want {
				t.Errorf("stale = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNextUpdateAt(t *testing.T) {
	due := t0.Add(every + pollDelay)
	tests := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{"next poll ahead", t0.Add(5 * time.Second), due},
		{"just before the poll", due.Add(-time.Second), due},
		{"poll overdue", due, due.Add(15 * time.Second)},
		{"long overdue", due.Add(time.Hour), due.Add(time.Hour + 15*time.Second)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := NextUpdateAt(snap("s1", t0), tc.now, pollDelay); !got.Equal(tc.want) {
				t.Errorf("NextUpdateAt = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestWriteReplacesAtomically(t *testing.T) {
	dir := t.TempDir()
	tr := newTracker()
	first := Build(snap("s1", t0, srv("A", 1)), tr, t0, pollDelay)
	second := Build(snap("s2", t0.Add(every), srv("B", 2), srv("C", 3)), tr, t0.Add(every), pollDelay)

	for _, f := range []File{first, second} {
		if err := Write(context.Background(), dir, f); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}

	b, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	var got File
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("published file is not valid JSON: %v", err)
	}
	if !reflect.DeepEqual(got, second) {
		t.Errorf("published file = %+v\nwant %+v", got, second)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if !reflect.DeepEqual(names, []string{FileName}) {
		t.Errorf("dir holds %q, want only %s (no temp files left)", names, FileName)
	}
}

func TestWriteMissingDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing")
	if err := Write(context.Background(), dir, File{}); err == nil {
		t.Fatal("Write to a missing dir succeeded, want error")
	}
}

func unix(at time.Time) *int64 {
	u := at.Unix()
	return &u
}

func deref(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}
