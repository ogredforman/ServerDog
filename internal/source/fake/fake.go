package fake

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/ogredforman/ServerDog/internal/clock"
	"github.com/ogredforman/ServerDog/internal/source"
)

// Method names recorded in Call.Method.
const (
	MethodSnapshot   = "Snapshot"
	MethodMatchStart = "MatchStart"
)

// ErrNoSnapshot is returned by Snapshot when no scripted snapshot is visible yet.
var ErrNoSnapshot = errors.New("fake source: no snapshot available")

// Call is one recorded request, made at fake-clock time At. Tests use the record to check the
// politeness rules: one detail per server per snapshot, no empty-server fetches, rate caps.
type Call struct {
	Method   string // MethodSnapshot or MethodMatchStart
	ServerID string // MatchStart only
	PrevTag  string // Snapshot only
	At       time.Time
}

// Source is a scriptable in-memory source.Source (SPEC §10.4). It is safe for concurrent use.
//
// Snapshot returns the latest scripted snapshot whose FetchedAt is not after the clock's Now,
// or ErrNotModified when prevTag equals that snapshot's Tag. MatchStart returns the scripted
// value for the server, or an Unknown start if none was scripted. Injected errors take
// priority and are consumed one per call.
type Source struct {
	clock clock.Clock

	mu            sync.Mutex
	snapshots     []source.Snapshot
	starts        map[string]source.MatchStart
	snapErrs      []error
	matchStartErr map[string][]error
	calls         []Call
}

var _ source.Source = (*Source)(nil)

// New returns an empty fake Source that reads time from c.
func New(c clock.Clock) *Source {
	return &Source{
		clock:         c,
		starts:        map[string]source.MatchStart{},
		matchStartErr: map[string][]error{},
	}
}

// AddSnapshots scripts snapshots. Each becomes visible once the clock reaches its FetchedAt.
func (s *Source) AddSnapshots(snaps ...source.Snapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshots = append(s.snapshots, snaps...)
}

// SetMatchStart scripts what MatchStart returns for serverID from now on.
func (s *Source) SetMatchStart(serverID string, ms source.MatchStart) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.starts[serverID] = ms
}

// FailNextSnapshot queues errors; each of the next len(errs) Snapshot calls returns one, in order.
func (s *Source) FailNextSnapshot(errs ...error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapErrs = append(s.snapErrs, errs...)
}

// FailNextMatchStart queues errors for serverID; each of the next len(errs) MatchStart calls for
// that server returns one, in order.
func (s *Source) FailNextMatchStart(serverID string, errs ...error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.matchStartErr[serverID] = append(s.matchStartErr[serverID], errs...)
}

// Calls returns a copy of every recorded call, in order.
func (s *Source) Calls() []Call {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Call(nil), s.calls...)
}

// Snapshot implements source.Source. A call with an already-done ctx returns ctx.Err() and is
// not recorded, since no upstream request would have been made.
func (s *Source) Snapshot(ctx context.Context, prevTag string) (source.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return source.Snapshot{}, err
	}
	now := s.clock.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, Call{Method: MethodSnapshot, PrevTag: prevTag, At: now})
	if len(s.snapErrs) > 0 {
		err := s.snapErrs[0]
		s.snapErrs = s.snapErrs[1:]
		return source.Snapshot{}, err
	}
	var cur *source.Snapshot
	for i := range s.snapshots {
		snap := &s.snapshots[i]
		if snap.FetchedAt.After(now) {
			continue
		}
		if cur == nil || !snap.FetchedAt.Before(cur.FetchedAt) {
			cur = snap
		}
	}
	if cur == nil {
		return source.Snapshot{}, ErrNoSnapshot
	}
	if prevTag != "" && prevTag == cur.Tag {
		return source.Snapshot{}, source.ErrNotModified
	}
	out := *cur
	out.Servers = append([]source.Server(nil), cur.Servers...)
	return out, nil
}

// MatchStart implements source.Source. A call with an already-done ctx returns ctx.Err() and is
// not recorded.
func (s *Source) MatchStart(ctx context.Context, serverID string) (source.MatchStart, error) {
	if err := ctx.Err(); err != nil {
		return source.MatchStart{}, err
	}
	now := s.clock.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, Call{Method: MethodMatchStart, ServerID: serverID, At: now})
	if errs := s.matchStartErr[serverID]; len(errs) > 0 {
		s.matchStartErr[serverID] = errs[1:]
		return source.MatchStart{}, errs[0]
	}
	return s.starts[serverID], nil
}
