package tracker

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ogredforman/ServerDog/internal/source"
)

// cachedTracker builds a tracker holding one server in each published state.
func cachedTracker() (*Tracker, []string) {
	tr := New(testOptions())
	tr.Observe(snap("s1", t0, srv("exact", 50), srv("est", 50), srv("starting", 50), srv("empty", 50)))
	t1 := t0.Add(every)
	tr.Observe(snap("s2", t1, srv("exact", 50), changedSrv("est"), changedSrv("starting"), srv("empty", 0)))
	tr.Apply("exact", exact(t0.Add(-10*time.Minute)), t1.Add(time.Second))
	tr.Apply("starting", notStarted, t1.Add(2*time.Second))
	return tr, []string{"empty", "est", "exact", "starting"}
}

// changedSrv is srv(id, 50) on a new instance.
func changedSrv(id string) source.Server {
	s := srv(id, 50)
	s.InstanceID = "inst-2"
	return s
}

func roundTrip(t *testing.T, tr *Tracker) *Tracker {
	t.Helper()
	var buf bytes.Buffer
	if err := tr.Save(&buf); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got := New(testOptions())
	if err := got.Load(&buf); err != nil {
		t.Fatalf("Load: %v", err)
	}
	return got
}

// TestCacheRoundTrip checks that a restored tracker publishes the same starts and, after the
// next snapshot, queues the same servers in the same tiers as the original (SPEC §8).
func TestCacheRoundTrip(t *testing.T) {
	orig, ids := cachedTracker()
	got := roundTrip(t, orig)

	now := t0.Add(5 * time.Minute)
	for _, id := range ids {
		want := mustGet(t, orig, id)
		have := mustGet(t, got, id)
		want.Tier, have.Tier = None, None // tiers are recomputed by Observe
		if !reflect.DeepEqual(have, want) {
			t.Errorf("%s: restored %+v, want %+v", id, have, want)
		}
		wp, _ := orig.Published(id, now)
		gp, _ := got.Published(id, now)
		if gp != wp {
			t.Errorf("%s: restored published %+v, want %+v", id, gp, wp)
		}
	}
	if wp, _ := orig.Published("starting", now); wp.State != Starting {
		t.Fatalf("fixture: starting publishes %v, want starting", wp.State)
	}
	if wp, _ := orig.Published("est", now); wp.State != Estimate {
		t.Fatalf("fixture: est publishes %v, want estimate", wp.State)
	}

	// The same snapshot ID again (a restart re-polls without an ETag): servers already checked
	// in it are not queued again (at most one detail fetch per server per snapshot ID).
	t1 := t0.Add(every)
	next := snap("s2", t1, srv("exact", 50), changedSrv("est"), changedSrv("starting"), srv("empty", 0))
	orig.Observe(next)
	got.Observe(next)
	if q, w := queueIDs(got), queueIDs(orig); !reflect.DeepEqual(q, w) {
		t.Errorf("restored queue = %v, want %v", q, w)
	}
	if q := queueIDs(got); !reflect.DeepEqual(q, []string{"est"}) {
		t.Errorf("restored queue = %v, want only the unchecked [est]", q)
	}
}

// TestCacheLoadRejects covers corrupt caches: Load fails and the tracker keeps its state.
func TestCacheLoadRejects(t *testing.T) {
	tests := []struct {
		name, data string
	}{
		{"empty", ""},
		{"truncated", `{"v":1,"servers":[{"id":"a"`},
		{"not an object", `[1,2,3]`},
		{"missing version", `{"servers":[]}`},
		{"other version", `{"v":2,"servers":[]}`},
		{"empty id", `{"v":1,"servers":[{"id":"","start":{"state":"exact"}}]}`},
		{"duplicate id", `{"v":1,"servers":[{"id":"a","start":{"state":"unknown"}},{"id":"a","start":{"state":"unknown"}}]}`},
		{"unknown start state", `{"v":1,"servers":[{"id":"a","start":{"state":"soon"}}]}`},
		{"bad time", `{"v":1,"servers":[{"id":"a","lastSeen":"yesterday","start":{"state":"unknown"}}]}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tr, ids := cachedTracker()
			if err := tr.Load(strings.NewReader(tc.data)); err == nil {
				t.Fatal("Load succeeded, want error")
			}
			if tr.Len() != len(ids) {
				t.Errorf("after failed Load: %d servers, want the original %d", tr.Len(), len(ids))
			}
		})
	}
}

func TestCacheFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tracker.json")
	ctx := context.Background()

	cold := New(testOptions())
	if err := cold.LoadFile(ctx, path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("LoadFile missing = %v, want fs.ErrNotExist", err)
	}

	orig, ids := cachedTracker()
	if err := orig.SaveFile(ctx, path); err != nil {
		t.Fatalf("SaveFile: %v", err)
	}
	if err := orig.SaveFile(ctx, path); err != nil { // replaces the existing file
		t.Fatalf("SaveFile again: %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("dir has %d entries, want only tracker.json", len(entries))
	}
	got := New(testOptions())
	if err := got.LoadFile(ctx, path); err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if got.Len() != len(ids) {
		t.Errorf("loaded %d servers, want %d", got.Len(), len(ids))
	}

	if err := os.WriteFile(path, []byte("{garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := New(testOptions()).LoadFile(ctx, path); err == nil || errors.Is(err, fs.ErrNotExist) {
		t.Errorf("LoadFile corrupt = %v, want a decode error", err)
	}
}
