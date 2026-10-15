package fake

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/ogredforman/ServerDog/internal/clock"
	"github.com/ogredforman/ServerDog/internal/source"
)

func worldSnap(t *testing.T, w *World) source.Snapshot {
	t.Helper()
	s, err := w.Snapshot(context.Background(), "")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	return s
}

func TestWorldDeterministic(t *testing.T) {
	at := t0.Add(17*time.Minute + 5*time.Second)
	a := worldSnap(t, NewWorld(clock.NewFake(at), 7))
	b := worldSnap(t, NewWorld(clock.NewFake(at), 7))
	if !reflect.DeepEqual(a, b) {
		t.Fatal("same seed and time gave different snapshots")
	}
	if c := worldSnap(t, NewWorld(clock.NewFake(at), 8)); reflect.DeepEqual(a.Servers, c.Servers) {
		t.Fatal("different seeds gave identical servers")
	}
	wa, wb := NewWorld(clock.NewFake(at), 7), NewWorld(clock.NewFake(at), 7)
	for _, s := range a.Servers {
		if s.ID == "" {
			continue
		}
		ma, _ := wa.MatchStart(context.Background(), s.ID)
		mb, _ := wb.MatchStart(context.Background(), s.ID)
		if ma != mb {
			t.Fatalf("MatchStart(%s) differs: %+v vs %+v", s.ID, ma, mb)
		}
	}
}

func TestWorldRosterMix(t *testing.T) {
	for _, seed := range []int64{1, 42, -3} {
		s := worldSnap(t, NewWorld(clock.NewFake(t0), seed))
		if len(s.Servers) != WorldServers {
			t.Fatalf("seed %d: %d servers, want %d", seed, len(s.Servers), WorldServers)
		}
		if !s.Complete || s.Stale || s.RefreshEvery != WorldRefresh || s.Tag != s.ID {
			t.Errorf("seed %d: bad snapshot header %+v", seed, s)
		}
		var official, community, empty, full, zero, noID int
		regions, ids := map[string]bool{}, map[string]bool{}
		for _, sv := range s.Servers {
			regions[sv.Region] = true
			if sv.Type == source.Official {
				official++
			} else {
				community++
			}
			switch {
			case sv.MaxPlayers == 0:
				zero++
			case sv.Players == 0:
				empty++
			case sv.Players == sv.MaxPlayers:
				full++
			}
			if sv.ID == "" {
				noID++
			} else if ids[sv.ID] {
				t.Errorf("seed %d: duplicate join code %s", seed, sv.ID)
			}
			ids[sv.ID] = true
			if sv.Players > sv.MaxPlayers || !sv.ObservedAt.Equal(s.FetchedAt) || sv.MatchStart != nil {
				t.Errorf("seed %d: bad server %+v", seed, sv)
			}
		}
		if official == 0 || community == 0 || empty == 0 || full == 0 || zero == 0 || noID == 0 {
			t.Errorf("seed %d: missing a case: official=%d community=%d empty=%d full=%d zero=%d noID=%d",
				seed, official, community, empty, full, zero, noID)
		}
		if len(regions) != len(worldRegions) {
			t.Errorf("seed %d: %d regions, want %d", seed, len(regions), len(worldRegions))
		}
	}
}

