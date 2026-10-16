package wardogservers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ogredforman/ServerDog/internal/source"
)

// DefaultBaseURL is the public API used when Options.BaseURL is empty.
const DefaultBaseURL = "https://api.wardogservers.com"

const (
	// DefaultTimeout bounds every upstream request (SPEC §8 Data provider).
	DefaultTimeout = 20 * time.Second
	// DefaultRetryAfter is used when a 429/503 carries no usable Retry-After header.
	DefaultRetryAfter = 15 * time.Second
	// maxBody caps a response body; a full snapshot is about 2 MB today.
	maxBody = 64 << 20
)

// Options configures a Client. Zero values select the defaults.
type Options struct {
	BaseURL    string       // default DefaultBaseURL
	Version    string       // ServerDog version for the User-Agent; default "dev"
	Contact    string       // optional contact appended to the User-Agent
	HTTPClient *http.Client // default: a client with DefaultTimeout
}

// Client is a source.Source backed by api.wardogservers.com.
type Client struct {
	base      string
	userAgent string
	http      *http.Client
}

var _ source.Source = (*Client)(nil)

// New returns a Client for the given options.
func New(opts Options) *Client {
	base := strings.TrimRight(opts.BaseURL, "/")
	if base == "" {
		base = DefaultBaseURL
	}
	hc := opts.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: DefaultTimeout}
	}
	return &Client{base: base, userAgent: UserAgent(opts.Version, opts.Contact), http: hc}
}

// UserAgent returns "ServerDog/<version> (+https://servers.dog)", with "; <contact>" inside the
// parentheses when contact is set.
func UserAgent(version, contact string) string {
	if version == "" {
		version = "dev"
	}
	if contact == "" {
		return fmt.Sprintf("ServerDog/%s (+https://servers.dog)", version)
	}
	return fmt.Sprintf("ServerDog/%s (+https://servers.dog; %s)", version, contact)
}

