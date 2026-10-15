# ServerDog — Implementation Plan

> Companion to [`SPEC.md`](./SPEC.md). The spec defines *what* and *why*; this plan defines *order*,
> *scope per stage*, and *exit criteria*. Each stage ends in a runnable, usable state. Don't pull
> later-stage work forward.

**Current stage: Stage 1 — Prototype (fakes, no real external dependencies)**

Task IDs are `<stage>.<task>` (e.g. `1.5`). A stage inserted between two others gets a dotted ID
(Stage `1.5`, tasks `1.5.1`, ...), and a task inserted after another gets a letter (`1.5.13b`).
Tick tasks `[x]` as they're finished.
`/devflow:next-task` picks the first unchecked task in the current stage; `/devflow:park` adds ideas
to the Parked section at the bottom.

## Background

- **Idea:** servers.dog, an unofficial look-alike of the Wardogs in-game server browser that adds a live match timer to every server and a yellow **Find Fresh Starts** sort.
- **Why:** players want to join matches near their start. The game doesn't show match age. The community API has it only per server (`MatchStartUTC`), not in the bulk list.
- **Core technique:** poll the provider's snapshot once per 180s cycle. Fetch per-server details only for servers likely to have a new match (P0 map/mode/instance change or player drop, P1 waiting for a start, P2 never checked, P3 slow sweep). Burst, publish once, then sweep.
- **Static to the visitor:** one Go binary writes `servers.json` and serves an embedded static page. Cloudflare caches everything, so origin load stays flat.
- **Provider-agnostic:** all wardogservers.com specifics live behind the `Source` interface, so the provider can be swapped.
- **Sync end to end:** the poller, published `nextUpdateAt`, `s-maxage`, and the client's next fetch all follow the provider's cycle.
- **Stack:** Go (standard library only), vanilla HTML/CSS/ES modules, `node --test` for frontend logic. TLS via a Cloudflare Origin Certificate loaded by Go. Systemd on a DigitalOcean droplet. GitHub + Actions CI.
- **Visuals** are copied from in-game screenshots in `design/screenshots/`, with map thumbnails in `design/maps/` and fonts in `design/fonts/`.
- **MVP guardrail:** servers.dog is live for 7 days, the owner has used Find Fresh Starts to join fresh matches, upstream requests stayed within budget, and the stale notice appeared only during upstream outages.

---

## Stage 0 — Foundations

**Goal:** an empty but correctly wired app that builds, runs, and tests on every target platform.

- [x] **0.1** Initialize git, `go.mod` (module path from the GitHub repo URL), `.gitignore` (`bin/`, `data/`), and the repo layout from SPEC §10.2 with placeholder packages. Add a minimal `cmd/serverdog` that serves a placeholder `index.html` from `internal/web/static` (via `go:embed`) on `:8080`, plus one trivial Go test and one trivial `web-tests/*.test.js`. Commands AGENTS.md will list:
  - build: `go build -o bin/serverdog ./cmd/serverdog`
  - run: `go run ./cmd/serverdog`
  - test: `go test ./...` and `node --test "web-tests/**/*.test.js"`
  - lint: `go vet ./...` and `gofmt -l .` (must print nothing)
- [x] **0.2** Add the GitHub Actions workflow (`.github/workflows/ci.yml`). On push and PR it runs `gofmt -l .` (fails on output), `go vet ./...`, `go test ./...` and `node --test "web-tests/**/*.test.js"` on ubuntu-latest, using the Go version from `go.mod` and Node LTS (SPEC §12).
- [x] **0.3** Declare the provider-neutral types and interfaces in `internal/source`: `Source`, `Snapshot`, `Server`, `ServerType`, `MatchStart`, `MatchStartState`, `ErrNotModified`, `RetryAfterError`. Declare the `Clock` interface plus a real implementation in `internal/clock`. Include doc comments carrying the SPEC rules, and small tests for any helpers (e.g. `ServerType` string mapping) (SPEC §6.1, §10.3).
- [x] **0.4** Start `docs/CODEMAP.md` from the devflow template, describing the packages, the wiring in `cmd/serverdog`, and where each SPEC area will live.

**Exit criteria:**
- `go build -o bin/serverdog ./cmd/serverdog` produces a binary that serves the placeholder page at `http://localhost:8080/`.
- `go test ./...`, `node --test "web-tests/**/*.test.js"`, `go vet ./...` and `gofmt -l .` all pass locally and in CI on GitHub.
- `internal/source` and `internal/clock` compile with the SPEC §10.3 interfaces. `docs/CODEMAP.md` exists.

