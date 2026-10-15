package scheduler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ogredforman/ServerDog/internal/clock"
)

// Limiter spaces upstream requests at most perSecond apart, using only the injected clock.
// One Limiter is shared by every upstream request (snapshot and detail), so it is both the
// total request-rate cap and the place a Retry-After pause applies (SPEC §5 Polite upstream
// client, §7.1). It is safe for concurrent use.
type Limiter struct {
	clk clock.Clock

	mu       sync.Mutex
	interval time.Duration
	next     time.Time // earliest time the next slot may be handed out
}

// NewLimiter returns a Limiter allowing perSecond requests per second. perSecond must be > 0.
func NewLimiter(clk clock.Clock, perSecond float64) (*Limiter, error) {
	interval, err := rateInterval(perSecond)
	if err != nil {
		return nil, err
	}
	return &Limiter{clk: clk, interval: interval}, nil
}

// SetRate changes the spacing after slots handed out from now on (burst vs sweep, SPEC §7.1);
// the gap after the most recent slot keeps the old rate.
// A non-positive rate is rejected and the old rate kept.
func (l *Limiter) SetRate(perSecond float64) error {
	interval, err := rateInterval(perSecond)
	if err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.interval = interval
	return nil
}

// PauseUntil hands out no slot before t. An earlier t than an existing pause has no effect.
// Callers use it to honor source.RetryAfterError.
func (l *Limiter) PauseUntil(t time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if t.After(l.next) {
		l.next = t
	}
}

// Wait blocks until the caller may make one upstream request, or ctx is done. A slot reserved
// before ctx is done stays used, which errs on the side of fewer requests.
func (l *Limiter) Wait(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	l.mu.Lock()
	now := l.clk.Now()
	at := l.next
	if at.Before(now) {
		at = now
	}
	l.next = at.Add(l.interval)
	l.mu.Unlock()

	if !at.After(now) {
		return nil
	}
	select {
	case <-l.clk.After(at.Sub(now)):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func rateInterval(perSecond float64) (time.Duration, error) {
	if !(perSecond > 0) {
		return 0, fmt.Errorf("limiter rate %v: must be > 0", perSecond)
	}
	return time.Duration(float64(time.Second) / perSecond), nil
}
