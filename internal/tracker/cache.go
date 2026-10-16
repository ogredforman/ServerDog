package tracker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/ogredforman/ServerDog/internal/atomicfile"
	"github.com/ogredforman/ServerDog/internal/source"
)

// cacheVersion is the cache file's schema version. A file with another version is rejected,
// which means a cold start.
const cacheVersion = 1

type cacheFile struct {
	V       int           `json:"v"`
	Servers []cacheServer `json:"servers"`
}

type cacheServer struct {
	ID                string     `json:"id"`
	InstanceID        string     `json:"instanceId"`
	MapVariant        string     `json:"mapVariant"`
	Experience        string     `json:"experience"`
	Players           int        `json:"players"`
	ChangeObservedAt  time.Time  `json:"changeObservedAt"`
	Start             cacheStart `json:"start"`
	CheckedAt         time.Time  `json:"checkedAt"`
	CheckedSnapshotID string     `json:"checkedSnapshotId"`
	LastSeen          time.Time  `json:"lastSeen"`
}

type cacheStart struct {
	State string    `json:"state"` // source.MatchStartState.String()
	At    time.Time `json:"at"`
}

var cacheStates = map[string]source.MatchStartState{
	source.Unknown.String():    source.Unknown,
	source.NotStarted.String(): source.NotStarted,
	source.Exact.String():      source.Exact,
}

// Save writes the tracker's per-server state as JSON (SPEC §7.1 step 7). Tiers are not saved:
// the next Observe recomputes them.
func (t *Tracker) Save(w io.Writer) error {
	t.mu.Lock()
	f := cacheFile{V: cacheVersion, Servers: make([]cacheServer, 0, len(t.servers))}
	for _, ts := range t.servers {
		f.Servers = append(f.Servers, cacheServer{
			ID:                ts.ID,
			InstanceID:        ts.InstanceID,
			MapVariant:        ts.MapVariant,
			Experience:        ts.Experience,
			Players:           ts.Players,
			ChangeObservedAt:  ts.ChangeObservedAt,
			Start:             cacheStart{State: ts.Start.State.String(), At: ts.Start.At},
			CheckedAt:         ts.CheckedAt,
			CheckedSnapshotID: ts.CheckedSnapshotID,
			LastSeen:          ts.LastSeen,
		})
	}
	t.mu.Unlock()
	if err := json.NewEncoder(w).Encode(f); err != nil {
		return fmt.Errorf("tracker: encode cache: %w", err)
	}
	return nil
}

// Load replaces the tracker's per-server state with a cache written by Save. On any error
// (malformed JSON, another version, an invalid entry) the tracker is left unchanged.
func (t *Tracker) Load(r io.Reader) error {
	var f cacheFile
	if err := json.NewDecoder(r).Decode(&f); err != nil {
		return fmt.Errorf("tracker: decode cache: %w", err)
	}
	if f.V != cacheVersion {
		return fmt.Errorf("tracker: cache version %d: want %d", f.V, cacheVersion)
	}
	servers := make(map[string]*Server, len(f.Servers))
	for i, cs := range f.Servers {
		state, ok := cacheStates[cs.Start.State]
		switch {
		case cs.ID == "":
			return fmt.Errorf("tracker: cache entry %d: empty id", i)
		case servers[cs.ID] != nil:
			return fmt.Errorf("tracker: cache entry %d: duplicate id %q", i, cs.ID)
		case !ok:
			return fmt.Errorf("tracker: cache entry %q: unknown start state %q", cs.ID, cs.Start.State)
		}
		start := source.MatchStart{State: state}
		if state == source.Exact {
			start.At = cs.Start.At
		}
		servers[cs.ID] = &Server{
			ID:                cs.ID,
			InstanceID:        cs.InstanceID,
			MapVariant:        cs.MapVariant,
			Experience:        cs.Experience,
			Players:           cs.Players,
			ChangeObservedAt:  cs.ChangeObservedAt,
			Start:             start,
			CheckedAt:         cs.CheckedAt,
			CheckedSnapshotID: cs.CheckedSnapshotID,
			LastSeen:          cs.LastSeen,
		}
	}
	t.mu.Lock()
	t.servers = servers
	t.mu.Unlock()
	return nil
}

// SaveFile saves the tracker to path atomically: a temp file in the same directory, then a
// rename (SPEC §7.4).
func (t *Tracker) SaveFile(ctx context.Context, path string) error {
	if err := atomicfile.Write(ctx, path, 0o600, t.Save); err != nil {
		return fmt.Errorf("tracker: save cache: %w", err)
	}
	return nil
}

// LoadFile loads the tracker from a file written by SaveFile. A missing file returns an error
// matching fs.ErrNotExist; either way an error leaves the tracker unchanged (a cold start).
func (t *Tracker) LoadFile(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("tracker: load cache: %w", err)
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("tracker: open cache: %w", err)
	}
	defer f.Close()
	return t.Load(f)
}

// Len returns how many servers the tracker holds.
func (t *Tracker) Len() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.servers)
}
