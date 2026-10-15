package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ogredforman/ServerDog/internal/clock"
	"github.com/ogredforman/ServerDog/internal/source"
	"github.com/ogredforman/ServerDog/internal/source/fake"
)

const (
	refresh   = 180 * time.Second
	pollDelay = 10 * time.Second
)

var errUpstream = errors.New("upstream 503")

func newPoller(t *testing.T, snaps ...source.Snapshot) (*clock.Fake, *fake.Source, *Limiter, *Poller) {
	t.Helper()
	clk := clock.NewFake(t0)
	src := fake.New(clk)
	src.AddSnapshots(snaps...)
	lim, err := NewLimiter(clk, 4)
	if err != nil {
		t.Fatal(err)
	}
	return clk, src, lim, NewPoller(src, clk, lim, pollDelay)
}

func snap(id, tag string, fetchedAt time.Duration) source.Snapshot {
	return source.Snapshot{ID: id, Tag: tag, FetchedAt: t0.Add(fetchedAt), RefreshEvery: refresh, Complete: true}
}

// TestPollerTiming polls at each Due in turn and checks the outcome and the next Due
// (SPEC §7.1 steps 1–2).
func TestPollerTiming(t *testing.T) {
	type step struct {
		inject  error         // injected into the next Snapshot call
		wantErr error         // nil, source.ErrNotModified or the injected error
		wantID  string        // snapshot ID on success
		wantDue time.Duration // next Due, relative to the poll time
	}
	s1 := snap("1", "a", -30*time.Second)
	s2 := snap("2", "b", 150*time.Second)

	tests := []struct {
		name  string
		snaps []source.Snapshot
		steps []step
	}{
		{
			name:  "new snapshot waits FetchedAt+RefreshEvery+PollDelay; 304 retries every 15s",
			snaps: []source.Snapshot{s1, s2},
			steps: []step{
				{wantID: "1", wantDue: 160 * time.Second}, // -30 + 180 + 10
				{wantID: "2", wantDue: 180 * time.Second}, // at +160: s2 fetched at +150, due +340
				{wantErr: source.ErrNotModified, wantDue: 15 * time.Second},
				{wantErr: source.ErrNotModified, wantDue: 15 * time.Second},
			},
		},
		{
			name:  "errors back off 15s to 5 min, success resets",
			snaps: []source.Snapshot{s1, s2},
			steps: []step{
				{inject: errUpstream, wantErr: errUpstream, wantDue: 15 * time.Second},
				{inject: errUpstream, wantErr: errUpstream, wantDue: 30 * time.Second},
				{inject: errUpstream, wantErr: errUpstream, wantDue: 60 * time.Second},
				{inject: errUpstream, wantErr: errUpstream, wantDue: 120 * time.Second},
				{inject: errUpstream, wantErr: errUpstream, wantDue: 240 * time.Second},
				{inject: errUpstream, wantErr: errUpstream, wantDue: 5 * time.Minute},
				{inject: errUpstream, wantErr: errUpstream, wantDue: 5 * time.Minute},
				// at +17m45s: s2 is the latest; FetchedAt +150s is long past.
				{wantID: "2", wantDue: 150*time.Second + 190*time.Second - 1065*time.Second},
				// Due is in the past, so the next poll is 1s later.
				{inject: errUpstream, wantErr: errUpstream, wantDue: 15 * time.Second},
			},
		},
		{
			name:  "not modified resets the error backoff",
			snaps: []source.Snapshot{s1},
			steps: []step{
				{wantID: "1", wantDue: 160 * time.Second},
				{inject: errUpstream, wantErr: errUpstream, wantDue: 15 * time.Second},
				{inject: errUpstream, wantErr: errUpstream, wantDue: 30 * time.Second},
				{wantErr: source.ErrNotModified, wantDue: 15 * time.Second},
				{inject: errUpstream, wantErr: errUpstream, wantDue: 15 * time.Second},
			},
		},
		{
			name:  "Retry-After is honored and longer than the backoff",
			snaps: []source.Snapshot{s1},
			steps: []step{
				{inject: source.RetryAfterError{After: 90 * time.Second}, wantErr: source.RetryAfterError{After: 90 * time.Second}, wantDue: 90 * time.Second},
				{inject: errUpstream, wantErr: errUpstream, wantDue: 30 * time.Second},
				{inject: source.RetryAfterError{After: 5 * time.Second}, wantErr: source.RetryAfterError{After: 5 * time.Second}, wantDue: 60 * time.Second},
				{wantID: "1"},
			},
		},
		{
			name:  "same snapshot ID without a tag counts as not modified",
			snaps: []source.Snapshot{snap("1", "", 0)},
			steps: []step{
				{wantID: "1", wantDue: 190 * time.Second},
				{wantErr: source.ErrNotModified, wantDue: 15 * time.Second},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clk, src, _, p := newPoller(t, tt.snaps...)
			if !p.Due().IsZero() {
				t.Fatalf("first Due = %v, want zero (poll immediately)", p.Due())
			}
			for i, st := range tt.steps {
				// Poll at Due, but at least 1s after the last poll so the limiter has a free slot.
				if i > 0 {
					clk.Set(later(p.Due(), clk.Now().Add(time.Second)))
				}
				if st.inject != nil {
					src.FailNextSnapshot(st.inject)
				}
				at := clk.Now()
				got, err := p.Poll(context.Background())
				switch {
				case st.wantErr == nil && err != nil:
					t.Fatalf("step %d: Poll error %v, want snapshot %q", i, err, st.wantID)
				case st.wantErr != nil && !errors.Is(err, st.wantErr):
					t.Fatalf("step %d: Poll error %v, want %v", i, err, st.wantErr)
				case st.wantErr == nil && got.ID != st.wantID:
					t.Fatalf("step %d: snapshot %q, want %q", i, got.ID, st.wantID)
				}
				if st.wantDue != 0 {
					if due := p.Due().Sub(at); due != st.wantDue {
						t.Fatalf("step %d: next Due in %v, want %v", i, due, st.wantDue)
					}
				}
			}
		})
	}
}

