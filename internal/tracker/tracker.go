package tracker

import (
	"sort"
	"sync"
	"time"

	"github.com/ogredforman/ServerDog/internal/source"
)

// EvictAfter is how long a server may be missing from snapshots before the tracker forgets it (SPEC §7.2).
const EvictAfter = time.Hour

// Tier is a server's detail-fetch priority (SPEC §7.2). None means "don't queue".
type Tier int

const (
	None Tier = iota
	P0        // change observed or sharp player drop: check this cycle
	P1        // waiting for a start inside StartingWindow
	P2        // new to the tracker
	P3        // background sweep, oldest CheckedAt first
)

// String returns "none", "P0", "P1", "P2" or "P3".
func (t Tier) String() string {
	switch t {
	case P0:
		return "P0"
	case P1:
		return "P1"
	case P2:
		return "P2"
	case P3:
		return "P3"
	}
	return "none"
}

// Options are the tracker's tuning settings (SPEC §8 Configuration).
type Options struct {
	StartingWindow time.Duration // how long after a change a server stays P1
	DropPct        int           // sharp player drop: percent of previous players
	DropMin        int           // sharp player drop: absolute players
}

// Server is the tracker's state for one join code (SPEC §6.1 TrackedServer).
type Server struct {
	ID         string
	InstanceID string
	MapVariant string
	Experience string
	Players    int
	// ChangeObservedAt is when a new match was last inferred. It is cleared once a start
	// confirms that match, so a set value means the published start is an estimate.
	ChangeObservedAt  time.Time
	Start             source.MatchStart
	CheckedAt         time.Time // last detail fetch
	CheckedSnapshotID string
	Tier              Tier
	LastSeen          time.Time // FetchedAt of the last snapshot listing the server
}

// Due is one server waiting for a detail fetch.
type Due struct {
	ID   string
	Tier Tier
}

// State is a server's published start state (SPEC §8 Published data).
type State int

const (
	Unknown State = iota
	Exact
	Estimate
	Starting
)

// String returns "unknown", "exact", "estimate" or "starting".
func (s State) String() string {
	switch s {
	case Exact:
		return "exact"
	case Estimate:
		return "estimate"
	case Starting:
		return "starting"
	}
	return "unknown"
}

// Published is what the publisher shows for one server's start.
type Published struct {
	State State
	At    time.Time // zero when State == Unknown
}

// Tracker keeps per-server state across snapshots and decides what to fetch next
// (SPEC §7.2–7.3). It holds no clock: callers pass the times it needs. It is safe for
// concurrent use.
type Tracker struct {
	mu           sync.Mutex
	opts         Options
	servers      map[string]*Server
	snapshotID   string
	refreshEvery time.Duration
}

// New returns an empty Tracker.
func New(o Options) *Tracker {
	return &Tracker{opts: o, servers: make(map[string]*Server)}
}

// Observe records a new snapshot, classifies every listed server into a tier, and evicts
// servers missing for longer than EvictAfter (SPEC §7.2).
func (t *Tracker) Observe(snap source.Snapshot) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.snapshotID = snap.ID
	t.refreshEvery = snap.RefreshEvery
	now := snap.FetchedAt

	seen := make(map[string]bool, len(snap.Servers))
	for i := range snap.Servers {
		s := &snap.Servers[i]
		if s.ID == "" || seen[s.ID] {
			continue
		}
		seen[s.ID] = true
		ts, known := t.servers[s.ID]
		if !known {
			ts = &Server{ID: s.ID}
			t.servers[s.ID] = ts
		}
		t.observe(ts, known, s, now)
	}

	for id, ts := range t.servers {
		if seen[id] {
			continue
		}
		ts.Tier = None
		if now.Sub(ts.LastSeen) > EvictAfter {
			delete(t.servers, id)
		}
	}
}

func (t *Tracker) observe(ts *Server, known bool, s *source.Server, now time.Time) {
	changed := known && (ts.InstanceID != s.InstanceID || ts.MapVariant != s.Map.Variant || ts.Experience != s.Mode.Experience)
	dropped := known && t.sharpDrop(ts.Players, s.Players)

	ts.InstanceID = s.InstanceID
	ts.MapVariant = s.Map.Variant
	ts.Experience = s.Mode.Experience
	ts.Players = s.Players
	ts.LastSeen = now

	switch {
	case s.Players == 0:
		ts.Start = source.MatchStart{}
		ts.ChangeObservedAt = time.Time{}
		ts.Tier = None
	case s.MatchStart != nil:
		ts.Start = *s.MatchStart
		ts.ChangeObservedAt = time.Time{}
		ts.Tier = None
	case !known:
		// A known server refilling from empty lands in P3 with an old CheckedAt, so it is swept early.
		ts.Tier = P2
	case changed:
		ts.ChangeObservedAt = now
		ts.Tier = P0
	case dropped:
		ts.Tier = P0
	case t.waiting(ts, now):
		ts.Tier = P1
	default:
		ts.Tier = P3
	}
}