// Across an hour of consecutive snapshots, a server's map changes exactly when its match
// rotates, a rotation drops a well-filled normal server's players by at least 30%, and the
// instance ID changes only on every 8th match.
func TestWorldRotationPattern(t *testing.T) {
	c := clock.NewFake(t0)
	w := NewWorld(c, 42)
	prev := worldSnap(t, w)
	var rotations, drops, restarts int
	for range 60 {
		c.Advance(WorldRefresh)
		cur := worldSnap(t, w)
		if cur.ID == prev.ID || cur.FetchedAt.Sub(prev.FetchedAt) != WorldRefresh {
			t.Fatalf("snapshot did not advance: %s → %s", prev.ID, cur.ID)
		}
		for i, sv := range cur.Servers {
			p, e := prev.Servers[i], &w.roster[i]
			mPrev, _ := e.match(prev.FetchedAt)
			mCur, _ := e.match(cur.FetchedAt)
			rotated := mCur != mPrev
			if (sv.Map != p.Map) != rotated {
				t.Fatalf("server %d: map %v → %v, rotated=%v", i, p.Map, sv.Map, rotated)
			}
			if sv.Mode.Experience != p.Mode.Experience {
				t.Fatalf("server %d: experience changed", i)
			}
			wantRestart := rotated && floorDivInt(mCur, worldRestartEvery) != floorDivInt(mPrev, worldRestartEvery)
			if (sv.InstanceID != p.InstanceID) != wantRestart {
				t.Fatalf("server %d: instance %s → %s, want restart=%v", i, p.InstanceID, sv.InstanceID, wantRestart)
			}
			if !rotated {
				continue
			}
			rotations++
			if wantRestart {
				restarts++
			}
			if e.kind == kindNormal && p.Players >= 20 {
				if float64(sv.Players) > 0.7*float64(p.Players) {
					t.Fatalf("server %d: rotation players %d → %d, want ≥30%% drop", i, p.Players, sv.Players)
				}
				drops++
			}
		}
		prev = cur
	}
	if rotations == 0 || drops == 0 || restarts == 0 {
		t.Fatalf("an hour had rotations=%d drops=%d restarts=%d; want all > 0", rotations, drops, restarts)
	}
}

func TestWorldMatchStart(t *testing.T) {
	w := NewWorld(clock.NewFake(t0), 42)
	var e *worldServer
	for i := range w.roster {
		if r := &w.roster[i]; r.kind == kindNormal && r.id != "" && !r.noStart {
			e = r
			break
		}
	}
	m, rotAt := e.match(t0)
	start := rotAt.Add(w.warmup(e, m))
	ctx := context.Background()
	// Warmup is 30–150s and a match lasts at least 12 min, so every case stays in match m.
	tests := []struct {
		name string
		at   time.Time
		want source.MatchStart
	}{
		{"just after rotation", rotAt.Add(time.Second), source.MatchStart{State: source.NotStarted}},
		{"end of warmup", start.Add(-time.Second), source.MatchStart{State: source.NotStarted}},
		{"at start", start, source.MatchStart{State: source.Exact, At: start}},
		{"later in match", start.Add(5 * time.Minute), source.MatchStart{State: source.Exact, At: start}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The fake clock only moves forward; a World is deterministic, so build one per time.
			got, err := NewWorld(clock.NewFake(tt.at), 42).MatchStart(ctx, e.id)
			if err != nil || got != tt.want {
				t.Fatalf("MatchStart = %+v, %v; want %+v", got, err, tt.want)
			}
		})
	}

	unknown := []string{"NOSUCH", ""}
	for i := range w.roster {
		if r := w.roster[i]; r.id != "" && (r.kind == kindEmpty || r.kind == kindZero || r.noStart) {
			unknown = append(unknown, r.id)
		}
	}
	for _, id := range unknown {
		if got, err := w.MatchStart(ctx, id); err != nil || got != (source.MatchStart{}) {
			t.Errorf("MatchStart(%q) = %+v, %v; want Unknown", id, got, err)
		}
	}
}

func TestWorldNotModifiedAndContext(t *testing.T) {
	c := clock.NewFake(t0)
	w := NewWorld(c, 1)
	s := worldSnap(t, w)
	if _, err := w.Snapshot(context.Background(), s.Tag); !errors.Is(err, source.ErrNotModified) {
		t.Fatalf("same tag: err = %v, want ErrNotModified", err)
	}
	c.Advance(WorldRefresh)
	if next, err := w.Snapshot(context.Background(), s.Tag); err != nil || next.Tag == s.Tag {
		t.Fatalf("after refresh: %s, %v; want a new snapshot", next.Tag, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := w.Snapshot(ctx, ""); !errors.Is(err, context.Canceled) {
		t.Errorf("Snapshot with done ctx: err = %v", err)
	}
	if _, err := w.MatchStart(ctx, s.Servers[0].ID); !errors.Is(err, context.Canceled) {
		t.Errorf("MatchStart with done ctx: err = %v", err)
	}
}
