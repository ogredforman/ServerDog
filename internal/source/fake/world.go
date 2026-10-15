package fake

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/ogredforman/ServerDog/internal/clock"
	"github.com/ogredforman/ServerDog/internal/source"
)

// WorldServers is how many servers a World generates.
const WorldServers = 300

// WorldRefresh is the World's snapshot interval (Snapshot.RefreshEvery).
const WorldRefresh = 60 * time.Second

// worldEpoch anchors every World timeline, so a World is a pure function of seed and clock time.
var worldEpoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

var (
	worldRegions = []string{"na-east", "na-west", "sa", "eu-west", "eu-central", "asia", "oceania"}
	worldMaps    = []source.Map{
		{Variant: "Harbor", Base: "Harbor"}, {Variant: "Harbor Night", Base: "Harbor"},
		{Variant: "Quarry", Base: "Quarry"}, {Variant: "Quarry Storm", Base: "Quarry"},
		{Variant: "Outpost", Base: "Outpost"}, {Variant: "Rail Yard", Base: "Rail Yard"},
		{Variant: "Rail Yard Dusk", Base: "Rail Yard"}, {Variant: "Dam", Base: "Dam"},
	}
	worldModes = []source.Mode{
		{Experience: "Conquest", GameMode: "conquest"},
		{Experience: "Breakthrough", GameMode: "breakthrough"},
		{Experience: "Team Deathmatch", GameMode: "tdm"},
	}
	worldRulesets = []string{"hardcore", "no-vehicles", "custom-tickets"}
	worldNames    = []string{"Dog Pound", "Kennel Club", "Night Watch", "Iron Pack", "Frontline", "Old Guard", "Red Collar", "Muddy Boots"}
	worldMaxes    = []int{32, 64, 100}
)

// worldRestartEvery is how many matches a server plays before its process restarts (new InstanceID).
const worldRestartEvery = 8

const joinCodeChars = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

type worldKind int

const (
	kindNormal worldKind = iota
	kindEmpty            // always 0 players
	kindFull             // always Players == MaxPlayers
	kindZero             // 0/0: MaxPlayers 0
)

// worldServer is one roster entry; everything time-dependent is derived from it on demand.
type worldServer struct {
	idx          int
	id, instBase string
	name, region string
	typ          source.ServerType
	maxPlayers   int
	password     bool
	mode         source.Mode
	rulesets     []string
	kind         worldKind
	fill         float64       // share of MaxPlayers once a match has filled up (normal servers)
	matchLen     time.Duration // 12–35 min
	offset       time.Duration // phase in [0, matchLen)
	mapBase      int
	noStart      bool // detail never carries a start (provider's missing MatchStartUTC)
}

// World is a generated, slowly changing set of servers implementing source.Source for
// source=fake (SPEC §10.4). It is deterministic: the same seed and clock time always give the
// same snapshot and match starts. Snapshots fall every WorldRefresh; each server rotates maps
// on its own schedule, with players dropping at rotation and refilling over a few minutes.
// MatchStart reports not-started for a short warmup after each rotation, then the exact start.
// It is immutable after NewWorld and safe for concurrent use.
type World struct {
	clock  clock.Clock
	seed   uint64
	roster []worldServer
	byID   map[string]int
}

var _ source.Source = (*World)(nil)

// NewWorld generates a World of WorldServers servers from seed, reading time from c.
func NewWorld(c clock.Clock, seed int64) *World {
	w := &World{clock: c, seed: uint64(seed), byID: map[string]int{}}
	rng := rand.New(rand.NewPCG(uint64(seed), 0x5e7d06))
	used := map[string]bool{}
	for i := range WorldServers {
		e := worldServer{
			idx:        i,
			instBase:   fmt.Sprintf("%08x", rng.Uint32()),
			region:     worldRegions[rng.IntN(len(worldRegions))],
			maxPlayers: worldMaxes[rng.IntN(len(worldMaxes))],
			fill:       0.3 + 0.7*rng.Float64(),
			matchLen:   time.Duration(12+rng.IntN(24)) * time.Minute,
			mapBase:    rng.IntN(len(worldMaps)),
			noStart:    rng.IntN(100) < 3,
		}
		e.offset = time.Duration(rng.Int64N(int64(e.matchLen)))
		if rng.IntN(3) == 0 {
			e.typ = source.Official
			e.mode = worldModes[i%len(worldModes)]
			e.name = fmt.Sprintf("Wardogs Official #%d", i+1)
			e.rulesets = []string{"official"}
		} else {
			e.typ = source.Community
			e.mode = worldModes[rng.IntN(len(worldModes))]
			e.name = fmt.Sprintf("%s %d", worldNames[rng.IntN(len(worldNames))], i+1)
			e.password = rng.IntN(100) < 15
			if rng.IntN(2) == 0 {
				e.rulesets = []string{worldRulesets[rng.IntN(len(worldRulesets))]}
			}
		}
		switch r := rng.IntN(100); {
		case r < 12:
			e.kind = kindEmpty
		case r < 18:
			e.kind = kindFull
		case r < 21:
			e.kind = kindZero
		}
		if rng.IntN(100) >= 2 {
			e.id = joinCode(rng, used)
		}
		w.roster = append(w.roster, e)
	}
	// Guarantee every edge case is present whatever the seed.
	w.roster[1].kind, w.roster[2].kind, w.roster[3].kind = kindEmpty, kindFull, kindZero
	w.roster[4].id = ""
	for i := range w.roster {
		e := &w.roster[i]
		if e.kind == kindZero {
			e.maxPlayers = 0
		}
		if e.id != "" {
			w.byID[e.id] = i
		}
	}
	return w
}

