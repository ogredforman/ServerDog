// Package clock provides the Clock interface and its real and fake implementations; code under
// internal/ gets time only from here (SPEC §10.3, §10.4).
package clock

import "time"

// Clock is the only source of time for code under internal/. Production uses Real; tests use
// the fake clock so multi-cycle runs need no real waiting.
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

// Real is the wall clock.
type Real struct{}

var _ Clock = Real{}

// Now returns the current wall-clock time.
func (Real) Now() time.Time { return time.Now() }

// After waits for d to elapse and then sends the current time on the returned channel.
func (Real) After(d time.Duration) <-chan time.Time { return time.After(d) }