// sharpDrop reports whether players fell by at least DropPct percent and DropMin players.
func (t *Tracker) sharpDrop(prev, cur int) bool {
	d := prev - cur
	return d > 0 && d >= t.opts.DropMin && d*100 >= t.opts.DropPct*prev
}

// waiting reports whether a server is still waiting for its start to be confirmed inside
// StartingWindow (SPEC §7.2 rule 5). Because confirmation clears ChangeObservedAt, a set
// value means the known start is not-started, unknown, or older than the change.
func (t *Tracker) waiting(ts *Server, now time.Time) bool {
	return !ts.ChangeObservedAt.IsZero() && now.Sub(ts.ChangeObservedAt) <= t.opts.StartingWindow
}

// Queue returns the servers due for a detail fetch: P0, P1 and P2 ordered by ID, then P3
// by oldest CheckedAt. Servers already checked in the current snapshot are left out.
func (t *Tracker) Queue() []Due {
	t.mu.Lock()
	defer t.mu.Unlock()

	var due []*Server
	for _, ts := range t.servers {
		if ts.Tier != None && ts.CheckedSnapshotID != t.snapshotID {
			due = append(due, ts)
		}
	}
	sort.Slice(due, func(i, j int) bool {
		a, b := due[i], due[j]
		if a.Tier != b.Tier {
			return a.Tier < b.Tier
		}
		if a.Tier == P3 && !a.CheckedAt.Equal(b.CheckedAt) {
			return a.CheckedAt.Before(b.CheckedAt)
		}
		return a.ID < b.ID
	})
	out := make([]Due, len(due))
	for i, ts := range due {
		out[i] = Due{ID: ts.ID, Tier: ts.Tier}
	}
	return out
}

// Apply records a successful detail result fetched at checkedAt (SPEC §7.3). An exact start
// confirms the observed change if it is no older than one RefreshEvery before it, or if
// StartingWindow has passed. An unknown result keeps the previous start. Results for
// untracked or unqueued servers are ignored.
func (t *Tracker) Apply(id string, ms source.MatchStart, checkedAt time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()

	ts, ok := t.servers[id]
	if !ok || ts.Tier == None {
		return
	}
	ts.CheckedAt = checkedAt
	ts.CheckedSnapshotID = t.snapshotID
	if ms.State != source.Unknown {
		ts.Start = ms
	}

	if ms.State == source.Exact && !ts.ChangeObservedAt.IsZero() {
		rounded := !ms.At.Before(ts.ChangeObservedAt.Add(-t.refreshEvery))
		if rounded || !t.waiting(ts, checkedAt) {
			ts.ChangeObservedAt = time.Time{}
		}
	}
	if t.waiting(ts, checkedAt) {
		ts.Tier = P1
	} else {
		ts.Tier = P3
	}
}

// Published returns the start to publish for a server at now (SPEC §8): unknown when empty,
// starting while not-started inside StartingWindow, estimate while a change is unconfirmed,
// otherwise exact or unknown from the last detail result.
func (t *Tracker) Published(id string, now time.Time) (Published, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	ts, ok := t.servers[id]
	if !ok {
		return Published{}, false
	}
	switch {
	case ts.Players == 0:
		return Published{}, true
	case ts.Start.State == source.NotStarted && t.waiting(ts, now):
		return Published{State: Starting, At: ts.ChangeObservedAt}, true
	case !ts.ChangeObservedAt.IsZero():
		return Published{State: Estimate, At: ts.ChangeObservedAt}, true
	case ts.Start.State == source.Exact:
		return Published{State: Exact, At: ts.Start.At}, true
	}
	return Published{}, true
}

// Get returns a copy of the tracker's state for one server.
func (t *Tracker) Get(id string) (Server, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	ts, ok := t.servers[id]
	if !ok {
		return Server{}, false
	}
	return *ts, true
}
