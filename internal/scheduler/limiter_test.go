package scheduler

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/ogredforman/ServerDog/internal/clock"
)

var t0 = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

// driveWaits runs n lim.Wait calls in a goroutine and advances the fake clock to each wanted
// offset from t0, returning the clock time at which each Wait returned.
func driveWaits(t *testing.T, clk *clock.Fake, lim *Limiter, want []time.Duration) []time.Duration {
	t.Helper()
	times := make(chan time.Time)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() {
		for range want {
			if err := lim.Wait(ctx); err != nil {
				return
			}
			times <- clk.Now()
		}
	}()
	var got []time.Duration
	for _, w := range want {
		if at := t0.Add(w); at.After(clk.Now()) {
			clk.BlockUntil(1)
			clk.Advance(at.Sub(clk.Now()))
		}
		got = append(got, (<-times).Sub(t0))
	}
	return got
}

func TestLimiterSpacing(t *testing.T) {
	tests := []struct {
		name  string
		rate  float64
		setup func(*Limiter) error
		want  []time.Duration
	}{
		{
			name: "4 per second spaces 250ms, first immediate",
			rate: 4,
			want: []time.Duration{0, 250 * time.Millisecond, 500 * time.Millisecond, 750 * time.Millisecond},
		},
		{
			name: "1.5 per second",
			rate: 1.5,
			want: []time.Duration{0, 666666666, 1333333332},
		},
		{
			name:  "SetRate changes spacing",
			rate:  1,
			setup: func(l *Limiter) error { return l.SetRate(2) },
			want:  []time.Duration{0, 500 * time.Millisecond, time.Second},
		},
		{
			name: "PauseUntil delays every slot",
			rate: 4,
			setup: func(l *Limiter) error {
				l.PauseUntil(t0.Add(90 * time.Second))
				return nil
			},
			want: []time.Duration{90 * time.Second, 90*time.Second + 250*time.Millisecond},
		},
		{
			name: "earlier pause does not shorten a later one",
			rate: 4,
			setup: func(l *Limiter) error {
				l.PauseUntil(t0.Add(10 * time.Second))
				l.PauseUntil(t0.Add(5 * time.Second))
				return nil
			},
			want: []time.Duration{10 * time.Second, 10*time.Second + 250*time.Millisecond},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clk := clock.NewFake(t0)
			lim, err := NewLimiter(clk, tt.rate)
			if err != nil {
				t.Fatal(err)
			}
			if tt.setup != nil {
				if err := tt.setup(lim); err != nil {
					t.Fatal(err)
				}
			}
			got := driveWaits(t, clk, lim, tt.want)
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("wait %d returned at +%v, want +%v (all: %v)", i, got[i], tt.want[i], got)
				}
			}
		})
	}
}

func TestLimiterIdleDoesNotBank(t *testing.T) {
	clk := clock.NewFake(t0)
	lim, err := NewLimiter(clk, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := lim.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	clk.Advance(10 * time.Second)
	// After a long idle gap, one slot is free now and the next is a full interval later.
	got := driveWaits(t, clk, lim, []time.Duration{10 * time.Second, 11 * time.Second})
	if got[1] != 11*time.Second {
		t.Fatalf("second wait after idle at +%v, want +11s", got[1])
	}
}

func TestLimiterWaitCancel(t *testing.T) {
	clk := clock.NewFake(t0)
	lim, err := NewLimiter(clk, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := lim.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() { done <- lim.Wait(ctx) }()
	clk.BlockUntil(1)
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Wait after cancel = %v, want context.Canceled", err)
	}
	if err := lim.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Wait with done ctx = %v, want context.Canceled", err)
	}
}

func TestLimiterRejectsBadRate(t *testing.T) {
	clk := clock.NewFake(t0)
	for _, r := range []float64{0, -1, math.NaN()} {
		if _, err := NewLimiter(clk, r); err == nil {
			t.Errorf("NewLimiter(%v) succeeded, want error", r)
		}
	}
	lim, err := NewLimiter(clk, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := lim.SetRate(0); err == nil {
		t.Error("SetRate(0) succeeded, want error")
	}
	// The old rate is kept.
	got := driveWaits(t, clk, lim, []time.Duration{0, time.Second})
	if got[1] != time.Second {
		t.Fatalf("second wait at +%v, want +1s", got[1])
	}
}