// Snapshot fetches /v1/snapshot. A non-empty prevTag is sent as If-None-Match; a 304 returns
// source.ErrNotModified. The response ETag becomes Snapshot.Tag.
func (c *Client) Snapshot(ctx context.Context, prevTag string) (source.Snapshot, error) {
	resp, err := c.get(ctx, "/v1/snapshot", prevTag)
	if err != nil {
		return source.Snapshot{}, fmt.Errorf("snapshot: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		return source.Snapshot{}, source.ErrNotModified
	}
	var body snapshotJSON
	if err := decode(resp, &body); err != nil {
		return source.Snapshot{}, fmt.Errorf("snapshot: %w", err)
	}
	snap, err := body.toSource(resp.Header.Get("ETag"))
	if err != nil {
		return source.Snapshot{}, fmt.Errorf("snapshot: %w", err)
	}
	return snap, nil
}

// MatchStart fetches /v1/servers/{serverId} and decodes attributes.MatchStartUTC.
func (c *Client) MatchStart(ctx context.Context, serverID string) (source.MatchStart, error) {
	resp, err := c.get(ctx, "/v1/servers/"+url.PathEscape(serverID), "")
	if err != nil {
		return source.MatchStart{}, fmt.Errorf("server %s: %w", serverID, err)
	}
	defer resp.Body.Close()
	var body detailJSON
	if err := decode(resp, &body); err != nil {
		return source.MatchStart{}, fmt.Errorf("server %s: %w", serverID, err)
	}
	raw, ok := body.Data.Attributes["MatchStartUTC"]
	if !ok {
		return source.MatchStart{State: source.Unknown}, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return source.MatchStart{}, fmt.Errorf("server %s: MatchStartUTC is not a string: %w", serverID, err)
	}
	ms, err := decodeTicks(s)
	if err != nil {
		return source.MatchStart{}, fmt.Errorf("server %s: %w", serverID, err)
	}
	return ms, nil
}

// get sends one GET. 429/503 are turned into source.RetryAfterError; other non-2xx statuses
// except 304 into plain errors. On success the caller closes the body.
func (c *Client) get(ctx context.Context, path, ifNoneMatch string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")
	if ifNoneMatch != "" {
		req.Header.Set("If-None-Match", ifNoneMatch)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	switch {
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable:
		resp.Body.Close()
		return nil, source.RetryAfterError{After: retryAfter(resp.Header)}
	case resp.StatusCode == http.StatusNotModified && ifNoneMatch != "":
		return resp, nil
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		resp.Body.Close()
		return nil, fmt.Errorf("unexpected status %s", resp.Status)
	}
	return resp, nil
}

func decode(resp *http.Response, v any) error {
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(v); err != nil {
		return fmt.Errorf("decode body: %w", err)
	}
	return nil
}

// retryAfter reads Retry-After as delay-seconds or an HTTP date (measured against the response's
// own Date header, so no local clock is needed). Missing or unusable values give DefaultRetryAfter.
func retryAfter(h http.Header) time.Duration {
	v := strings.TrimSpace(h.Get("Retry-After"))
	if v == "" {
		return DefaultRetryAfter
	}
	if n, err := strconv.Atoi(v); err == nil {
		if n < 1 {
			return time.Second
		}
		return time.Duration(n) * time.Second
	}
	at, err := http.ParseTime(v)
	if err != nil {
		return DefaultRetryAfter
	}
	now, err := http.ParseTime(h.Get("Date"))
	if err != nil {
		return DefaultRetryAfter
	}
	if d := at.Sub(now); d >= time.Second {
		return d
	}
	return time.Second
}

const (
	ticksPrefix = "int64:"
	// unixEpochTicks is 1970-01-01T00:00:00Z in .NET ticks (100 ns since 0001-01-01).
	unixEpochTicks = 621_355_968_000_000_000
	ticksPerSecond = 10_000_000
)

// decodeTicks decodes an "int64:<.NET ticks>" attribute. "-1" means not started.
func decodeTicks(s string) (source.MatchStart, error) {
	v, ok := strings.CutPrefix(s, ticksPrefix)
	if !ok {
		return source.MatchStart{}, fmt.Errorf("MatchStartUTC %q: missing %q prefix", s, ticksPrefix)
	}
	ticks, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return source.MatchStart{}, fmt.Errorf("MatchStartUTC %q: %w", s, err)
	}
	if ticks == -1 {
		return source.MatchStart{State: source.NotStarted}, nil
	}
	if ticks <= unixEpochTicks {
		return source.MatchStart{}, fmt.Errorf("MatchStartUTC %q: before the Unix epoch", s)
	}
	t := ticks - unixEpochTicks
	at := time.Unix(t/ticksPerSecond, (t%ticksPerSecond)*100).UTC()
	return source.MatchStart{State: source.Exact, At: at}, nil
}

type metaJSON struct {
	FetchedAt      time.Time `json:"fetchedAt"`
	SnapshotID     string    `json:"snapshotId"`
	RefreshSeconds int       `json:"refreshSeconds"`
	Stale          bool      `json:"stale"`
	Complete       bool      `json:"complete"`
}

type snapshotJSON struct {
	Data []serverJSON `json:"data"`
	Meta metaJSON     `json:"meta"`
}

type serverJSON struct {
	ID                string    `json:"id"`       // instance UUID; changes on restart
	ServerID          string    `json:"serverId"` // join code
	ObservedAt        time.Time `json:"observedAt"`
	Name              string    `json:"name"`
	Type              string    `json:"type"`
	Region            string    `json:"region"`
	Players           int       `json:"players"`
	MaxPlayers        int       `json:"maxPlayers"`
	PasswordProtected bool      `json:"passwordProtected"`
	Map               struct {
		Variant string `json:"variant"`
		Base    string `json:"base"`
	} `json:"map"`
	Mode struct {
		Experience string   `json:"experience"`
		GameMode   string   `json:"gameMode"`
		Modifiers  []string `json:"modifiers"`
	} `json:"mode"`
	Rulesets []string `json:"rulesets"`
}

type detailJSON struct {
	Data struct {
		Attributes map[string]json.RawMessage `json:"attributes"`
	} `json:"data"`
}

func (b snapshotJSON) toSource(etag string) (source.Snapshot, error) {
	m := b.Meta
	switch {
	case m.SnapshotID == "":
		return source.Snapshot{}, fmt.Errorf("meta.snapshotId missing")
	case m.FetchedAt.IsZero():
		return source.Snapshot{}, fmt.Errorf("meta.fetchedAt missing")
	case m.RefreshSeconds <= 0:
		return source.Snapshot{}, fmt.Errorf("meta.refreshSeconds %d is not positive", m.RefreshSeconds)
	}
	snap := source.Snapshot{
		ID:           m.SnapshotID,
		Tag:          etag,
		FetchedAt:    m.FetchedAt,
		RefreshEvery: time.Duration(m.RefreshSeconds) * time.Second,
		Stale:        m.Stale,
		Complete:     m.Complete,
		Servers:      make([]source.Server, 0, len(b.Data)),
	}
	unknownType := 0
	for _, s := range b.Data {
		var typ source.ServerType
		switch s.Type {
		case "official":
			typ = source.Official
		case "community":
			typ = source.Community
		default:
			unknownType++
			continue
		}
		snap.Servers = append(snap.Servers, source.Server{
			ID:                s.ServerID,
			InstanceID:        s.ID,
			Name:              s.Name,
			Region:            s.Region,
			Type:              typ,
			Players:           s.Players,
			MaxPlayers:        s.MaxPlayers,
			PasswordProtected: s.PasswordProtected,
			Map:               source.Map{Variant: s.Map.Variant, Base: s.Map.Base, Name: mapName(s.Map.Variant)},
			Mode:              source.Mode{Experience: s.Mode.Experience, GameMode: s.Mode.GameMode, Modifiers: s.Mode.Modifiers, Name: modeName(s.Mode.Experience)},
			Rulesets:          s.Rulesets,
			ObservedAt:        s.ObservedAt,
		})
	}
	if unknownType > 0 {
		slog.Warn("skipped servers with unknown type", "snapshot", m.SnapshotID, "count", unknownType)
	}
	return snap, nil
}

// mapNames maps the provider's map variant codenames to in-game map names (checked in-game
// against live servers, 2026-10-16).
var mapNames = map[string]string{
	"Bakurani": "Bakurani",
	"Madrid":   "Ozeti",
	"Detroit":  "Zestafona",
}

// mapName is the in-game name for a map variant, or the variant itself when it isn't known.
func mapName(variant string) string {
	if n, ok := mapNames[variant]; ok {
		return n
	}
	return variant
}

// modeName is the in-game mode for an experience: "Bakurani_KOTH_02+KOTH_InfantryOnly" is
// <map>_<mode>_<n> plus modifiers, so KOTH. An experience without that shape is returned as is,
// minus modifiers.
func modeName(experience string) string {
	base, _, _ := strings.Cut(experience, "+")
	if parts := strings.Split(base, "_"); len(parts) >= 3 {
		return parts[1]
	}
	return base
}
