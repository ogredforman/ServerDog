// Package publish builds the servers.json model and writes it atomically (SPEC §7.4, §8).
package publish

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"time"

	"github.com/ogredforman/ServerDog/internal/atomicfile"
	"github.com/ogredforman/ServerDog/internal/source"
	"github.com/ogredforman/ServerDog/internal/tracker"
)

// Version is the published schema version (`v`). Bump it, and SPEC §8 "Published data",
// whenever the shape changes.
const Version = 1

// FileName is the published file's name inside the data directory.
const FileName = "servers.json"

// retryAfter is when the next publish is expected if the regular one is already overdue;
// it matches the poller's not-modified retry (SPEC §7.1).
const retryAfter = 15 * time.Second

// File is the published servers.json (SPEC §8 Published data). Times are Unix seconds.
type File struct {
	V              int      `json:"v"`
	GeneratedAt    int64    `json:"generatedAt"`
	NextUpdateAt   int64    `json:"nextUpdateAt"`
	SnapshotAt     int64    `json:"snapshotAt"`
	RefreshSeconds int64    `json:"refreshSeconds"`
	Stale          bool     `json:"stale"`
	Complete       bool     `json:"complete"`
	Regions        []string `json:"regions"`
	Servers        []Server `json:"servers"`
}

// Server is one published server, with short keys to keep the file small.
type Server struct {
	ID         string   `json:"id"`
	Type       string   `json:"t"` // "o" official, "c" community
	Region     string   `json:"r"`
	Name       string   `json:"n"`
	Players    int      `json:"p"`
	MaxPlayers int      `json:"mp"`
	Password   bool     `json:"pw"`
	Map        string   `json:"map"`
	Base       string   `json:"base"`
	Experience string   `json:"exp"`
	Rulesets   []string `json:"rs"`
	MatchStart *int64   `json:"ms"` // null when st is unknown; for starting, when the change was observed
	State      string   `json:"st"`
}

// Starts gives the start to publish for a join code; *tracker.Tracker implements it.
type Starts interface {
	Published(id string, now time.Time) (tracker.Published, bool)
}

// Build makes the published file from a snapshot and the tracker's starts at now.
// pollDelay is the scheduler's PollDelay, used for nextUpdateAt.
func Build(snap source.Snapshot, starts Starts, now time.Time, pollDelay time.Duration) File {
	f := File{
		V:              Version,
		GeneratedAt:    now.Unix(),
		NextUpdateAt:   NextUpdateAt(snap, now, pollDelay).Unix(),
		SnapshotAt:     snap.FetchedAt.Unix(),
		RefreshSeconds: int64(snap.RefreshEvery / time.Second),
		Stale:          Stale(snap, now),
		Complete:       snap.Complete,
		Regions:        []string{},
		Servers:        make([]Server, 0, len(snap.Servers)),
	}
	regions := map[string]bool{}
	for _, s := range snap.Servers {
		if s.Region != "" && !regions[s.Region] {
			regions[s.Region] = true
			f.Regions = append(f.Regions, s.Region)
		}
		f.Servers = append(f.Servers, server(s, starts, now))
	}
	sort.Strings(f.Regions)
	return f
}

func server(s source.Server, starts Starts, now time.Time) Server {
	out := Server{
		ID:         s.ID,
		Type:       "o",
		Region:     s.Region,
		Name:       s.Name,
		Players:    s.Players,
		MaxPlayers: s.MaxPlayers,
		Password:   s.PasswordProtected,
		Map:        s.Map.Variant,
		Base:       s.Map.Base,
		Experience: s.Mode.Experience,
		Rulesets:   append([]string{}, s.Rulesets...),
		State:      tracker.Unknown.String(),
	}
	if s.Type == source.Community {
		out.Type = "c"
	}
	if s.ID == "" || s.Players == 0 {
		return out
	}
	p, ok := starts.Published(s.ID, now)
	if !ok {
		return out
	}
	out.State = p.State.String()
	if !p.At.IsZero() {
		ms := p.At.Unix()
		out.MatchStart = &ms
	}
	return out
}

// NextUpdateAt is when the next publish is expected: the next snapshot poll at
// FetchedAt + RefreshEvery + pollDelay, or 15s from now if that has already passed.
func NextUpdateAt(snap source.Snapshot, now time.Time, pollDelay time.Duration) time.Time {
	next := snap.FetchedAt.Add(snap.RefreshEvery + pollDelay)
	if !next.After(now) {
		return now.Add(retryAfter)
	}
	return next
}

// Stale reports whether the data must be flagged stale: the provider says so, or the
// snapshot is older than 2 × RefreshEvery + 60s (SPEC §7.1, §7.5).
func Stale(snap source.Snapshot, now time.Time) bool {
	return snap.Stale || now.After(StaleAt(snap))
}

// StaleAt returns the age limit for snap: from just after this time, with no newer snapshot,
// the data is stale (2 × RefreshEvery + 60s after FetchedAt).
func StaleAt(snap source.Snapshot) time.Time {
	return snap.FetchedAt.Add(2*snap.RefreshEvery + time.Minute)
}

// Write writes f to dir/servers.json atomically: a temp file in dir, then a rename.
func Write(ctx context.Context, dir string, f File) error {
	// The published file is world-readable.
	err := atomicfile.Write(ctx, filepath.Join(dir, FileName), 0o644, func(w io.Writer) error {
		return json.NewEncoder(w).Encode(f)
	})
	if err != nil {
		return fmt.Errorf("publish: %w", err)
	}
	return nil
}
