package scheduler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/ogredforman/ServerDog/internal/clock"
	"github.com/ogredforman/ServerDog/internal/publish"
	"github.com/ogredforman/ServerDog/internal/source"
	"github.com/ogredforman/ServerDog/internal/source/fake"
	"github.com/ogredforman/ServerDog/internal/tracker"
)

// event is one upstream request or publish, in the order the scheduler made them.
type event struct {
	kind string // "snapshot", "detail" or "publish"
	id   string // detail: server ID
	at   time.Time
	snap *source.Snapshot // snapshot: the new snapshot, nil on error or not modified
	file *publish.File    // publish
}

// harness runs a Scheduler against a Source and the fake clock, logging every upstream
// request and publish as an event.
type harness struct {
	source.Source
	clk  *clock.Fake
	opts Options

	failPublishes int // the first n publishes return an error and are not recorded

	mu     sync.Mutex
	events []event
}

func defaultOptions() Options {
	return Options{BurstRate: 4, BurstBudget: 200, SweepRate: 1.5, Concurrency: 1, PollDelay: pollDelay}
}

func newHarness(clk *clock.Fake, src source.Source, o Options) *harness {
	return &harness{Source: src, clk: clk, opts: o}
}

func (h *harness) add(e event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = append(h.events, e)
}

func (h *harness) Snapshot(ctx context.Context, prevTag string) (source.Snapshot, error) {
	snap, err := h.Source.Snapshot(ctx, prevTag)
	e := event{kind: "snapshot", at: h.clk.Now()}
	if err == nil {
		e.snap = &snap
	}
	h.add(e)
	return snap, err
}

func (h *harness) MatchStart(ctx context.Context, id string) (source.MatchStart, error) {
	h.add(event{kind: "detail", id: id, at: h.clk.Now()})
	return h.Source.MatchStart(ctx, id)
}

// run runs a Scheduler over h until the clock passes t0+end, stepping the clock from one
// pending wake-up to the next, then shuts it down.
func (h *harness) run(t *testing.T, window time.Duration, end time.Duration) {
	t.Helper()
	trk := tracker.New(tracker.Options{StartingWindow: window, DropPct: 30, DropMin: 25})
	o := h.opts
	o.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	o.Publish = func(_ context.Context, f publish.File) error {
		if h.failPublishes > 0 {
			h.failPublishes--
			return errors.New("disk full")
		}
		h.add(event{kind: "publish", at: h.clk.Now(), file: &f})
		return nil
	}
	s, err := New(h, h.clk, trk, o)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() { done <- s.Run(ctx) }()
	for {
		h.clk.BlockUntil(1)
		next, _ := h.clk.Next()
		if next.After(t0.Add(end)) {
			break
		}
		h.clk.Set(next)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run = %v, want context.Canceled", err)
	}
}

// cycle is one new snapshot with the requests and publishes that followed it.
type cycle struct {
	snap    source.Snapshot
	burst   []event // details before the cycle's first publish
	publish []event
	sweep   []event // details after it
}

func (h *harness) cycles() []cycle {
	var out []cycle
	for _, e := range h.events {
		switch {
		case e.kind == "snapshot" && e.snap != nil:
			out = append(out, cycle{snap: *e.snap})
		case len(out) == 0:
		case e.kind == "publish":
			out[len(out)-1].publish = append(out[len(out)-1].publish, e)
		case e.kind == "detail" && len(out[len(out)-1].publish) == 0:
			out[len(out)-1].burst = append(out[len(out)-1].burst, e)
		case e.kind == "detail":
			out[len(out)-1].sweep = append(out[len(out)-1].sweep, e)
		}
	}
	return out
}