func later(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func TestPollerPassesTagBack(t *testing.T) {
	clk, src, _, p := newPoller(t, snap("1", "a", 0), snap("2", "b", 100*time.Second))
	ctx := context.Background()
	for range 3 {
		if d := p.Due(); d.After(clk.Now()) {
			clk.Set(d)
		}
		p.Poll(ctx)
	}
	var tags []string
	for _, c := range src.Calls() {
		tags = append(tags, c.PrevTag)
	}
	want := []string{"", "a", "b"}
	if len(tags) != len(want) {
		t.Fatalf("prevTags %q, want %q", tags, want)
	}
	for i := range want {
		if tags[i] != want[i] {
			t.Fatalf("prevTags %q, want %q", tags, want)
		}
	}
}

// TestPollerRetryAfterPausesLimiter checks that a Retry-After on the snapshot also holds back
// every other upstream request sharing the limiter.
func TestPollerRetryAfterPausesLimiter(t *testing.T) {
	clk, src, lim, p := newPoller(t, snap("1", "a", 0))
	src.FailNextSnapshot(source.RetryAfterError{After: 90 * time.Second})
	if _, err := p.Poll(context.Background()); err == nil {
		t.Fatal("Poll succeeded, want RetryAfterError")
	}
	got := driveWaits(t, clk, lim, []time.Duration{90 * time.Second})
	if got[0] != 90*time.Second {
		t.Fatalf("next upstream slot at +%v, want +90s", got[0])
	}
}

// TestPollerLoop runs Wait+Poll in a goroutine on the fake clock and checks the recorded
// upstream call times.
func TestPollerLoop(t *testing.T) {
	clk, src, _, p := newPoller(t, snap("1", "a", -30*time.Second), snap("2", "b", 150*time.Second))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	want := []time.Duration{0, 160 * time.Second, 340 * time.Second, 355 * time.Second}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range want {
			if p.Wait(ctx) != nil {
				return
			}
			p.Poll(ctx)
		}
	}()
	for _, w := range want[1:] {
		clk.BlockUntil(1)
		clk.Advance(t0.Add(w).Sub(clk.Now()))
	}
	<-done

	calls := src.Calls()
	if len(calls) != len(want) {
		t.Fatalf("%d calls, want %d: %+v", len(calls), len(want), calls)
	}
	for i, c := range calls {
		if c.Method != fake.MethodSnapshot || c.At.Sub(t0) != want[i] {
			t.Fatalf("call %d: %s at +%v, want Snapshot at +%v", i, c.Method, c.At.Sub(t0), want[i])
		}
	}
}

func TestPollerCancelIsNotABackoff(t *testing.T) {
	clk, src, _, p := newPoller(t, snap("1", "a", 0))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Poll(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Poll with done ctx = %v, want context.Canceled", err)
	}
	if !p.Due().IsZero() {
		t.Fatalf("Due after cancelled poll = %v, want unchanged (zero)", p.Due())
	}
	clk.Advance(time.Second)
	src.FailNextSnapshot(errUpstream)
	p.Poll(context.Background())
	if due := p.Due().Sub(clk.Now()); due != BackoffMin {
		t.Fatalf("first real error backs off %v, want %v", due, BackoffMin)
	}
}
