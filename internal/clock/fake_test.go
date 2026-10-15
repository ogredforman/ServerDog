package clock

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

func TestFakeNowAndSet(t *testing.T) {
	f := NewFake(t0)
	if got := f.Now(); !got.Equal(t0) {
		t.Fatalf("Now() = %v, want %v", got, t0)
	}
	later := t0.Add(90 * time.Second)
	f.Set(later)
	if got := f.Now(); !got.Equal(later) {
		t.Fatalf("after Set, Now() = %v, want %v", got, later)
	}
}

func TestFakeSetBackwardsPanics(t *testing.T) {
	f := NewFake(t0)
	defer func() {
		if recover() == nil {
			t.Error("Set to an earlier time did not panic")
		}
	}()
	f.Set(t0.Add(-time.Second))
}

func TestFakeAfterZeroFiresImmediately(t *testing.T) {
	for _, d := range []time.Duration{0, -time.Second} {
		f := NewFake(t0)
		select {
		case got := <-f.After(d):
			if !got.Equal(t0) {
				t.Errorf("After(%v) sent %v, want %v", d, got, t0)
			}
		default:
			t.Errorf("After(%v) did not fire immediately", d)
		}
		if f.Pending() != 0 {
			t.Errorf("After(%v) left %d pending timers", d, f.Pending())
		}
	}
}

func TestFakeAfterNotBeforeDeadline(t *testing.T) {
	f := NewFake(t0)
	ch := f.After(10 * time.Second)
	f.Advance(10*time.Second - time.Nanosecond)
	select {
	case <-ch:
		t.Fatal("fired before its deadline")
	default:
	}
	f.Advance(time.Nanosecond)
	select {
	case got := <-ch:
		if want := t0.Add(10 * time.Second); !got.Equal(want) {
			t.Errorf("sent %v, want %v", got, want)
		}
	default:
		t.Fatal("did not fire at its deadline")
	}
}

func TestFakeAdvanceFiresDueTimers(t *testing.T) {
	tests := []struct {
		name    string
		advance time.Duration
		fired   []int // indexes into durations that must have fired
		pending int
	}{
		{"none due", 9 * time.Second, nil, 5},
		{"ties and earlier", 10 * time.Second, []int{1, 3}, 3},
		{"most", 45 * time.Second, []int{0, 1, 2, 3}, 1},
		{"all", time.Hour, []int{0, 1, 2, 3, 4}, 0},
	}
	// Created out of deadline order, with a shared deadline.
	durations := []time.Duration{30 * time.Second, 10 * time.Second, 20 * time.Second, 10 * time.Second, time.Minute}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := NewFake(t0)
			chans := make([]<-chan time.Time, len(durations))
			for i, d := range durations {
				chans[i] = f.After(d)
			}
			f.Advance(tt.advance)
			want := map[int]bool{}
			for _, i := range tt.fired {
				want[i] = true
			}
			for i, ch := range chans {
				select {
				case at := <-ch:
					if !want[i] {
						t.Errorf("timer %d fired early", i)
					} else if w := t0.Add(durations[i]); !at.Equal(w) {
						t.Errorf("timer %d sent %v, want its deadline %v", i, at, w)
					}
				default:
					if want[i] {
						t.Errorf("timer %d did not fire", i)
					}
				}
			}
			if f.Pending() != tt.pending {
				t.Errorf("Pending() = %d, want %d", f.Pending(), tt.pending)
			}
			if got, w := f.Now(), t0.Add(tt.advance); !got.Equal(w) {
				t.Errorf("Now() = %v, want %v", got, w)
			}
		})
	}
}

func TestFakeAdvanceSequencesWaiters(t *testing.T) {
	// A goroutine re-arming After on each fire sees Now at each deadline in turn, which only
	// holds if Advance steps through deadlines in order.
	f := NewFake(t0)
	seen := make(chan time.Time, 10)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 3; i++ {
			at := <-f.After(time.Second)
			seen <- at
		}
	}()
	for i := 0; i < 3; i++ {
		f.BlockUntil(1)
		f.Advance(time.Second)
	}
	<-done
	close(seen)
	i := 1
	for at := range seen {
		if w := t0.Add(time.Duration(i) * time.Second); !at.Equal(w) {
			t.Errorf("fire %d at %v, want %v", i, at, w)
		}
		i++
	}
}

func TestFakeBlockUntil(t *testing.T) {
	f := NewFake(t0)
	ready := make(chan struct{})
	go func() {
		f.BlockUntil(2)
		close(ready)
	}()
	f.After(time.Second)
	f.After(2 * time.Second)
	<-ready
	if f.Pending() != 2 {
		t.Errorf("Pending() = %d, want 2", f.Pending())
	}
}
