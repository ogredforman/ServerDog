package clock

import (
	"sort"
	"sync"
	"time"
)

// Fake is a test clock. Time moves only when the test calls Set or Advance, which fire due After
// channels in deadline order (SPEC §10.4). It is safe for concurrent use.
type Fake struct {
	mu     sync.Mutex
	cond   *sync.Cond
	now    time.Time
	timers []fakeTimer
	seq    int
}

type fakeTimer struct {
	at  time.Time
	seq int // creation order; breaks ties between equal deadlines
	ch  chan time.Time
}

var _ Clock = (*Fake)(nil)

// NewFake returns a fake clock reading start.
func NewFake(start time.Time) *Fake {
	f := &Fake{now: start}
	f.cond = sync.NewCond(&f.mu)
	return f
}

// Now returns the fake current time.
func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// After returns a channel that receives the deadline once the clock reaches now+d.
// If d <= 0 the channel fires immediately with the current time.
func (f *Fake) After(d time.Duration) <-chan time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	ch := make(chan time.Time, 1)
	if d <= 0 {
		ch <- f.now
		return ch
	}
	f.seq++
	f.timers = append(f.timers, fakeTimer{at: f.now.Add(d), seq: f.seq, ch: ch})
	f.cond.Broadcast()
	return ch
}

// Set moves the clock to t, firing due timers as Advance does. It panics if t is before Now.
func (f *Fake) Set(t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if t.Before(f.now) {
		panic("clock.Fake: Set would move time backwards")
	}
	f.advanceTo(t)
}

// Advance moves the clock forward by d. Timers due by the new time fire one at a time in
// (deadline, creation) order; before each fires, Now reads that timer's deadline.
// It panics if d is negative.
func (f *Fake) Advance(d time.Duration) {
	if d < 0 {
		panic("clock.Fake: Advance by a negative duration")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.advanceTo(f.now.Add(d))
}

// advanceTo fires due timers and moves the clock to target. f.mu must be held.
func (f *Fake) advanceTo(target time.Time) {
	sort.Slice(f.timers, func(i, j int) bool {
		a, b := f.timers[i], f.timers[j]
		if !a.at.Equal(b.at) {
			return a.at.Before(b.at)
		}
		return a.seq < b.seq
	})
	n := 0
	for _, t := range f.timers {
		if t.at.After(target) {
			break
		}
		f.now = t.at
		t.ch <- t.at
		n++
	}
	f.timers = f.timers[n:]
	f.now = target
}

// Pending returns the number of After channels that have not fired yet.
func (f *Fake) Pending() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.timers)
}

// BlockUntil waits until at least n After channels are pending. Tests use it to know a goroutine
// is parked on After before calling Advance.
func (f *Fake) BlockUntil(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for len(f.timers) < n {
		f.cond.Wait()
	}
}

// Next returns the earliest pending After deadline, or false if none is pending. Tests use it
// with BlockUntil to step the clock from one wake-up to the next.
func (f *Fake) Next() (time.Time, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var next time.Time
	for i, t := range f.timers {
		if i == 0 || t.at.Before(next) {
			next = t.at
		}
	}
	return next, len(f.timers) > 0
}