// checkPolite asserts the politeness rules on every cycle (SPEC §5 Polite upstream client,
// §7.1, §7.2): no detail for an empty or unlisted server, at most one detail per server per
// snapshot, the burst within budget, request spacing within the rate in force, and no sweep
// request once the next snapshot is due.
func (h *harness) checkPolite(t *testing.T) {
	t.Helper()
	burstGap := time.Duration(float64(time.Second) / h.opts.BurstRate)
	sweepGap := time.Duration(float64(time.Second) / h.opts.SweepRate)
	var prev *event
	var prevGap time.Duration
	for _, c := range h.cycles() {
		players := map[string]int{}
		for _, s := range c.snap.Servers {
			players[s.ID] = s.Players
		}
		due := c.snap.FetchedAt.Add(c.snap.RefreshEvery + h.opts.PollDelay)
		if len(c.burst) > h.opts.BurstBudget {
			t.Errorf("snapshot %s: %d burst requests, budget %d", c.snap.ID, len(c.burst), h.opts.BurstBudget)
		}
		seen := map[string]bool{}
		for i, e := range append(append([]event{}, c.burst...), c.sweep...) {
			if players[e.id] == 0 {
				t.Errorf("snapshot %s: detail for empty or unlisted server %s", c.snap.ID, e.id)
			}
			if seen[e.id] {
				t.Errorf("snapshot %s: second detail for %s", c.snap.ID, e.id)
			}
			seen[e.id] = true
			if prev != nil && e.at.Sub(prev.at) < prevGap {
				t.Errorf("detail %s at +%v only %v after the previous, want >= %v", e.id, e.at.Sub(t0), e.at.Sub(prev.at), prevGap)
			}
			if i >= len(c.burst) && !e.at.Before(due) {
				t.Errorf("snapshot %s: sweep detail %s at +%v, next snapshot due +%v", c.snap.ID, e.id, e.at.Sub(t0), due.Sub(t0))
			}
			prev, prevGap = &e, sweepGap
			if i < len(c.burst) {
				prevGap = burstGap
			}
		}
	}
}

func srv(id string, players int, variant string) source.Server {
	return source.Server{ID: id, Players: players, MaxPlayers: 64, Map: source.Map{Variant: variant}}
}

func withServers(s source.Snapshot, servers ...source.Server) source.Snapshot {
	s.Servers = servers
	return s
}

func exactAt(d time.Duration) source.MatchStart {
	return source.MatchStart{State: source.Exact, At: t0.Add(d)}
}

func ids(es []event) []string {
	var out []string
	for _, e := range es {
		out = append(out, e.id)
	}
	return out
}

