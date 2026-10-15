package fake

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ogredforman/ServerDog/internal/clock"
	"github.com/ogredforman/ServerDog/internal/source"
)

var t0 = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

func snap(id string, at time.Time) source.Snapshot {
	return source.Snapshot{
		ID: id, Tag: "tag-" + id, FetchedAt: at, RefreshEvery: 3 * time.Minute, Complete: true,
		Servers: []source.Server{{ID: "AAA", Players: 4, MaxPlayers: 16}},
	}
}

func TestSnapshotVisibilityAndNotModified(t *testing.T) {
	c := clock.NewFake(t0)
	s := New(c)
	ctx := context.Background()

	if _, err := s.Snapshot(ctx, ""); !errors.Is(err, ErrNoSnapshot) {
		t.Fatalf("before any snapshot: err = %v, want ErrNoSnapshot", err)
	}
	s.AddSnapshots(snap("1", t0), snap("2", t0.Add(3*time.Minute)))

	steps := []struct {
		name    string
		advance time.Duration
		prevTag string
		wantID  string
		wantErr error
	}{
		{"first visible", 0, "", "1", nil},
		{"unchanged", time.Minute, "tag-1", "", source.ErrNotModified},
		{"empty prevTag always returns", 0, "", "1", nil},
		{"next visible at FetchedAt", 2 * time.Minute, "tag-1", "2", nil},
		{"latest unchanged", time.Minute, "tag-2", "", source.ErrNotModified},
		{"stale tag gets latest", 0, "tag-1", "2", nil},
	}
	for _, st := range steps {
		c.Advance(st.advance)
		got, err := s.Snapshot(ctx, st.prevTag)
		if !errors.Is(err, st.wantErr) {
			t.Fatalf("%s: err = %v, want %v", st.name, err, st.wantErr)
		}
		if got.ID != st.wantID {
			t.Errorf("%s: snapshot ID = %q, want %q", st.name, got.ID, st.wantID)
		}
	}
}

func TestSnapshotReturnsCopy(t *testing.T) {
	c := clock.NewFake(t0)
	s := New(c)
	s.AddSnapshots(snap("1", t0))
	got, _ := s.Snapshot(context.Background(), "")
	got.Servers[0].Players = 0
	again, _ := s.Snapshot(context.Background(), "")
	if again.Servers[0].Players != 4 {
		t.Error("mutating a returned snapshot changed the scripted one")
	}
}

func TestInjectedErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		is   func(error) bool
	}{
		{"not modified", source.ErrNotModified, func(e error) bool { return errors.Is(e, source.ErrNotModified) }},
		{"retry after", source.RetryAfterError{After: 30 * time.Second}, func(e error) bool {
			var ra source.RetryAfterError
			return errors.As(e, &ra) && ra.After == 30*time.Second
		}},
		{"timeout", context.DeadlineExceeded, func(e error) bool { return errors.Is(e, context.DeadlineExceeded) }},
		{"generic", errors.New("upstream 503"), func(e error) bool { return e != nil && e.Error() == "upstream 503" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			s := New(clock.NewFake(t0))
			s.AddSnapshots(snap("1", t0))
			s.SetMatchStart("AAA", source.MatchStart{State: source.NotStarted})

			s.FailNextSnapshot(tt.err)
			if _, err := s.Snapshot(ctx, ""); !tt.is(err) {
				t.Errorf("Snapshot err = %v, want %v", err, tt.err)
			}
			if got, err := s.Snapshot(ctx, ""); err != nil || got.ID != "1" {
				t.Errorf("Snapshot after injected error = (%q, %v), want (\"1\", nil)", got.ID, err)
			}

			s.FailNextMatchStart("AAA", tt.err)
			if _, err := s.MatchStart(ctx, "BBB"); err != nil {
				t.Errorf("MatchStart(BBB) err = %v, want nil (error was for AAA)", err)
			}
			if _, err := s.MatchStart(ctx, "AAA"); !tt.is(err) {
				t.Errorf("MatchStart err = %v, want %v", err, tt.err)
			}
			if got, err := s.MatchStart(ctx, "AAA"); err != nil || got.State != source.NotStarted {
				t.Errorf("MatchStart after injected error = (%v, %v), want (not-started, nil)", got.State, err)
			}
		})
	}
}

func TestMatchStartScripted(t *testing.T) {
	ctx := context.Background()
	s := New(clock.NewFake(t0))
	exact := source.MatchStart{State: source.Exact, At: t0.Add(-5 * time.Minute)}
	s.SetMatchStart("AAA", exact)

	if got, err := s.MatchStart(ctx, "AAA"); err != nil || got != exact {
		t.Errorf("MatchStart(AAA) = (%v, %v), want (%v, nil)", got, err, exact)
	}
	if got, err := s.MatchStart(ctx, "ZZZ"); err != nil || got.State != source.Unknown {
		t.Errorf("MatchStart(unscripted) = (%v, %v), want (unknown, nil)", got.State, err)
	}
	s.SetMatchStart("AAA", source.MatchStart{State: source.NotStarted})
	if got, _ := s.MatchStart(ctx, "AAA"); got.State != source.NotStarted {
		t.Errorf("after re-scripting, State = %v, want not-started", got.State)
	}
}

func TestCallsRecorded(t *testing.T) {
	c := clock.NewFake(t0)
	s := New(c)
	s.AddSnapshots(snap("1", t0))
	ctx := context.Background()

	s.Snapshot(ctx, "")
	c.Advance(2 * time.Second)
	s.FailNextMatchStart("AAA", source.RetryAfterError{After: time.Minute})
	s.MatchStart(ctx, "AAA")
	c.Advance(time.Second)
	s.Snapshot(ctx, "tag-1")

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.MatchStart(cancelled, "BBB"); !errors.Is(err, context.Canceled) {
		t.Errorf("MatchStart with cancelled ctx: err = %v, want context.Canceled", err)
	}
	if _, err := s.Snapshot(cancelled, ""); !errors.Is(err, context.Canceled) {
		t.Errorf("Snapshot with cancelled ctx: err = %v, want context.Canceled", err)
	}

	want := []Call{
		{Method: MethodSnapshot, At: t0},
		{Method: MethodMatchStart, ServerID: "AAA", At: t0.Add(2 * time.Second)},
		{Method: MethodSnapshot, PrevTag: "tag-1", At: t0.Add(3 * time.Second)},
	}
	got := s.Calls()
	if len(got) != len(want) {
		t.Fatalf("recorded %d calls, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("call %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}