---

## Stage 1 — Prototype (fakes, no real external dependencies)

**Goal:** the complete core loop working end to end, with every external dependency replaced by a
deterministic fake. Validates the data model, architecture, and UI before real integrations add cost
and unpredictability.

### Fakes and config

- [x] **1.1** Build the fake clock (settable `Now`, `Advance` firing due `After` channels in order) and the scriptable fake `Source`. The fake Source takes a scripted sequence of snapshots and per-server match starts, can inject errors per call (`ErrNotModified`, `RetryAfterError`, timeout, generic error), and records every call with the server ID and fake-clock time. Tests for both (SPEC §10.4).
- [x] **1.2** Add the fake Source's world generator for `source=fake`. It produces a few hundred official and community servers across regions, rotates maps on a schedule, and moves starts from `-1` to exact. It also includes empty, full and 0/0 servers. Deterministic given a seed and clock. Tests check a rotation produces the expected change pattern (SPEC §10.4).
- [x] **1.3** Implement config loading in `internal/config`. Precedence: flags, then `SERVERDOG_*` env, then optional JSON file (`--config`), then defaults. Covers every setting in SPEC §8 Configuration, with local-dev defaults (`:8080`, `./data`, real provider). Unknown file keys and invalid values are startup errors. Table-driven precedence and validation tests (SPEC §8 Configuration, §5 "Defaults suit local development").

### Backend core loop

- [x] **1.4** Implement the tracker in `internal/tracker`:
  - per-join-code state;
  - tier classification exactly per SPEC §7.2 (empty skip, new → P2, change → P0 with `ChangeObservedAt`, player drop → P0, P1 window, P3 by oldest `CheckedAt`, one fetch per snapshot ID, provider-supplied `MatchStart` bypass, 1h eviction);
  - applying detail results per SPEC §7.3;
  - derived published state (exact / estimate / starting / unknown).

  Table-driven tests for every rule, including the snapshot-rounding acceptance window.
- [x] **1.5** Implement the clock-driven rate limiter and the poll timing in `internal/scheduler`: wait until `FetchedAt + RefreshEvery + PollDelay`, 15s retries on `ErrNotModified`, exponential error backoff 15s→5 min, honor `RetryAfterError`. Tests use the fake clock and fake Source (SPEC §7.1 steps 1–2).
- [x] **1.6** Implement the publisher in `internal/publish`. Builds the `servers.json` model (all top-level and per-server keys from SPEC §8 Published data, Unix-second times, empty servers as `unknown`) from a snapshot plus tracker state. Computes `nextUpdateAt` and `stale` (provider stale, or last good snapshot older than `2 × RefreshEvery + 60s`). Writes atomically via temp file + rename. Tests cover schema, estimate/starting output, stale flag, and atomic replace in `t.TempDir()` (SPEC §7.4, §8).
- [x] **1.7** Wire the full cycle in the scheduler: snapshot → tracker → P0/P1 burst at `burst_rate`/`concurrency` within `burst_budget` → publish → P2/P3 sweep at `sweep_rate` until the next snapshot is due. Multi-cycle scenario tests (fake Source + fake clock) check:
  - P0 is fetched in the same cycle;
  - P1 stops after `starting_window`;
  - empty servers are never fetched;
  - at most one detail per server per snapshot;
  - rate caps hold;
  - unchecked P0 servers publish as estimates;
  - the stale flag appears after an outage.

  (SPEC §7.1, §7.2, §9 Upstream load, §12)
- [x] **1.8** Serve the published data and wire `cmd/serverdog`. `internal/web` serves embedded static files and `/data/servers.json` from `data_dir`, with `Cache-Control: public, max-age=N, s-maxage=N` where N is seconds until `nextUpdateAt` (kept between 5 and `RefreshEvery`). `main.go` builds config → Source (`fake` only for now) → tracker → scheduler → publisher → HTTP, and shuts down cleanly on SIGINT/SIGTERM. Handler tests check headers and the 404 before the first publish (SPEC §7.4, §8 HTTP serving, §10).

### Frontend

