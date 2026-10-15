package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ogredforman/ServerDog/internal/clock"
	"github.com/ogredforman/ServerDog/internal/publish"
	"github.com/ogredforman/ServerDog/internal/source"
	"github.com/ogredforman/ServerDog/internal/tracker"
)

// Options are the scheduler's settings (SPEC §8 Configuration) and its publish step.
type Options struct {
	BurstRate   float64       // detail requests per second during the burst
	BurstBudget int           // max detail requests per burst
	SweepRate   float64       // detail requests per second during the sweep
	Concurrency int           // parallel detail requests
	PollDelay   time.Duration // wait after FetchedAt + RefreshEvery before polling

	// Publish stores one built servers.json, e.g. with publish.Write. Required.
	Publish func(ctx context.Context, f publish.File) error
	// Logger receives per-cycle and failure logs. Nil means slog.Default().
	Logger *slog.Logger
}

// Scheduler runs the poll cycle (SPEC §7.1): snapshot, tracker update, P0/P1 burst, publish,
// then the P2/P3 sweep until the next snapshot is due. While no new snapshot arrives it keeps
// the last publish, and republishes it marked stale once the data is too old.
type Scheduler struct {
	src  source.Source
	clk  clock.Clock
	trk  *tracker.Tracker
	opts Options
	log  *slog.Logger
	lim  *Limiter
	poll *Poller
}

// New returns a Scheduler. Every upstream request it makes, snapshot or detail, goes through
// one shared Limiter.
func New(src source.Source, clk clock.Clock, trk *tracker.Tracker, o Options) (*Scheduler, error) {
	switch {
	case !(o.SweepRate > 0):
		return nil, fmt.Errorf("scheduler: sweep rate %v: must be > 0", o.SweepRate)
	case o.BurstBudget < 0:
		return nil, fmt.Errorf("scheduler: burst budget %d: must be >= 0", o.BurstBudget)
	case o.Concurrency < 1:
		return nil, fmt.Errorf("scheduler: concurrency %d: must be >= 1", o.Concurrency)
	case o.Publish == nil:
		return nil, errors.New("scheduler: Publish is required")
	}
	lim, err := NewLimiter(clk, o.BurstRate)
	if err != nil {
		return nil, fmt.Errorf("scheduler: burst rate: %w", err)
	}
	log := o.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Scheduler{
		src: src, clk: clk, trk: trk, opts: o, log: log, lim: lim,
		poll: NewPoller(src, clk, lim, o.PollDelay),
	}, nil
}

// Run polls and runs cycles until ctx is done, then waits for in-flight detail requests and
// returns ctx.Err().
func (s *Scheduler) Run(ctx context.Context) error {
	var (
		last           source.Snapshot
		have           bool // last holds a good snapshot
		stalePublished bool // last was republished as stale during this outage
	)
	for {
		// Wait for the next poll, republishing the last snapshot as stale if that comes first.
		for {
			due := s.poll.Due()
			if !have || stalePublished {
				break
			}
			// Stale holds from just after StaleAt; published times are whole seconds.
			at := publish.StaleAt(last).Add(time.Second)
			if !at.Before(due) {
				break
			}
			if err := s.sleepUntil(ctx, at); err != nil {
				return err
			}
			s.publish(ctx, last)
			stalePublished = true
		}
		if err := s.sleepUntil(ctx, s.poll.Due()); err != nil {
			return err
		}

		snap, err := s.poll.Poll(ctx)
		switch {
		case ctx.Err() != nil:
			return ctx.Err()
		case errors.Is(err, source.ErrNotModified):
			continue
		case err != nil:
			s.log.Warn("snapshot failed", "err", err, "retry_at", s.poll.Due())
			continue
		}
		last, have, stalePublished = snap, true, false
		s.cycle(ctx, snap)
	}
}

// cycle handles one new snapshot (SPEC §7.1 steps 3–6).
func (s *Scheduler) cycle(ctx context.Context, snap source.Snapshot) {
	s.trk.Observe(snap)
	tried := make(map[string]bool) // one request per server per snapshot, even a failed one

	var burst []string
	for _, d := range s.trk.Queue() {
		if d.Tier > tracker.P1 || len(burst) == s.opts.BurstBudget {
			break
		}
		burst = append(burst, d.ID)
	}
	s.lim.SetRate(s.opts.BurstRate) // both rates were checked in New
	nBurst := s.fetch(ctx, burst, time.Time{}, tried)

	s.publish(ctx, snap)

	// The sweep takes what the burst left, in queue order: P0/P1 servers beyond the budget
	// first, then P2, then P3 by oldest CheckedAt.
	var sweep []string
	for _, d := range s.trk.Queue() {
		if !tried[d.ID] {
			sweep = append(sweep, d.ID)
		}
	}
	s.lim.SetRate(s.opts.SweepRate)
	nSweep := s.fetch(ctx, sweep, s.poll.Due(), tried)

	s.log.Info("cycle done",
		"snapshot", snap.ID,
		"servers", len(snap.Servers),
		"burst", nBurst,
		"burst_queued", len(burst),
		"sweep", nSweep,
		"sweep_queued", len(sweep),
	)
}

// fetch requests details for ids in order, at most Concurrency at a time, each after a limiter
// slot before deadline (zero: no deadline). It stops early when ctx is done or the deadline
// is reached, waits for in-flight requests, and returns how many were made.
func (s *Scheduler) fetch(ctx context.Context, ids []string, deadline time.Time, tried map[string]bool) int {
	sem := make(chan struct{}, s.opts.Concurrency)
	var wg sync.WaitGroup
	n := 0
	for _, id := range ids {
		// Take a concurrency slot before a limiter slot, so a reserved limiter slot is never
		// held up behind requests in flight.
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
		}
		if ctx.Err() != nil {
			break
		}
		if ok, err := s.lim.WaitBefore(ctx, deadline); !ok || err != nil {
			<-sem
			break
		}
		tried[id] = true
		n++
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			s.detail(ctx, id)
		}()
	}
	wg.Wait()
	return n
}

// detail makes one detail request and applies the result (SPEC §7.3). A failed request skips
// the server this cycle and keeps its previous state; a RetryAfterError pauses every upstream
// request.
func (s *Scheduler) detail(ctx context.Context, id string) {
	ms, err := s.src.MatchStart(ctx, id)
	now := s.clk.Now()
	var ra source.RetryAfterError
	switch {
	case err == nil:
		s.trk.Apply(id, ms, now)
	case ctx.Err() != nil:
	case errors.As(err, &ra):
		s.lim.PauseUntil(now.Add(ra.After))
		s.log.Warn("detail throttled", "server", id, "retry_after", ra.After)
	default:
		s.log.Warn("detail failed", "server", id, "err", err)
	}
}

// publish builds servers.json from snap and the tracker at the current time and hands it to
// Options.Publish. A failure is logged; the previous file stays in place.
func (s *Scheduler) publish(ctx context.Context, snap source.Snapshot) {
	if ctx.Err() != nil {
		return
	}
	f := publish.Build(snap, s.trk, s.clk.Now(), s.opts.PollDelay)
	if err := s.opts.Publish(ctx, f); err != nil {
		s.log.Error("publish failed", "snapshot", snap.ID, "err", err)
		return
	}
	s.log.Debug("published", "snapshot", snap.ID, "stale", f.Stale)
}

// sleepUntil blocks until t on the clock, or until ctx is done. A zero or past t returns at once.
func (s *Scheduler) sleepUntil(ctx context.Context, t time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	d := t.Sub(s.clk.Now())
	if t.IsZero() || d <= 0 {
		return nil
	}
	select {
	case <-s.clk.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
