package source

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestServerTypeString(t *testing.T) {
	tests := []struct {
		in   ServerType
		want string
	}{
		{Official, "official"},
		{Community, "community"},
		{ServerType(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.in.String(); got != tt.want {
			t.Errorf("ServerType(%d).String() = %q, want %q", int(tt.in), got, tt.want)
		}
	}
}

func TestMatchStartStateString(t *testing.T) {
	tests := []struct {
		in   MatchStartState
		want string
	}{
		{Unknown, "unknown"},
		{NotStarted, "not-started"},
		{Exact, "exact"},
		{MatchStartState(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.in.String(); got != tt.want {
			t.Errorf("MatchStartState(%d).String() = %q, want %q", int(tt.in), got, tt.want)
		}
	}
}

func TestMatchStartZeroValueIsUnknown(t *testing.T) {
	var ms MatchStart
	if ms.State != Unknown {
		t.Errorf("zero MatchStart.State = %v, want unknown", ms.State)
	}
}

func TestRetryAfterError(t *testing.T) {
	err := fmt.Errorf("fetch snapshot: %w", RetryAfterError{After: 30 * time.Second})

	var ra RetryAfterError
	if !errors.As(err, &ra) {
		t.Fatalf("errors.As did not find RetryAfterError in %v", err)
	}
	if ra.After != 30*time.Second {
		t.Errorf("After = %v, want 30s", ra.After)
	}
	if want := "fetch snapshot: provider throttled: retry after 30s"; err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}
}

func TestErrNotModifiedWrapped(t *testing.T) {
	err := fmt.Errorf("fetch snapshot: %w", ErrNotModified)
	if !errors.Is(err, ErrNotModified) {
		t.Errorf("errors.Is(%v, ErrNotModified) = false", err)
	}
}
