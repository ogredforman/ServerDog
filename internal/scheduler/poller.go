package scheduler

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ogredforman/ServerDog/internal/clock"
	"github.com/ogredforman/ServerDog/internal/source"
)

// Poll timing (SPEC §7.1 steps 1–2).
const (
	NotModifiedRetry = 15 * time.Second // retry interval while the provider has no new snapshot
	BackoffMin       = 15 * time.Second // first retry after an error
	BackoffMax       = 5 * time.Minute  // error backoff cap
)

// Poller decides when to ask the Source for the next snapshot and makes that request. It is
// a single step, not a loop, so the scheduler can act between attempts (stale publish, sweep).
// It is not safe for concurrent use.
type Poller struct {
	src       source.Source
	clk       clock.Clock
	lim       *Limiter
	pollDelay time.Duration

	due     time.Time // zero: poll now (first start)
	tag     string    // last snapshot's Tag, passed back as prevTag
	lastID  string    // last snapshot's ID
	backoff time.Duration
}

// NewPoller returns a Poller whose first poll is due immediately. Every Snapshot call goes
// through lim, and a RetryAfterError pauses lim for every other upstream request too.
func NewPoller(src source.Source, clk clock.Clock, lim *Limiter, pollDelay time.Duration) *Poller {
	return &Poller{src: src, clk: clk, lim: lim, pollDelay: pollDelay, backoff: BackoffMin}
}

// Due returns when the next Snapshot call may be made. The zero time means now.
func (p *Poller) Due() time.Time { return p.due }

// Wait blocks until Due, or until ctx is done.
func (p *Poller) Wait(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	d := p.due.Sub(p.clk.Now())
	if p.due.IsZero() || d <= 0 {
		return nil
	}
	select {
	case <-p.clk.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Poll makes one Snapshot request and sets Due from the outcome:
//   - new snapshot: FetchedAt + RefreshEvery + PollDelay; the error backoff resets;
//   - source.ErrNotModified, or the same snapshot ID again: now + 15s; returns ErrNotModified;
//   - source.RetryAfterError: now + max(backoff, After), and the limiter pauses until now + After;
//   - any other error: now + backoff, which doubles from 15s up to 5 min.
//
// A failure caused by ctx being done leaves Due and the backoff unchanged. Poll does not
// check Due; call Wait first.
func (p *Poller) Poll(ctx context.Context) (source.Snapshot, error) {
	if err := p.lim.Wait(ctx); err != nil {
		return source.Snapshot{}, fmt.Errorf("poll snapshot: %w", err)
	}
	snap, err := p.src.Snapshot(ctx, p.tag)
	if err != nil && ctx.Err() != nil {
		// Our own cancellation (e.g. shutdown) is not an upstream failure: leave Due and backoff.
		return source.Snapshot{}, fmt.Errorf("poll snapshot: %w", err)
	}
	now := p.clk.Now()
	if err == nil && p.lastID != "" && snap.ID == p.lastID {
		err = source.ErrNotModified
	}

	var ra source.RetryAfterError
	switch {
	case err == nil:
		p.tag, p.lastID = snap.Tag, snap.ID
		p.backoff = BackoffMin
		p.due = snap.FetchedAt.Add(snap.RefreshEvery + p.pollDelay)
		return snap, nil
	case errors.Is(err, source.ErrNotModified):
		p.backoff = BackoffMin
		p.due = now.Add(NotModifiedRetry)
		return source.Snapshot{}, source.ErrNotModified
	case errors.As(err, &ra):
		p.lim.PauseUntil(now.Add(ra.After))
		p.due = now.Add(max(p.backoff, ra.After))
		p.growBackoff()
	default:
		p.due = now.Add(p.backoff)
		p.growBackoff()
	}
	return source.Snapshot{}, fmt.Errorf("poll snapshot: %w", err)
}

func (p *Poller) growBackoff() {
	p.backoff = min(2*p.backoff, BackoffMax)
}
