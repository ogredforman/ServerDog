package clock

import (
	"testing"
	"time"
)

func TestRealNow(t *testing.T) {
	if (Real{}).Now().IsZero() {
		t.Error("Real.Now() returned the zero time")
	}
}

func TestRealAfterFires(t *testing.T) {
	select {
	case <-(Real{}).After(0):
	case <-time.After(5 * time.Second):
		t.Fatal("Real.After(0) did not fire within 5s")
	}
}