func joinCode(rng *rand.Rand, used map[string]bool) string {
	for {
		b := make([]byte, 6)
		for i := range b {
			b[i] = joinCodeChars[rng.IntN(len(joinCodeChars))]
		}
		if c := string(b); !used[c] {
			used[c] = true
			return c
		}
	}
}

// Snapshot implements source.Source. It returns the latest snapshot at or before Now, or
// ErrNotModified when prevTag is that snapshot's Tag.
func (w *World) Snapshot(ctx context.Context, prevTag string) (source.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return source.Snapshot{}, err
	}
	k := floorDiv(w.clock.Now().Sub(worldEpoch), WorldRefresh)
	id := fmt.Sprintf("fake-%d", k)
	if prevTag != "" && prevTag == id {
		return source.Snapshot{}, source.ErrNotModified
	}
	at := worldEpoch.Add(time.Duration(k) * WorldRefresh)
	snap := source.Snapshot{
		ID: id, Tag: id, FetchedAt: at, RefreshEvery: WorldRefresh, Complete: true,
		Servers: make([]source.Server, 0, len(w.roster)),
	}
	for i := range w.roster {
		snap.Servers = append(snap.Servers, w.server(&w.roster[i], at))
	}
	return snap, nil
}

// MatchStart implements source.Source, evaluated at Now. Empty and 0/0 servers, servers whose
// detail carries no start, and unknown IDs report Unknown.
func (w *World) MatchStart(ctx context.Context, serverID string) (source.MatchStart, error) {
	if err := ctx.Err(); err != nil {
		return source.MatchStart{}, err
	}
	i, ok := w.byID[serverID]
	if !ok {
		return source.MatchStart{}, nil
	}
	e := &w.roster[i]
	if e.kind == kindEmpty || e.kind == kindZero || e.noStart {
		return source.MatchStart{}, nil
	}
	now := w.clock.Now()
	m, rotAt := e.match(now)
	start := rotAt.Add(w.warmup(e, m))
	if now.Before(start) {
		return source.MatchStart{State: source.NotStarted}, nil
	}
	return source.MatchStart{State: source.Exact, At: start}, nil
}

// server renders roster entry e as seen at time at.
func (w *World) server(e *worldServer, at time.Time) source.Server {
	m, rotAt := e.match(at)
	s := source.Server{
		ID:                e.id,
		InstanceID:        fmt.Sprintf("%s-%d", e.instBase, floorDivInt(m, worldRestartEvery)),
		Name:              e.name,
		Region:            e.region,
		Type:              e.typ,
		MaxPlayers:        e.maxPlayers,
		PasswordProtected: e.password,
		Map:               worldMaps[posMod(int64(e.mapBase)+m, len(worldMaps))],
		Mode:              e.mode,
		Rulesets:          e.rulesets,
		ObservedAt:        at,
	}
	switch e.kind {
	case kindFull:
		s.Players = e.maxPlayers
	case kindNormal:
		// Refill from 30% to 100% of the target over the first 4 minutes, ±1 jitter.
		ramp := min(float64(at.Sub(rotAt))/float64(4*time.Minute), 1)
		target := float64(e.maxPlayers) * e.fill
		jitter := int(mix(w.seed, uint64(e.idx), uint64(at.Unix()), 1)%3) - 1
		s.Players = max(1, min(e.maxPlayers, int(target*(0.3+0.7*ramp)+0.5)+jitter))
	}
	return s
}

// match returns e's match number at t and when that match's rotation happened.
func (e *worldServer) match(t time.Time) (int64, time.Time) {
	m := floorDiv(t.Sub(worldEpoch)+e.offset, e.matchLen)
	return m, worldEpoch.Add(-e.offset + time.Duration(m)*e.matchLen)
}

// warmup is how long after rotation match m of e actually starts: 30–150s.
func (w *World) warmup(e *worldServer, m int64) time.Duration {
	return time.Duration(30+mix(w.seed, uint64(e.idx), uint64(m), 2)%121) * time.Second
}

func floorDiv(a, b time.Duration) int64 {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}
	return int64(q)
}

func floorDivInt(a, b int64) int64 {
	return floorDiv(time.Duration(a), time.Duration(b))
}

func posMod(a int64, n int) int {
	r := a % int64(n)
	if r < 0 {
		r += int64(n)
	}
	return int(r)
}

// mix hashes vals with splitmix64 steps.
func mix(vals ...uint64) uint64 {
	h := uint64(0x9e3779b97f4a7c15)
	for _, v := range vals {
		h ^= v
		h += 0x9e3779b97f4a7c15
		h = (h ^ (h >> 30)) * 0xbf58476d1ce4e5b9
		h = (h ^ (h >> 27)) * 0x94d049bb133111eb
		h ^= h >> 31
	}
	return h
}
