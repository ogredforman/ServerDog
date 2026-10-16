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
	kind string // "snapshot", "detail", "publish" or "save"
	id   string // detail: server ID
	at   time.Time
	snap *source.Snapshot // snapshot: the new snapshot, nil on error or not modified
	file *publish.File    // publish
	err  error            // detail: the request's error
}

// harness runs a Scheduler against a Source and the fake clock, logging every upstream
// request and publish as an event.
type harness struct {
	source.Source
	clk  *clock.Fake
	opts Options

	failPublishes int // the first n publishes return an error and are not recorded
	failSaves     int // the first n saves return an error (still recorded)

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
	at := h.clk.Now()
	ms, err := h.Source.MatchStart(ctx, id)
	h.add(event{kind: "detail", id: id, at: at, err: err})
	return ms, err
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
	o.Save = func(ctx context.Context) error {
		h.add(event{kind: "save", at: h.clk.Now()})
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if h.failSaves > 0 {
			h.failSaves--
			return errors.New("disk full")
		}
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
	saves   []event
}

func (h *harness) cycles() []cycle {
	var out []cycle
	for _, e := range h.events {
		switch {
		case e.kind == "snapshot" && e.snap != nil:
			out = append(out, cycle{snap: *e.snap})
		case len(out) == 0:
		case e.kind == "save":
			out[len(out)-1].saves = append(out[len(out)-1].saves, e)
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
		if e.kind != "publish" && e.kind != "save" {
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

// TestCycleSavesTrackerAfterSweep: the tracker is saved once per cycle, after the sweep's
// details, and a failed save neither stops the scheduler nor skips the next cycle (SPEC §7.1
// step 7).
func TestCycleSavesTrackerAfterSweep(t *testing.T) {
	clk := clock.NewFake(t0)
	src := fake.New(clk)
	src.AddSnapshots(
		withServers(snap("1", "a", 0), srv("A", 10, "m1"), srv("B", 10, "m1")),
		withServers(snap("2", "b", refresh), srv("A", 10, "m1"), srv("B", 10, "m1")),
	)
	h := newHarness(clk, src, defaultOptions())
	h.failSaves = 1
	h.run(t, 20*time.Minute, refresh+60*time.Second)
	h.checkPolite(t)

	cs := h.cycles()
	if len(cs) != 2 {
		t.Fatalf("%d cycles, want 2", len(cs))
	}
	for i, c := range cs {
		if len(c.saves) != 1 {
			t.Fatalf("cycle %d saved %d times, want 1", i+1, len(c.saves))
		}
		if len(c.publish) != 1 {
			t.Fatalf("cycle %d published %d times, want 1", i+1, len(c.publish))
		}
	}
	// Events are logged in order: the save follows the cycle's last detail request.
	for i, e := range h.events {
		if e.kind != "save" {
			continue
		}
		for _, later := range h.events[i+1:] {
			if later.kind == "snapshot" {
				break
			}
			if later.kind == "detail" {
				t.Errorf("detail %s at +%v after the cycle's save", later.id, later.at.Sub(t0))
			}
		}
	}
	if got := ids(cs[0].sweep); len(got) != 2 {
		t.Errorf("cycle 1 sweep = %v, want A and B before the save", got)
	}
}

// TestCycleProviderStartsSkipDetails: when the snapshot carries every server's start, no
// detail is ever fetched and the provider's start is published exact (SPEC §7.2 rule 8).
func TestCycleProviderStartsSkipDetails(t *testing.T) {
	clk := clock.NewFake(t0)
	src := fake.New(clk)
	withStart := func(s source.Server, d time.Duration) source.Server {
		ms := exactAt(d)
		s.MatchStart = &ms
		return s
	}
	src.AddSnapshots(
		withServers(snap("1", "a", 0), withStart(srv("A", 10, "m1"), -10*time.Minute), withStart(srv("B", 10, "m1"), -3*time.Minute)),
		withServers(snap("2", "b", refresh), withStart(srv("A", 10, "m2"), refresh-5*time.Second), withStart(srv("B", 60, "m1"), -3*time.Minute)),
	)
	h := newHarness(clk, src, defaultOptions())
	h.run(t, 20*time.Minute, 2*refresh+30*time.Second)
	h.checkPolite(t)

	if n := len(src.Calls()) - countSnapshotCalls(src.Calls()); n != 0 {
		t.Errorf("%d detail calls, want none when the snapshot carries starts", n)
	}
	cs := h.cycles()
	if len(cs) != 2 || len(cs[1].publish) != 1 {
		t.Fatalf("got %d cycles, want 2 with one publish each", len(cs))
	}
	checkState(t, cs[0].publish[0].file, "A", "exact", -10*time.Minute)
	// A map change confirmed by the provider's start publishes exact, not as an estimate.
	checkState(t, cs[1].publish[0].file, "A", "exact", refresh-5*time.Second)
	checkState(t, cs[1].publish[0].file, "B", "exact", -3*time.Minute)
}

func countSnapshotCalls(calls []fake.Call) int {
	n := 0
	for _, c := range calls {
		if c.Method == fake.MethodSnapshot {
			n++
		}
	}
	return n
}

// throttle answers every detail request with a RetryAfterError while the clock is inside one
// of its windows (offsets from t0).
type throttle struct {
	source.Source
	clk     clock.Clock
	after   time.Duration
	windows [][2]time.Duration
}

func (th throttle) MatchStart(ctx context.Context, id string) (source.MatchStart, error) {
	now := th.clk.Now().Sub(t0)
	for _, w := range th.windows {
		if now >= w[0] && now < w[1] {
			return source.MatchStart{}, source.RetryAfterError{After: th.after}
		}
	}
	return th.Source.MatchStart(ctx, id)
}

// checkRates asserts the total upstream rate cap over every request, snapshot or detail,
// across burst, sweep and cycle boundaries: no two requests closer than the faster configured
// rate allows, and none inside a Retry-After pause (SPEC §8 Data provider). It returns the
// longest run of consecutive throttled details.
func (h *harness) checkRates(t *testing.T) int {
	t.Helper()
	gap := time.Duration(float64(time.Second) / max(h.opts.BurstRate, h.opts.SweepRate))
	var prev *event
	var pausedUntil time.Time
	run, longest := 0, 0
	for i := range h.events {
		e := &h.events[i]
		if e.kind == "publish" || e.kind == "save" {
			continue
		}
		if prev != nil && e.at.Sub(prev.at) < gap {
			t.Errorf("%s %s at +%v only %v after the previous request, want >= %v", e.kind, e.id, e.at.Sub(t0), e.at.Sub(prev.at), gap)
		}
		if e.at.Before(pausedUntil) {
			t.Errorf("%s %s at +%v inside a Retry-After pause until +%v", e.kind, e.id, e.at.Sub(t0), pausedUntil.Sub(t0))
		}
		var ra source.RetryAfterError
		switch {
		case errors.As(e.err, &ra):
			pausedUntil = e.at.Add(ra.After)
			run++
			longest = max(longest, run)
		case e.kind == "detail" && e.err == nil:
			run = 0
		}
		prev = e
	}
	return longest
}

// TestCycleRepeatedThrottleCutsBurst: a second throttle in a row ends the burst at once, so the
// cycle publishes without waiting and unchecked P0 servers stay estimates; the sweep checks
// them once the detail pause is over (SPEC §7.3 step 4).
func TestCycleRepeatedThrottleCutsBurst(t *testing.T) {
	clk := clock.NewFake(t0)
	inner := fake.New(clk)
	names := []string{"A", "B", "C", "D", "E"}
	var before, after []source.Server
	for _, n := range names {
		before = append(before, srv(n, 10, "m1"))
		after = append(after, srv(n, 10, "m2"))
		inner.SetMatchStart(n, exactAt(refresh-5*time.Second))
	}
	inner.AddSnapshots(withServers(snap("1", "a", 0), before...), withServers(snap("2", "b", refresh), after...))
	poll := refresh + pollDelay
	src := throttle{Source: inner, clk: clk, after: time.Second, windows: [][2]time.Duration{{poll, poll + 2*time.Second}}}
	h := newHarness(clk, src, defaultOptions())
	h.run(t, 20*time.Minute, refresh+90*time.Second)
	h.checkPolite(t)
	h.checkRates(t)

	cs := h.cycles()
	if len(cs) != 2 {
		t.Fatalf("%d cycles, want 2", len(cs))
	}
	c := cs[1]
	if got := ids(c.burst); len(got) != 2 || got[0] != "A" || got[1] != "B" {
		t.Fatalf("burst = %v, want [A B] (cut after the second throttle)", got)
	}
	if got := c.publish[0].at; !got.Equal(c.burst[1].at) {
		t.Errorf("published at +%v, want at once after the second throttle (+%v)", got.Sub(t0), c.burst[1].at.Sub(t0))
	}
	for _, n := range names {
		checkState(t, c.publish[0].file, n, "estimate", refresh)
	}
	if got := ids(c.sweep); len(got) != 3 || got[0] != "C" {
		t.Fatalf("sweep = %v, want [C D E]", got)
	}
	if wait := c.sweep[0].at.Sub(c.burst[1].at); wait != BackoffMin {
		t.Errorf("sweep resumed %v after the second throttle, want %v", wait, BackoffMin)
	}
}

// TestCycleThrottleBackoffGrows: while every detail is throttled, the detail pause doubles from
// 15s up to 5 min across cycles, bursts are skipped while paused, snapshots keep coming, and
// one good detail ends the backoff (SPEC §7.3 step 4, §7.1).
func TestCycleThrottleBackoffGrows(t *testing.T) {
	clk := clock.NewFake(t0)
	inner := fake.New(clk)
	var servers []source.Server
	for _, n := range []string{"A", "B", "C", "D", "E", "F", "G", "H", "I", "J", "K", "L"} {
		servers = append(servers, srv(n, 10, "m1"))
	}
	for i := range 6 {
		ss := append([]source.Server{}, servers...)
		if i >= 2 {
			ss[0] = srv("A", 10, "m2") // A changes in the third snapshot: P0 while paused
		}
		inner.AddSnapshots(withServers(snap(string(rune('1'+i)), string(rune('a'+i)), time.Duration(i)*refresh), ss...))
	}
	src := throttle{Source: inner, clk: clk, after: time.Second, windows: [][2]time.Duration{{0, 1000 * time.Second}}}
	h := newHarness(clk, src, defaultOptions())
	h.run(t, 20*time.Minute, 6*refresh+5*time.Second)
	h.checkPolite(t)
	h.checkRates(t)

	var details []event
	for _, e := range h.events {
		if e.kind == "detail" {
			details = append(details, e)
		}
	}
	want := []time.Duration{time.Second, 15 * time.Second, 30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute, 5 * time.Minute, 5 * time.Minute}
	if len(details) < len(want)+2 {
		t.Fatalf("%d detail requests, want at least %d", len(details), len(want)+2)
	}
	for i, w := range want {
		if got := details[i+1].at.Sub(details[i].at); got != w {
			t.Errorf("gap before detail %d = %v, want %v", i+1, got, w)
		}
	}
	if details[len(want)].err != nil {
		t.Errorf("detail %d at +%v failed (%v), want success after the throttle window", len(want), details[len(want)].at.Sub(t0), details[len(want)].err)
	}
	sweepGap := time.Duration(float64(time.Second) / h.opts.SweepRate)
	if got := details[len(want)+1].at.Sub(details[len(want)].at); got != sweepGap {
		t.Errorf("gap after the first good detail = %v, want the sweep rate %v", got, sweepGap)
	}

	cs := h.cycles()
	if len(cs) != 6 {
		t.Fatalf("%d cycles, want 6 (snapshots keep coming while details are paused)", len(cs))
	}
	if c := cs[2]; len(c.burst) != 0 || !c.publish[0].at.Equal(c.snap.FetchedAt.Add(pollDelay)) {
		t.Errorf("snapshot 3: burst %v published at +%v, want no burst and an immediate publish", ids(c.burst), c.publish[0].at.Sub(t0))
	}
	checkState(t, cs[2].publish[0].file, "A", "estimate", 2*refresh)
}

// TestCycleRateCapWorldThrottled runs the generated world through throttling spells and checks
// the total request-rate cap and politeness over every request (SPEC §8 Data provider, §9).
func TestCycleRateCapWorldThrottled(t *testing.T) {
	clk := clock.NewFake(t0)
	o := defaultOptions()
	o.BurstBudget = 20 // Concurrency 1: request goroutines record the clock before it moves on
	src := throttle{Source: fake.NewWorld(clk, 7), clk: clk, after: 2 * time.Second,
		windows: [][2]time.Duration{{200 * time.Second, 203 * time.Second}, {600 * time.Second, 700 * time.Second}}}
	h := newHarness(clk, src, o)
	h.run(t, 20*time.Minute, 15*time.Minute)
	h.checkPolite(t)
	if run := h.checkRates(t); run < repeatedThrottles {
		t.Fatalf("longest throttled run %d: scenario never throttled repeatedly", run)
	}
	var burst, sweep int
	for _, c := range h.cycles() {
		burst += len(c.burst)
		sweep += len(c.sweep)
	}
	if burst == 0 || sweep == 0 {
		t.Fatalf("%d burst and %d sweep requests: scenario too quiet to check", burst, sweep)
	}
}