- [x] **1.9** Write the pure frontend logic modules in `internal/web/static/js/logic/`:
  - timer formatting (`m:ss`, `h:mm:ss`, `~` estimate, STARTING, `—`);
  - type/region filters;
  - Show Full (incl. 0/0) and Show Empty rules;
  - column sorts;
  - Fresh Starts order with tie-breaks;
  - stale detection;
  - clock offset from the `Date` header (applied only when off by more than 2s).

  Each has `node --test` coverage in `web-tests/` (SPEC §7.5, §8 Browser UI, §12).
- [ ] **1.10** Write the refresh scheduler module: next fetch at `nextUpdateAt` + random 0–10s (randomness injected), backoff 15s→30s→60s when `generatedAt` hasn't changed or the fetch fails, keep the last good data. Injected `now`/`fetch`/`setTimeout`, with `node --test` coverage (SPEC §7.5 step 4).
- [ ] **1.11** Build the unstyled page structure (SPEC §11):
  - navbar with placeholder disclaimer text and a stale notice slot;
  - type picker shown on first visit;
  - multi-region selection built from `regions`;
  - Show Full / Show Empty toggles;
  - sortable column headers;
  - server list rows.

  Type, regions, toggles and sort are saved to localStorage. Changing them re-renders without network requests and keeps scroll position (SPEC §7.5 steps 1–3, §8 Browser UI).
- [ ] **1.12** Add the live behavior to the page:
  - one shared 1s ticker that updates only visible timer cells in place;
  - the Find Fresh Starts button (active state; a column click goes back to normal sort);
  - the refresh scheduler hooked to re-render while keeping sort, filters and scroll;
  - the stale notice.

  Manual check against `source=fake` (SPEC §7.5 steps 3–6, §11 UX rules).

**Exit criteria:**
- `go run ./cmd/serverdog --source fake` serves a page where you can:
  - pick a type and regions;
  - sort by every column;
  - toggle Show Full / Show Empty;
  - watch timers tick every second;
  - press Find Fresh Starts and see STARTING → newest → estimates → unknown.
- Over several fake cycles the page refreshes itself at `nextUpdateAt`. Rotated maps show up as estimates then exact starts.
- Scheduler scenario tests prove the politeness rules. All tests pass in CI.

---

## Stage 2 — MVP (real integrations, actually usable)

**Goal:** swap fakes for real integrations, with just enough polish that the owner uses it for real.

### Real provider and hardening

- [ ] **2.1** Implement the wardogservers.com adapter in `internal/source/wardogservers`:
  - `/v1/snapshot` with ETag/If-None-Match → `ErrNotModified`;
  - `/v1/servers/{serverId}` with `MatchStartUTC` .NET-tick decoding (`-1` → NotStarted, missing → Unknown);
  - 429/503 + `Retry-After` → `RetryAfterError`;
  - `User-Agent: ServerDog/<version> (+https://servers.dog)` and a 20s timeout.

  Record fixtures once into `testdata/wardogservers/` (snapshot, detail exact / `-1` / missing, 429, malformed). Test against `httptest.Server` only. Make `source=wardogservers` the default in `main.go` (SPEC §8 Data provider, §10.4).
- [ ] **2.2** Harden throttling: repeated 429/503 pause all detail fetching with backoff and cut the burst short (unchecked P0 servers publish as estimates). Check that the configured total detail rate cap holds across burst + sweep. Scenario tests (SPEC §7.3 step 4, §8 Data provider).
- [ ] **2.3** Save the tracker to `cache_file` after each cycle (atomic) and load it at startup. A corrupt or missing file logs a cold start. Switch off detail fetching automatically when snapshots carry `Server.MatchStart`. Tests for round-trip, corrupt file, and the bypass (SPEC §7.1 step 7, §7.2 step 8, §8 Scheduling and tracking).
- [ ] **2.4** Add caching and health:
  - `index.html` `public, max-age=60, s-maxage=300`;
  - content-hash `?v=` asset versioning filled into `index.html` at startup, with versioned assets `public, max-age=31536000, immutable`;
  - `/healthz` with last snapshot age, `no-store`.

  Handler tests for every header (SPEC §8 HTTP serving, §5 Flat origin load).
- [ ] **2.5** Add HTTPS: serve TLS on `listen` when `tls_cert`/`tls_key` are set, plain HTTP otherwise. Test with a generated self-signed cert. Add a publisher test that gzip size stays under 100 KB at 4,000 generated servers (SPEC §8 HTTP serving, §8 Published data).

### In-game look (uses `design/screenshots/`, `design/maps/`, `design/fonts/`)

