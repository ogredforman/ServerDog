# ServerDog — System Specification

> **Audience:** the project owner and AI coding agents. Source of truth for *what* the system is
> and *why*. For *how and in what order* to build it, see [`PLAN.md`](./PLAN.md).
>
> **Status:** Pre-prototype

## 1. Summary

ServerDog (servers.dog) is an unofficial web server browser for the game Wardogs. It mirrors the in-game server browser as closely as possible and adds one thing the game lacks: a live, ticking match timer on every server, plus a yellow **Find Fresh Starts** button that sorts the list so matches about to start, then the most recently started, come first.

A single Go binary polls a server-list data provider (today: api.wardogservers.com), works out each server's match start time while keeping upstream traffic small, and publishes one `servers.json` file per upstream cycle. It also serves a static HTML/CSS/vanilla-JS page that reads that file. Cloudflare sits in front and caches almost everything, so hosting cost stays flat however many people visit.

## 2. Problem statement

- Wardogs players want to join matches near their start, but the in-game browser doesn't show how long a match has been running.
- The data exists. The community API lists every server, and its per-server detail includes `MatchStartUTC`. But the match start is **not** in the bulk list, so a naive client would need thousands of detail requests per cycle.
- No existing site presents this in the familiar in-game layout.
- The site must stay cheap on one small droplet even if it gets popular, and must not overload the hobby-run upstream API.
- The provider may change (a new community API, an official one, or this one going away). The rest of the system must not depend on any one provider's API shape.

## 3. Goals

1. A player picks a server type (official / community) and region(s) and sees every matching server with name, players, map/mode and a match timer that ticks every second.
2. **Find Fresh Starts** sorts the current list: STARTING first, then newest exact start, then estimates, then unknown. Nothing is hidden.
3. The page looks as close to the in-game Wardogs server browser as possible (layout, colors, fonts), modeled on screenshots in `design/screenshots/` and the review notes in `design/screenshots/NOTES.md`. The intended differences are the match timer and the STARTING state, plus dropping in-game controls the site can't support (§11).
4. Column sorting and the **Show Full** / **Show Empty** toggles behave like the game's.
5. Data refreshes in step with the provider's snapshot cycle (180s today), end to end. The origin's request rate doesn't depend on visitor count.
6. Navbar and footer both say clearly that the site is not affiliated with or endorsed by Bulkhead or Wardogs.

## 4. Non-goals (for now)

- Joining a server from the web. A copy-join-code button is offered instead.
- Accounts, server-side user data, or cross-device favorites. Preferences live in localStorage.
- Historical charts, stats pages, or per-server detail pages.
- A frontend framework, bundler, or npm dependencies for the shipped site.
- Filtering in Fresh Starts mode (it only sorts).
- Hosting anywhere other than one droplet behind Cloudflare.

The design must not *preclude*:
- swapping or adding data providers (see the `Source` interface, §10.3);
- a provider that includes match start in its bulk list (the detail-fetch machinery must be able to switch off);
- later per-region data files if one `servers.json` grows too large.

## 5. Product principles

- **Static to the visitor.** The browser only ever loads our own static files (HTML, CSS, JS, images, `servers.json`). It never calls the upstream provider or any per-visitor dynamic endpoint.
- **Flat origin load.** Every response a visitor gets can be cached by Cloudflare with explicit `Cache-Control`. Origin requests per minute must not grow with visitor count.
- **Polite upstream client.** At most one list request per provider cycle (plus cheap ETag/304 retries while waiting for the next snapshot). At most one detail fetch per server per snapshot. Never fetch details for empty servers. Keep total request rate under a configurable cap. Back off on 429/503/`Retry-After`. Always send a User-Agent naming ServerDog and `https://servers.dog`.
- **Provider-agnostic core.** Everything specific to one provider (URLs, JSON shapes, `int64:` tick encoding, ETags) lives in one adapter package behind the `Source` interface. Scheduling, tracking, publishing and the frontend use only ServerDog's own types.
- **Fresh Starts sorts, never hides.** Pressing it reorders the current view. Even if nothing fresh exists, the user can pick a server without changing any setting.
- **Honest data.** Never present stale data as current. If the provider reports stale data, or our last successful snapshot is too old, show a visible notice. Show estimated starts with a different look from exact ones.
- **Copy the game, don't redesign it.** Apart from the match timer and the STARTING state, the UI follows the in-game browser as shown in the screenshots. When in doubt, match the screenshot.
- **Unmistakably unofficial.** The non-affiliation disclaimer is visible in both the navbar and the footer on every page.
- **Defaults suit local development.** Running the binary with no flags, env vars or config file serves the site over plain HTTP on `:8080` using the real provider.

