// Package source declares the provider-neutral Source interface and its types (SPEC §6.1, §10.3).
//
// Nothing outside internal/source/<provider> may depend on a specific provider's URLs, JSON shapes,
// tick encodings or ETags. Everything else uses only this package.
package source

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrNotModified is returned by Source.Snapshot when prevTag is non-empty and the provider's
// listing has not changed since. The scheduler retries after 15s (SPEC §7.1).
var ErrNotModified = errors.New("snapshot not modified")

// RetryAfterError signals that the provider is throttling us. The caller must not send another
// upstream request before After has elapsed and backs off (SPEC §5 Polite upstream client).
// Match it with errors.As.
type RetryAfterError struct {
	After time.Duration
}

func (e RetryAfterError) Error() string {
	return fmt.Sprintf("provider throttled: retry after %s", e.After)
}

// Source is a server-list provider.
//
// Callers must keep the upstream politeness rules (SPEC §5, §7.2):
//   - every call goes through the rate limiter;
//   - at most one MatchStart call per server per snapshot ID;
//   - never call MatchStart for a server with 0 players;
//   - honor RetryAfterError.
//
// Implementations send the ServerDog User-Agent on every request.
type Source interface {
	// Snapshot returns the provider's full current listing. If prevTag is non-empty
	// and nothing changed, it returns ErrNotModified.
	Snapshot(ctx context.Context, prevTag string) (Snapshot, error)

	// MatchStart looks up one server's current match start by join code.
	MatchStart(ctx context.Context, serverID string) (MatchStart, error)
}

// Snapshot is one complete provider listing (SPEC §6.1).
type Snapshot struct {
	ID           string
	Tag          string // opaque; passed back to Source.Snapshot as prevTag
	FetchedAt    time.Time
	RefreshEvery time.Duration
	Stale        bool // the provider itself reports its data as stale (SPEC §7.1)
	Complete     bool
	Servers      []Server
}

// Server is one game server as reported in a snapshot (SPEC §6.1).
type Server struct {
	// ID is the join code. It may be empty; servers without a join code are not tracked
	// and never get a detail fetch (SPEC §6.2).
	ID string
	// InstanceID changes when the server process restarts; a change implies a new match.
	InstanceID        string
	Name              string
	Region            string
	Type              ServerType
	Players           int
	MaxPlayers        int
	PasswordProtected bool
	Map               Map
	Mode              Mode
	Rulesets          []string
	ObservedAt        time.Time
	// MatchStart is nil unless the provider's bulk list carries it. When snapshots carry it,
	// detail fetching is switched off (SPEC §7.2).
	MatchStart *MatchStart
}

// Map is the map a server is currently running.
type Map struct {
	Variant string
	Base    string
}

// Mode is a server's experience, game mode and modifiers.
type Mode struct {
	Experience string
	GameMode   string
	Modifiers  []string
}

// ServerType says whether a server is run by the game or by the community.
type ServerType int

const (
	Official ServerType = iota
	Community
)

// String returns "official" or "community"; any other value maps to "unknown".
func (t ServerType) String() string {
	switch t {
	case Official:
		return "official"
	case Community:
		return "community"
	default:
		return "unknown"
	}
}

// MatchStartState is what we know about a server's current match start.
// The zero value is Unknown, so a missing value never claims a start.
type MatchStartState int

const (
	Unknown MatchStartState = iota
	NotStarted
	Exact
)

// String returns "unknown", "not-started" or "exact"; any other value maps to "unknown".
func (s MatchStartState) String() string {
	switch s {
	case NotStarted:
		return "not-started"
	case Exact:
		return "exact"
	default:
		return "unknown"
	}
}

// MatchStart is when a server's current match started.
type MatchStart struct {
	State MatchStartState
	At    time.Time // set only when State == Exact
}