- [ ] **2.5b** Review the screenshots with the owner. Go through each file in `design/screenshots/` one at a time: the owner describes what it shows and how it behaves, and the agent asks about anything unclear (colors, states, interactions, region names, type-picker flow, which `02-type-picker` version is current). Record the result per screenshot in `design/screenshots/NOTES.md`. Update SPEC §11 and close the visual items in SPEC §13 from it. No code (SPEC §11, §13).
- [ ] **2.6** Style the page layout from the screenshots and `design/screenshots/NOTES.md`: overall frame, colors, typography (game font if supplied, else the closest free self-hosted font), navbar, type picker, region selector with in-game display names (fallback to ID), toggles, column headers and sort indicators. Copy assets from `design/` into `internal/web/static/` (SPEC §11, §8 Browser UI, §5 Copy the game).
- [ ] **2.7** Style the rows and states from the screenshots and NOTES.md:
  - map thumbnails keyed by variant, with a neutral placeholder;
  - full / empty / password / ruleset looks;
  - hover / selected;
  - timer, STARTING and muted `~` estimate styling;
  - the yellow Find Fresh Starts button;
  - the empty-state message naming active filters.

  Check the 16 ms budget for the 1s tick and a 4,000-row re-sort (SPEC §8 Browser UI, §9 Client performance, §11).
- [ ] **2.8** Add the final disclaimer ("Unofficial fan site. Not affiliated with or endorsed by Bulkhead or Wardogs.") in both the navbar (always visible) and the footer. The footer also credits api.wardogservers.com and gives a contact. Add a per-row copy-join-code control with "Copied" feedback. Confirm the first load is under 150 KB excluding thumbnails, with no third-party requests (SPEC §5 Unmistakably unofficial, §8 Browser UI, §9 Page weight, Privacy).

### Deploy and go live

- [ ] **2.9** Write `deploy/`:
  - systemd unit (`AmbientCapabilities=CAP_NET_BIND_SERVICE`, restart on failure, dedicated user);
  - `serverdog.example.json`;
  - `deploy.sh` (`GOOS=linux GOARCH=amd64 go build` → `scp` → `systemctl restart`);
  - `DEPLOY.md` covering the Cloudflare Origin Certificate, SSL "Full (strict)", the Cache Rule for `/data/*`, firewalling to Cloudflare IP ranges, and the `curl -I` header checks (SPEC §8 Deployment, §10.1).
- [ ] **2.10** Deploy to servers.dog. Verify with `curl -I` that `servers.json`, `index.html` and assets return the expected `Cache-Control` and `cf-cache-status: HIT` on repeat. Confirm the binary stays under 64 MB RSS. After 24h of live running, review the logs for upstream request counts per cycle and freshness (new match published within 90s of the snapshot), then tune `drop_pct`, `drop_min`, `starting_window` and the rates. Record the chosen values in SPEC §13 (SPEC §9, §13).

**Exit criteria:**
- servers.dog has been live for 7 days, and the owner has used Find Fresh Starts to join fresh matches.
- Upstream requests per cycle stayed within the SPEC §9 budget. The stale notice showed only during upstream outages.
- Visitors see the in-game look with disclaimers in navbar and footer. Cloudflare serves repeat requests as cache hits.

---

## Stage 3 — Refinement (driven by real use)

**Goal:** fix what real use revealed. Priorities come from the friction log and Parked items,
not from this list. Candidates:

- Retune tier thresholds and rates from a week of live data.
- Drop or demote detail fetching if the provider adds match start to the bulk list.
- Fix visual differences from the in-game browser that players point out.

**Exit criteria:** defined at the start of the stage.

---

## Stage 4 — Packaging

**Goal:** easy for someone who isn't the author to install and run.

- Not planned yet. ServerDog is a single deployment; `deploy/` from Stage 2 covers the owner's install.

**Exit criteria:** defined at the start of the stage.

---

## Later / future (not planned yet)

- Search box for name / map, if the in-game browser has one (SPEC §8 [L]).
- Per-region data files if `servers.json` exceeds its size budget (SPEC §8 [L]).

---

## Working agreements

- **One stage at a time.** Finish the exit criteria before starting the next stage.
- **One task per session, one task per commit.**
- **Keep docs true.** If implementation changes a decision, update SPEC.md/PLAN.md or add an ADR in the same change.
- **Definition of done:** code, tests, docs updated if needed, reviewer run, task ticked.

---

## Parked

Ideas and tangents captured mid-work (via `/devflow:park`). Not scheduled. Review them at the start of each stage.