func published(t *testing.T, f *publish.File, id string) publish.Server {
	t.Helper()
	for _, s := range f.Servers {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("server %s not published", id)
	return publish.Server{}
}

func checkState(t *testing.T, f *publish.File, id, state string, at time.Duration) {
	t.Helper()
	s := published(t, f, id)
	if s.State != state || s.MatchStart == nil || *s.MatchStart != t0.Add(at).Unix() {
		got := "nil"
		if s.MatchStart != nil {
			got = time.Unix(*s.MatchStart, 0).Sub(t0).String()
		}
		t.Errorf("%s published %s at +%s, want %s at +%v", id, s.State, got, state, at)
	}
}

// TestCycleBurstFetchesChangeBeforePublish: a map change is checked in the burst of the same
// cycle and published exact; an empty server is never fetched (SPEC §7.1 steps 3–5, §7.2).
func TestCycleBurstFetchesChangeBeforePublish(t *testing.T) {
	clk := clock.NewFake(t0)
	src := fake.New(clk)
	src.AddSnapshots(
		withServers(snap("1", "a", 0), srv("A", 10, "m1"), srv("B", 0, "m1"), srv("C", 10, "m1")),
		withServers(snap("2", "b", refresh), srv("A", 10, "m2"), srv("B", 0, "m2"), srv("C", 10, "m1")),
	)
	src.SetMatchStart("A", exactAt(refresh-5*time.Second))
	src.SetMatchStart("C", exactAt(-10*time.Minute))
	h := newHarness(clk, src, defaultOptions())
	h.run(t, 20*time.Minute, 2*refresh+30*time.Second)
	h.checkPolite(t)

	cs := h.cycles()
	if len(cs) != 2 {
		t.Fatalf("%d cycles, want 2", len(cs))
	}
	if got := ids(cs[0].burst); len(got) != 0 {
		t.Errorf("cycle 1 burst = %v, want none (new servers are P2)", got)
	}
	if got := ids(cs[0].sweep); len(got) != 2 {
		t.Errorf("cycle 1 sweep = %v, want A and C", got)
	}
	if got := ids(cs[1].burst); len(got) != 1 || got[0] != "A" {
		t.Errorf("cycle 2 burst = %v, want [A]", got)
	}
	if len(cs[1].publish) != 1 {
		t.Fatalf("cycle 2 published %d times, want 1", len(cs[1].publish))
	}
	checkState(t, cs[1].publish[0].file, "A", "exact", refresh-5*time.Second)
	if st := published(t, cs[1].publish[0].file, "B").State; st != "unknown" {
		t.Errorf("empty B published %s, want unknown", st)
	}
}

// TestCycleStartingWindow: a changed server that stays not-started is rechecked in the burst
// (P1) only while inside StartingWindow, then falls back to the sweep (SPEC §7.2 rule 5).
func TestCycleStartingWindow(t *testing.T) {
	clk := clock.NewFake(t0)
	src := fake.New(clk)
	src.AddSnapshots(withServers(snap("1", "a", 0), srv("A", 10, "m1")))
	for i, tag := range []string{"b", "c", "d", "e", "f"} {
		src.AddSnapshots(withServers(snap(tag, tag, time.Duration(i+1)*refresh), srv("A", 10, "m2")))
	}
	src.SetMatchStart("A", source.MatchStart{State: source.NotStarted})
	h := newHarness(clk, src, defaultOptions())
	// The change is observed at +180s; a 7 min window covers snapshots up to +540s.
	h.run(t, 7*time.Minute, 5*refresh+90*time.Second)
	h.checkPolite(t)

	cs := h.cycles()
	if len(cs) != 6 {
		t.Fatalf("%d cycles, want 6", len(cs))
	}
	for i, c := range cs[1:] {
		inWindow := i < 3
		if got := len(c.burst) == 1; got != inWindow {
			t.Errorf("snapshot %s: A in burst = %v, want %v", c.snap.ID, got, inWindow)
		}
		if got := len(c.sweep) == 1; got == inWindow {
			t.Errorf("snapshot %s: A in sweep = %v, want %v", c.snap.ID, got, !inWindow)
		}
		want := "starting"
		if !inWindow {
			want = "estimate"
		}
		checkState(t, c.publish[0].file, "A", want, refresh)
	}
}

// TestCycleBudgetLeavesEstimates: P0 servers beyond BurstBudget publish as estimates and are
// checked in the sweep that follows (SPEC §7.1 step 4, §7.3 step 4).
func TestCycleBudgetLeavesEstimates(t *testing.T) {
	clk := clock.NewFake(t0)
	src := fake.New(clk)
	names := []string{"A", "B", "C", "D", "E"}
	var before, after []source.Server
	for _, n := range names {
		before = append(before, srv(n, 10, "m1"))
		after = append(after, srv(n, 10, "m2"))
		src.SetMatchStart(n, exactAt(refresh-5*time.Second))
	}
	src.AddSnapshots(withServers(snap("1", "a", 0), before...), withServers(snap("2", "b", refresh), after...))
	o := defaultOptions()
	o.BurstBudget = 2
	h := newHarness(clk, src, o)
	h.run(t, 20*time.Minute, refresh+60*time.Second)
	h.checkPolite(t)

	cs := h.cycles()
	if len(cs) != 2 {
		t.Fatalf("%d cycles, want 2", len(cs))
	}
	c := cs[1]
	if got := ids(c.burst); len(got) != 2 || got[0] != "A" || got[1] != "B" {
		t.Errorf("burst = %v, want [A B]", got)
	}
	f := c.publish[0].file
	checkState(t, f, "A", "exact", refresh-5*time.Second)
	checkState(t, f, "B", "exact", refresh-5*time.Second)
	for _, n := range names[2:] {
		checkState(t, f, n, "estimate", refresh)
	}
	if got := ids(c.sweep); len(got) != 3 || got[0] != "C" {
		t.Errorf("sweep = %v, want [C D E]", got)
	}
}

// TestCycleStaleAfterOutage: with no new snapshot, the last one is republished once as stale
// just after 2 × RefreshEvery + 60s, and the next good snapshot publishes fresh (SPEC §7.1 step 2).
func TestCycleStaleAfterOutage(t *testing.T) {
	clk := clock.NewFake(t0)
	src := fake.New(clk)
	src.AddSnapshots(
		withServers(snap("1", "a", 0), srv("A", 10, "m1")),
		withServers(snap("2", "b", 600*time.Second), srv("A", 10, "m1")),
	)
	// Polls fail at +190, +205, +235, +295 and +415; the one at +655 succeeds.
	down := outage{Source: src, clk: clk, from: t0.Add(time.Second), to: t0.Add(600 * time.Second)}
	h := newHarness(clk, down, defaultOptions())
	h.run(t, 20*time.Minute, 700*time.Second)

	var pubs []event
	for _, e := range h.events {
		if e.kind == "publish" {
			pubs = append(pubs, e)
		}
	}
	want := []struct {
		at, snapshotAt time.Duration
		stale          bool
	}{
		{0, 0, false},
		{2*refresh + time.Minute + time.Second, 0, true},
		{655 * time.Second, 600 * time.Second, false},
	}
	if len(pubs) != len(want) {
		t.Fatalf("%d publishes, want %d", len(pubs), len(want))
	}
	for i, w := range want {
		p := pubs[i]
		if p.at.Sub(t0) != w.at || p.file.Stale != w.stale || p.file.SnapshotAt != t0.Add(w.snapshotAt).Unix() {
			t.Errorf("publish %d at +%v stale=%v snapshotAt=%d, want +%v stale=%v snapshot +%v",
				i, p.at.Sub(t0), p.file.Stale, p.file.SnapshotAt-t0.Unix(), w.at, w.stale, w.snapshotAt)
		}
	}
}

// outage fails every Snapshot call made in [from, to).
type outage struct {
	source.Source
	clk      clock.Clock
	from, to time.Time
}

func (o outage) Snapshot(ctx context.Context, prevTag string) (source.Snapshot, error) {
	if now := o.clk.Now(); !now.Before(o.from) && now.Before(o.to) {
		return source.Snapshot{}, errUpstream
	}
	return o.Source.Snapshot(ctx, prevTag)
}

// TestCycleRetryAfterPausesDetails: a throttled detail request holds back every upstream
// request for Retry-After, and the server is not retried in the same snapshot (SPEC §5).
func TestCycleRetryAfterPausesDetails(t *testing.T) {
	clk := clock.NewFake(t0)
	src := fake.New(clk)
	src.AddSnapshots(withServers(snap("1", "a", 0), srv("A", 10, "m1"), srv("B", 10, "m1"), srv("C", 10, "m1")))
	src.FailNextMatchStart("A", source.RetryAfterError{After: 30 * time.Second})
	h := newHarness(clk, src, defaultOptions())
	h.run(t, 20*time.Minute, 100*time.Second)
	h.checkPolite(t)

	var reqs []event
	for _, e := range h.events {
		if e.kind != "publish" {
			reqs = append(reqs, e)
		}
	}
	if len(reqs) != 4 || reqs[1].id != "A" {
		t.Fatalf("requests = %+v, want snapshot, A, B, C", reqs)
	}
	if gap := reqs[2].at.Sub(reqs[1].at); gap < 30*time.Second {
		t.Errorf("next request %v after the throttled one, want >= 30s", gap)
	}
}

// TestCycleConcurrency: with parallel requests every P0 server is still checked exactly once,
// before the publish.
func TestCycleConcurrency(t *testing.T) {
	clk := clock.NewFake(t0)
	src := fake.New(clk)
	var before, after []source.Server
	for _, n := range []string{"A", "B", "C", "D", "E", "F", "G", "H", "I", "J"} {
		before = append(before, srv(n, 10, "m1"))
		after = append(after, srv(n, 10, "m2"))
		src.SetMatchStart(n, exactAt(refresh-5*time.Second))
	}
	src.AddSnapshots(withServers(snap("1", "a", 0), before...), withServers(snap("2", "b", refresh), after...))
	o := defaultOptions()
	o.Concurrency = 3
	h := newHarness(clk, src, o)
	h.run(t, 20*time.Minute, refresh+60*time.Second)

	cs := h.cycles()
	if len(cs) != 2 {
		t.Fatalf("%d cycles, want 2", len(cs))
	}
	seen := map[string]int{}
	for _, e := range cs[1].burst {
		seen[e.id]++
	}
	for _, s := range after {
		if seen[s.ID] != 1 {
			t.Errorf("%s fetched %d times in the burst, want 1", s.ID, seen[s.ID])
		}
		checkState(t, cs[1].publish[0].file, s.ID, "exact", refresh-5*time.Second)
	}
	if len(cs[1].sweep) != 0 {
		t.Errorf("sweep = %v, want nothing left", ids(cs[1].sweep))
	}
}

// TestCyclePolitenessWorld runs the generated world for several cycles with a small burst
// budget and checks the politeness rules on every request (SPEC §9 Upstream load, §12).
func TestCyclePolitenessWorld(t *testing.T) {
	clk := clock.NewFake(t0)
	o := defaultOptions()
	o.BurstBudget = 20
	h := newHarness(clk, fake.NewWorld(clk, 7), o)
	h.run(t, 20*time.Minute, 15*time.Minute)
	h.checkPolite(t)

	cs := h.cycles()
	var burst, sweep, full int
	for _, c := range cs {
		burst += len(c.burst)
		sweep += len(c.sweep)
		if len(c.burst) == o.BurstBudget {
			full++
		}
	}
	if len(cs) < 10 || burst == 0 || sweep == 0 {
		t.Fatalf("%d cycles, %d burst and %d sweep requests: scenario too quiet to check", len(cs), burst, sweep)
	}
	t.Logf("%d cycles, %d burst (%d at budget), %d sweep requests", len(cs), burst, full, sweep)
}

// TestCyclePublishFailureKeepsRunning: a failed publish is logged and the cycle carries on;
// the next snapshot publishes normally.
func TestCyclePublishFailureKeepsRunning(t *testing.T) {
	clk := clock.NewFake(t0)
	src := fake.New(clk)
	src.AddSnapshots(
		withServers(snap("1", "a", 0), srv("A", 10, "m1")),
		withServers(snap("2", "b", refresh), srv("A", 10, "m1")),
	)
	h := newHarness(clk, src, defaultOptions())
	h.failPublishes = 1
	h.run(t, 20*time.Minute, refresh+60*time.Second)
	h.checkPolite(t)

	cs := h.cycles()
	if len(cs) != 2 || len(cs[0].publish) != 0 || len(cs[1].publish) != 1 {
		t.Fatalf("got %d cycles, want 2 with the first publish failed and the second published", len(cs))
	}
	// With no publish event, cycles() files the cycle's sweep requests under burst.
	if got := ids(append(cs[0].burst, cs[0].sweep...)); len(got) != 1 {
		t.Errorf("cycle 1 details = %v after a failed publish, want the sweep of [A]", got)
	}
}