## 6. Domain model

### 6.1 Entities

| Entity | Purpose | Key fields |
|---|---|---|
| **Server** | One game server as reported in a snapshot (provider-neutral) | `ID` (join code; may be empty), `InstanceID`, `Type` (official/community), `Region`, `Name`, `Players`, `MaxPlayers`, `PasswordProtected`, `Map{Variant, Base}`, `Mode{Experience, GameMode, Modifiers}`, `Rulesets`, `ObservedAt`, `MatchStart` (optional; set only if the provider's bulk list carries it) |
| **Snapshot** | One complete provider listing | `ID`, `FetchedAt`, `RefreshEvery`, `Stale`, `Complete`, `Servers[]`, `Tag` (opaque, for conditional fetches) |
| **MatchStart** | What we know about when a server's current match started | `State` (exact / not-started / unknown), `At` (time, when exact) |
| **TrackedServer** | Tracker state per join code, kept across snapshots and saved to disk | `ID`, last `InstanceID`, last `Map.Variant`, last `Mode.Experience`, last `Players`, `ChangeObservedAt` (when a new match was last inferred; cleared once a start confirms it), `Start` (MatchStart), `CheckedAt` (last detail fetch), `CheckedSnapshotID`, `Tier`, `LastSeen` (for eviction) |
| **Region** | A region identifier seen in snapshots | `ID` (e.g. `na-east`), display name (mapping from screenshots, falls back to ID) |
| **Published file** (`servers.json`) | What the browser reads | see §8 "Published data" |

### 6.2 Relationships

```
Snapshot 1─* Server
Server   *─1 Region
Server   1─1 TrackedServer   (by join code; servers without a join code are not tracked)
TrackedServer 1─1 MatchStart
Published file = latest Snapshot ⨝ TrackedServer state, rendered per Server
```

## 7. Core flows

### 7.1 Poll cycle (backend)

1. Wait until `lastSnapshot.FetchedAt + RefreshEvery + PollDelay` (default 10s). On first start, poll immediately.
2. Call `Source.Snapshot(ctx, lastTag)`.
   - **Not modified:** retry every 15s.
   - **Error / 5xx / timeout:** retry with exponential backoff (15s → 30s → 60s, capped at 5 min) and honor `Retry-After`. Keep serving the last published file. Once our last good snapshot is older than `2 × RefreshEvery + 60s`, publish again with `stale: true`.
3. On a new snapshot, update the tracker (§7.2) and get this cycle's check queue.
4. **Burst:** fetch details for P0 then P1 servers at `BurstRate` (default 4/s, `Concurrency` default 3), in priority order, until done or `BurstBudget` (default 200) is used.
5. **Publish** `servers.json` (§7.4).
6. **Sweep:** for the rest of the cycle, fetch details for P2 then P3 at `SweepRate` (default 1.5/s), and stop as soon as the next snapshot is due. Results show up in the next publish.
7. Save tracker state to the cache file (atomic write).

### 7.2 Tracking match starts per snapshot

For each server in the new snapshot that has a join code:
1. **Empty (`Players == 0`):** record state, never queue. Start becomes unknown and `ChangeObservedAt` is cleared.
2. **New to the tracker:** queue as **P2**.
3. **Instance, map variant or experience changed since the last snapshot:** set `ChangeObservedAt = snapshot.FetchedAt`, mark the start as estimated, queue as **P0**.
4. **Sharp player drop** (≥ `DropPct` default 30% *and* ≥ `DropMin` default 25 players vs. the previous snapshot): queue as **P0**, but don't set `ChangeObservedAt`.
5. **Waiting for a start:** `ChangeObservedAt` is within `StartingWindow` (default 20 min), and the change is not yet confirmed (§7.3; the known start is not-started, unknown, or earlier than `ChangeObservedAt − RefreshEvery`). Queue as **P1**. Once the window has passed, stop rechecking as P1.
6. **Otherwise:** eligible for **P3** sweep, ordered by oldest `CheckedAt`.
7. Never queue a server whose `CheckedSnapshotID` equals the current snapshot ID.
8. If the provider's `Server.MatchStart` is set, use it directly (it confirms any change) and don't queue the server at all.

Servers that left the snapshot are dropped from the tracker after 1 hour.

### 7.3 Applying a detail result

1. `Source.MatchStart(ctx, id)` returns exact(time), not-started, or unknown.
2. Exact: store it. If `At` is later than `ChangeObservedAt`, the match is confirmed. If it's within one `RefreshEvery` before it, the match is also accepted (snapshot rounding). If it's older than that and we're still inside `StartingWindow`, keep it queued as P1; once the window has passed, it is accepted. Confirming clears `ChangeObservedAt`.
3. Not-started: keep the server queued as P1 while inside `StartingWindow`.
   Unknown: keep the previous start (still counts as this snapshot's check).
4. Errors on one server: skip it this cycle and keep its previous state. Repeated 429/503 pause all detail fetching (with backoff) and cut the burst short. Unchecked P0 servers then publish as estimates.
   "Repeated" means 2 throttled detail responses in a row; any successful detail resets the count. A single one only honors `Retry-After`. From the second on, detail fetching (not snapshot polling) pauses for max(`Retry-After`, backoff), with the backoff going 15s → 30s → … capped at 5 min. While paused, bursts are skipped, and the sweep resumes only if the pause ends before the next snapshot is due.

### 7.4 Publish

1. Build the published file from the snapshot and tracker state (§8 "Published data").
2. Write it to a temp file in the data directory, then rename it to `servers.json` (atomic).
3. The HTTP handler serves it with `Cache-Control: public, max-age=N, s-maxage=N`. N = seconds until `nextUpdateAt`, kept between 5 and `RefreshEvery`.

### 7.5 Browser

1. Load the page. Restore the chosen type, regions, sort and toggles from localStorage. If no type has been chosen, show the type picker first, as in-game.
2. Fetch `/data/servers.json`. Compute clock offset from the response `Date` header; apply it only when the difference is more than 2s.
3. Render the filtered, sorted list. One shared 1s timer updates every visible timer cell, without rebuilding the list.
4. Schedule the next fetch for `nextUpdateAt` plus a random 0–10s. If the response's `generatedAt` hasn't changed, retry after 15s, 30s, then 60s (capped). If the fetch fails, keep the current data and retry on the same backoff.
5. Show a stale notice if `stale` is true or `now − generatedAt > 2 × refreshSeconds + 60s`.
6. **Find Fresh Starts** switches the sort to the Fresh Starts order. Any column-header click returns to normal sorting.

## 8. Functional requirements

**Data provider**
- [P] A `Source` interface (§10.3) with a fake implementation, selected by config (`source=fake`).
- [M] A wardogservers.com adapter, selected by `source=wardogservers` (the default once it exists). It reads `/v1/snapshot` (ETag/If-None-Match) and `/v1/servers/{serverId}`. It decodes `attributes.MatchStartUTC` (`int64:<.NET ticks>`; `-1` = not started; missing = unknown) and maps upstream JSON to ServerDog types.
- [M] Requests send `User-Agent: ServerDog/<version> (+https://servers.dog)`. Request timeout default 20s.
- [M] Back off on 429/503 and `Retry-After`, with total detail request rate capped by config.

**Scheduling and tracking**
- [P] Poll cycle synced to `FetchedAt + RefreshEvery` (§7.1), with 304-retry and error backoff.
- [P] Tier classification P0/P1/P2/P3 and the skip rules exactly as in §7.2. Burst then publish then sweep (§7.1).
- [P] At most one detail fetch per server per snapshot ID.
- [P] STARTING state: `Players > 0`, start not-started, `ChangeObservedAt` within `StartingWindow`.
- [P] Estimate state: `ChangeObservedAt` is set and no confirming detail result since. The published start is `ChangeObservedAt`.
- [M] Tracker state saved to and loaded from a JSON cache file. A corrupt or missing file means a cold start (logged), not a crash.
- [M] Detail fetching switches off automatically when the provider's snapshot reports `MatchStart`.

**Published data** (`/data/servers.json`)
- [P] Top level: `v` (schema version, 1), `generatedAt`, `nextUpdateAt`, `snapshotAt` (provider `FetchedAt`), `refreshSeconds`, `stale`, `complete`, `regions` (sorted IDs present), `servers`.
- [P] Per server (short keys): `id` (join code or empty), `t` (`o`/`c`), `r` (region), `n` (display name), `p`, `mp` (players, max), `pw` (password), `map` (variant), `base` (map base), `exp` (experience), `rs` (rulesets), `ms` (start, Unix seconds, or null), `st` (`exact` / `estimate` / `starting` / `unknown`).
- [P] Times are Unix seconds (integers). Servers with 0 players are included with `st: "unknown"`. `ms` is the confirmed start for `exact`, `ChangeObservedAt` for `estimate` and `starting`, and null for `unknown`. `nextUpdateAt` is `snapshotAt + refreshSeconds + PollDelay`, or 15s after `generatedAt` if that has passed.
- [M] Gzip-compressed size under 100 KB at ~4,000 servers.

**HTTP serving**
- [P] Serve `/`, static assets, and `/data/servers.json` from the binary (assets built in via `go:embed`).
- [M] Caching headers: `servers.json` as in §7.4. `index.html` `public, max-age=60, s-maxage=300`. Versioned assets (`?v=<content hash>`, filled into `index.html` at startup) `public, max-age=31536000, immutable`.
- [M] HTTPS on a configurable address with configured cert/key files (Cloudflare Origin Certificate). Plain HTTP when no cert is set.
- [M] `/healthz` returns 200 with last snapshot age. It is never cached.

**Browser UI**
- [P] Server type picker (official / community) and multi-region selection, both remembered in localStorage.
- [P] Server list showing name, players/max, map/mode, rulesets, password flag and match timer, with sorting by those columns.
- [P] Show Full / Show Empty toggles (full = `p >= mp`, including 0/0; empty = `p == 0`), remembered in localStorage.
- [P] Match timer ticks every second, formatted `m:ss` below 1 hour and `h:mm:ss` above. STARTING shows a "STARTING" label, estimates show `~m:ss` in a muted style, unknown shows `—`.
- [P] Find Fresh Starts button. Order: starting (newest `ms` first) → exact (newest first) → estimate (newest first) → unknown (players descending). Ties broken by players descending, then name.
- [P] Refresh scheduling and stale notice as in §7.5.
- [M] Visual match to the in-game browser from `design/screenshots/`: layout, colors, fonts, region display names, hover/selected states. No map images (none are available, and the game's artwork isn't ours to copy). The Find Fresh Starts button uses the game's yellow accent.
- [M] Disclaimer in navbar and footer: "Unofficial fan site. Not affiliated with or endorsed by Bulkhead or Wardogs."
- [M] Copy-join-code control per row, with brief "Copied" feedback.
- [L] Search box (name / map) if the in-game browser has one.
- [L] Per-region data files if `servers.json` exceeds its size budget.

**Configuration**
- [P] Settings come from, highest priority first: command-line flags, then `SERVERDOG_*` environment variables, then an optional JSON config file (`--config path`), then built-in defaults.
- [P] Settings: `source` (`wardogservers` / `fake`), `upstream_url`, `listen` (default `:8080`), `tls_cert`, `tls_key`, `data_dir` (default `./data`), `cache_file` (default `<data_dir>/tracker.json`), `burst_rate`, `burst_budget`, `sweep_rate`, `concurrency`, `poll_delay`, `starting_window`, `drop_pct`, `drop_min`, `user_agent_contact`.
- [P] Unknown keys in the config file are an error (catches typos). Invalid values fail startup with a clear message.

**Deployment**
- [M] `deploy/` holds a systemd unit (with `AmbientCapabilities=CAP_NET_BIND_SERVICE`), an example config file, and a short `DEPLOY.md`. The notes cover the Cloudflare Origin Certificate, SSL "Full (strict)", the Cache Rule for `/data/*`, and firewalling the droplet to Cloudflare IP ranges.
- [M] `design/screenshots/README.md` lists the screenshots to capture.

## 9. Non-functional requirements

- **Upstream load:** in steady state, averaged over one cycle, no more than one snapshot request (plus 304 retries) and no more than `burst_budget + sweep_rate × RefreshEvery` detail requests. Default ceiling: about 470 per 180s.
- **Freshness:** a new match seen in snapshot S appears in `servers.json` within 90s of S's `FetchedAt` (burst at defaults, healthy upstream).
- **Origin load:** with Cloudflare caching working, the origin sees about one `servers.json` request per Cloudflare location per cycle, regardless of visitor count.
- **Footprint:** runs in under 64 MB RSS on a 1 vCPU / 512 MB droplet. Single static binary, no runtime dependencies.
- **Page weight:** first load under 150 KB transferred. No third-party requests.
- **Resilience:** an upstream outage never empties the site. The last published data stays, marked stale. A crash or restart loses at most one cycle of tracker state.
- **Client performance:** the 1s timer update and a full re-sort of 4,000 rows each finish in under 16 ms on a mid-range laptop. Only visible timer cells are touched each second.
- **Privacy:** no cookies, analytics or user data sent anywhere. localStorage only.
- **Portability:** builds with `go build` on Linux, macOS and Windows. Only the Go standard library unless a dependency is justified in the PLAN.

## 10. Architecture

```
            ┌──────────────── serverdog (one Go binary) ────────────────┐
provider ◀──┤ source adapter ◀─ scheduler ─▶ tracker ─▶ publisher ─▶ data/servers.json
(wardog-    │  (Source iface)    (Clock)      (tiers,     (atomic       │
 servers)   │                                  cache)      write)       │
            │                      http server: embedded static + /data/ │
            └───────────────────────────────▲───────────────────────────┘
                                            │ HTTPS (Origin Cert)
                                     Cloudflare (cache)
                                            │
                                         browsers
```

### 10.1 Technology choices

| Concern | Choice | Notes |
|---|---|---|
| Backend | Go (current stable), standard library only | Single binary; `net/http`, `encoding/json`, `embed`, `log/slog` |
| Rate limiting | Small in-house limiter on top of `Clock` | Avoids a dependency and stays deterministic under the fake clock |
| Frontend | HTML + CSS + vanilla ES modules | No build step. Pure logic in `js/logic/` with no DOM access |
| Frontend tests | `node --test` (Node built-in runner) | Dev-only. No npm packages. Not needed to build or deploy |
| Config | flags > env > JSON file > defaults | JSON keeps it standard-library only |
| Storage | `servers.json` + `tracker.json` on disk | Atomic writes via temp file + rename |
| TLS | Cloudflare Origin Certificate loaded by Go | No Apache or Certbot. Plain HTTP when unset |
| Hosting | DigitalOcean droplet + systemd, Cloudflare proxy at servers.dog | Cache Rule for `/data/*` |

### 10.2 Repository layout

```
cmd/serverdog/main.go            wiring: config → source → scheduler → publisher → http
internal/config/                 flags/env/file loading and validation
internal/clock/                  Clock interface, real clock, fake clock
internal/source/                 Source interface + provider-neutral types
internal/source/wardogservers/   api.wardogservers.com adapter
internal/source/fake/            scriptable fake Source
internal/tracker/                per-server state, tier classification, cache file
internal/scheduler/              poll cycle, burst/sweep, rate limiting, backoff
internal/publish/                servers.json model + atomic writer
internal/web/                    HTTP handlers, cache headers, asset versioning
internal/web/static/             index.html, css/, js/, js/logic/, fonts/
web-tests/                       node --test files for js/logic
testdata/wardogservers/          recorded upstream fixtures (snapshot, detail)
design/screenshots/              in-game UI reference shots + README
deploy/                          systemd unit, example config, DEPLOY.md
docs/                            BRIEF.md, SPEC.md, PLAN.md
```

### 10.3 Key interfaces

```go
// package source — provider-neutral. Nothing outside internal/source/<provider>
// may depend on a specific provider's URLs, JSON, or encodings.

var ErrNotModified = errors.New("snapshot not modified")

type Source interface {
    // Snapshot returns the provider's full current listing. If prevTag is non-empty
    // and nothing changed, it returns ErrNotModified.
    Snapshot(ctx context.Context, prevTag string) (Snapshot, error)

    // MatchStart looks up one server's current match start by join code.
    MatchStart(ctx context.Context, serverID string) (MatchStart, error)
}

// Errors a Source returns to signal throttling; the scheduler backs off.
type RetryAfterError struct{ After time.Duration }

type Snapshot struct {
    ID           string
    Tag          string        // opaque; passed back as prevTag
    FetchedAt    time.Time
    RefreshEvery time.Duration
    Stale        bool
    Complete     bool
    Servers      []Server
}

type Server struct {
    ID, InstanceID, Name, Region string
    Type                         ServerType // Official | Community
    Players, MaxPlayers          int
    PasswordProtected            bool
    Map                          Map  // {Variant, Base string}
    Mode                         Mode // {Experience, GameMode string; Modifiers []string}
    Rulesets                     []string
    ObservedAt                   time.Time
    MatchStart                   *MatchStart // nil unless the provider's list carries it
}

type MatchStartState int // Unknown | NotStarted | Exact
type MatchStart struct {
    State MatchStartState
    At    time.Time // set when State == Exact
}

// package clock
type Clock interface {
    Now() time.Time
    After(d time.Duration) <-chan time.Time
}
```

### 10.4 Fakes

- **Source → `internal/source/fake`.** A scriptable in-memory provider. Tests set a sequence of snapshots and per-server match starts, and inject errors (`ErrNotModified`, `RetryAfterError`, timeouts, 5xx-equivalent errors) for a given call. It records every call (method, server ID, time from the injected clock) so tests can check politeness rules: one detail per server per snapshot, no empty-server fetches, rate caps. It also runs as `source=fake` at startup, generating a small changing world (a few hundred servers across regions, rotating maps, starts and `-1`s), so the UI can be developed offline.
- **wardogservers adapter.** Tested against `httptest.Server` serving recorded fixtures from `testdata/wardogservers/` (snapshot, 304, detail with exact / `-1` / missing `MatchStartUTC`, 429 with `Retry-After`, malformed JSON). No test calls the real API.
- **Clock → `internal/clock` fake.** `Now()` is set by the test. `Advance(d)` fires due `After` channels in order. The scheduler, rate limiter, backoff, tracker windows and publish `nextUpdateAt` all use it, so whole multi-cycle runs can be tested without real waiting.
- **Filesystem:** not faked. Data and cache paths are injected, and tests use `t.TempDir()`.
- **Cloudflare:** not faked. Its behavior follows from our response headers, which tests check directly on handler responses.
- **Browser clock / fetch (frontend):** logic functions take `now` and data as arguments, and the refresh scheduler takes injected `now`/`fetch`/`setTimeout`. `node --test` runs them deterministically.

## 11. UI surfaces

One page, at `/`. Visuals come from `design/screenshots/`; per-screenshot details, colors and owner decisions are in `design/screenshots/NOTES.md`. No game artwork (logo, photos, map images) is used.

- **Navbar:** like the in-game top bar: back button (returns to the type picker), small grey `// SERVER BROWSER` above the type as the title. Where the game shows account stats: ServerDog name, the non-affiliation disclaimer (always visible, not collapsed), stale-data notice when relevant.
- **Type picker:** two cards, Official and Community, in the layout of the older in-game picker (`02-type-picker-old.jpg`), with our own simple icons. Infantry-only, hardcore and level-capped servers are community servers and appear under Community. The choice is saved; a returning visitor goes straight to the saved browser, and the back button returns to the picker.
- **Controls row:** Show Full and Show Empty toggles (independent; on = amber style), and the yellow **Find Fresh Starts** button (shown active while Fresh Starts sorting is on). The game's Refresh, Filter and Join By ID buttons are dropped.
- **Region selector:** a `REGION [n]` button opens a modal listing regions in game order with the game's display names (unknown IDs fall back to the raw ID). Checkboxes apply immediately; there is no Confirm and no limit on how many are selected (the game caps at 3). Ping and "pick best region" are dropped. On a first visit with nothing saved, the region nearest the visitor is preselected if it can be inferred locally (e.g. from the browser's time zone, no permission prompt or request); otherwise none is selected, which shows all regions.
- **Server list:** column headers padlock (password), official check, Server Name, Mode, Map, Players, and Timer in the slot where the game shows Ping. Headers show `▶` when unsorted and `▲` (descending) / `▼` (ascending) on the sorted column, which also gets a lighter header cell. Rows show the name with ruleset chips under it, players/max, map name (text, no image), mode, password indicator and match timer. Rows have hover (lighter, grey border) and selected (gold) states. The selected row gets a gold copy-join-code button styled like the game's Join Match button ([M]). The game's right-hand detail panel is dropped. Scrolls through all results; no pagination.
- **Empty state:** the empty table frame (as in-game), plus a short message naming the active filters (the game shows no message).
- **Footer:** disclaimer repeated, data source credit (api.wardogservers.com), contact.

UX rules:
- Changing type, region, toggles or sort happens instantly in the browser; it never triggers a network request.
- A data refresh keeps the current sort, filters and scroll position.
- Timer cells update in place each second. Rows don't jump during a tick, only on a data refresh or a sort change.

## 12. Quality and testing

- Go: `go test ./...`, `go vet ./...` and `gofmt` clean. Table-driven tests for tick decoding, tier classification and config precedence.
- Scheduler/tracker tests run multi-cycle scenarios with the fake Source and fake Clock and check:
  - P0 is checked in the same cycle as the change;
  - P1 stops after `StartingWindow`;
  - empty servers are never fetched;
  - each server gets at most one detail fetch per snapshot;
  - rate caps hold;
  - unchecked servers are published as estimates;
  - the data turns stale after `2 × RefreshEvery + 60s` with no snapshot.
- Adapter tests use the recorded fixtures only.
- Publisher tests check the JSON schema (§8), atomic replacement, and `Cache-Control` values relative to `nextUpdateAt`.
- Frontend: `node --test "web-tests/**/*.test.js"` covers:
  - timer formatting;
  - filter rules (full includes 0/0);
  - column sorts;
  - Fresh Starts order with all four states and tie-breaks;
  - refresh scheduling (unchanged `generatedAt` → backoff);
  - stale detection;
  - clock offset.
- Manual check before each deploy: run with `source=fake` and with the real provider. Confirm timers tick, Fresh Starts order, toggles, the disclaimers, and response headers (`curl -I`).

## 13. Open questions

- **Fonts:** the game font files aren't available. Decision: self-host Barlow (SIL OFL), which the in-game text closely resembles; confirm side by side in PLAN 2.6.
- **Unseen row variants:** no screenshot shows a password-protected, hardcore or infantry-only row. Assumption: a padlock icon in the padlock column, and extra ruleset chips like the `standard` chip.
- **Provider adding match start to the bulk list:** the API owner is looking into it. Not blocking; handled by `Server.MatchStart` (§10.3, §7.2 step 8).
- **Tuning defaults** (`drop_pct` 30, `drop_min` 25, `starting_window` 20m, rates/budgets): assumed from a 30-server sample. Revisit after a day of live running.
- **Clock skew source:** assumed the Cloudflare edge `Date` header is accurate enough (1s resolution) to correct client clocks. Revisit if timers visibly disagree between devices.
- **Config format:** assumed JSON to stay standard-library only. Switch to TOML only if the user prefers it enough to add a dependency.
- **Servers without a join code:** assumed they're listed but never tracked (timer `—`). Upstream reports codes for all servers today.

## 14. Guidance for AI agents

Agent instructions live in [`AGENTS.md`](../AGENTS.md) (imported by `CLAUDE.md`), with area-specific
rules in `.claude/rules/`. If a rule there contradicts this spec, the spec wins and the rule should be fixed.

## 15. Glossary

- **Provider / Source:** the external service that lists Wardogs servers. Today api.wardogservers.com, accessed only through the `Source` interface.
- **Snapshot:** one complete listing from the provider, identified by `ID` and taken at `FetchedAt`.
- **Cycle:** the provider's refresh period (`RefreshEvery`, 180s today). ServerDog publishes once per cycle.
- **Join code (`serverId`):** the in-game server lookup code. Stays the same across restarts and map changes. ServerDog's server identity.
- **Instance ID:** the provider's per-process server UUID. Changes on restart.
- **Detail fetch:** one per-server request to learn the match start (`Source.MatchStart`).
- **Match start:** when the current match began. Exact (from detail), not-started (`-1`), or unknown.
- **Change observed:** a snapshot in which a server's instance, map variant or experience changed. Taken as evidence of a new match.
- **STARTING:** players present, start not-started, and a change observed within the starting window.
- **Estimate:** a new match inferred from a change observed, but not yet confirmed by a detail fetch. Shown as `~m:ss`.
- **Tiers P0–P3:** detail-fetch priority. P0 likely new match, P1 waiting for a start, P2 never checked, P3 sweep.
- **Burst / sweep:** fast P0/P1 fetching right after a snapshot / slow background fetching for the rest of the cycle.
- **Find Fresh Starts:** the sort mode ordering STARTING → exact newest → estimate newest → unknown.
- **Publish:** writing `servers.json` atomically for the browser and Cloudflare to pick up.
