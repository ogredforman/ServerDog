# Code map

A short orientation to the code, so a task can start without reading whole packages. Read this
first, then open only the files you'll change or call. Keep it current: when a task adds a
package, a public entry point, or a test fixture, update the matching line here in the same commit.
Signatures here are summaries; `go doc ./internal/<pkg>` (or `go doc ./internal/<pkg> <Name>`) gives the exact ones.

<!-- Keep this a map, not documentation: one line per thing. If a line needs a paragraph, the
     detail belongs in the code's own doc comments or an ADR. -->

## Reading the docs cheaply

- SPEC sections by heading: `awk '/^### 7.1/,/^### 7.2/' docs/SPEC.md` (or `/^## 7\./,/^## 8\./` for a whole section).
  Worth knowing: §5 principles, §6.1 entities, §7.1 poll cycle, §7.2–7.3 tracking, §7.4 publish, §7.5 browser,
  §8 requirements (bold subheads: Data provider, Scheduling and tracking, Published data, HTTP serving, Browser UI, Configuration),
  §10.2 layout, §10.3 interfaces, §10.4 fakes, §11 UI, §15 glossary.
- PLAN: `grep -n '\[ \]' docs/PLAN.md | head` for the next tasks; read only the current stage's section.
- ADRs: none yet (`docs/adr/0000-template.md` is the template).

## Wiring

`cmd/serverdog/main.go`: `config.Load` → `newSource` (`wardogservers.New` by default with `UpstreamURL`, `version` (ldflags `-X main.version=`) and `UserAgentContact`; `fake.NewWorld(clock.Real{}, 1)` for `source=fake`; `cmd/serverdog` is the only non-`source` importer of a provider) → `tracker.New` + `loadCache` (`LoadFile`; missing/corrupt = logged cold start) → `scheduler.New` (Publish = `publish.Write` then `site.Published`; Save = `trk.SaveFile(cfg.CacheFile)`) → `http.Server` on `cfg.Listen` with `web.New`. SIGINT/SIGTERM: cancel and await the scheduler, then `Shutdown` (10s).

## Packages

- **source** (`internal/source`, SPEC §6.1, §10.3): provider-neutral interface and types. Key: `Source{Snapshot(ctx, prevTag), MatchStart(ctx, serverID)}`, `Snapshot`, `Server`, `ServerType`, `MatchStart`, `MatchStartState`, `Map`, `Mode`, `ErrNotModified`, `RetryAfterError`.
- **source/wardogservers** (SPEC §8 Data provider, §10.3): api.wardogservers.com adapter; all provider URLs, JSON, `int64:` ticks, ETags live here. Key: `New(Options{BaseURL, Version, Contact, HTTPClient}) *Client` (20s timeout), `UserAgent(version, contact)`; ETag → `Snapshot.Tag`, 304 → `ErrNotModified`, 429/503 → `RetryAfterError` (seconds or HTTP date vs response `Date`, default 15s); upstream `id` = `InstanceID`, `serverId` = join code; unknown `type` servers are skipped.
- **source/fake** (SPEC §10.4): `New(clk)` scriptable fake Source for tests (records `Calls()`); `NewWorld(clk, seed)` deterministic generated world for `source=fake` (`WorldServers`, `WorldRefresh`; pure function of seed + clock time, no call record).
- **clock** (`internal/clock`, SPEC §10.3–10.4): only source of time under `internal/`. Key: `Clock{Now, After}`, `Real`, `Fake` (`NewFake`, `Set`, `Advance` fires due `After`s in deadline order, `Pending`, `BlockUntil(n)` to sync with goroutines parked on `After`, `Next` earliest pending deadline).
- **config** (`internal/config`, SPEC §8 Configuration): `Load(args, getenv) (Config, error)`; flags (`--burst-rate`) → env (`SERVERDOG_BURST_RATE`) → JSON file (`--config`/`SERVERDOG_CONFIG`, key `burst_rate`) → defaults. One `settings()` table holds key, default and validation.
- **tracker** (`internal/tracker`, SPEC §7.2–7.3): clockless per-join-code state machine (callers pass times). Key: `New(Options)`, `Observe(snap)`, `Queue() []Due` (P0, P1, P2, P3 by oldest `CheckedAt`), `Apply(id, ms, checkedAt)`, `Published(id, now)` (exact/estimate/starting/unknown), `Get`. Set `ChangeObservedAt` = unconfirmed estimate; a snapshot `Server.MatchStart` sets tier None (no detail fetch). `cache.go`: `Save(w)`/`Load(r)` versioned JSON (tiers not saved; `Load` all-or-nothing), `SaveFile(ctx, path)` atomic, `LoadFile(ctx, path)` (missing wraps `fs.ErrNotExist`), `Len`.
- **scheduler** (`internal/scheduler`, SPEC §7.1): `New(src, clk, trk, Options) (*Scheduler, error)` + `Run(ctx)`: poll → `Observe` → P0/P1 burst within budget → `Options.Publish` → sweep until next poll due → `Options.Save` (optional, uncancelled ctx; failure logged); stale republish once at `publish.StaleAt`+1s. `NewLimiter(clk, perSec)` one shared upstream limiter (`Wait`, `WaitBefore(deadline)` for the sweep, `SetRate` burst/sweep, `PauseUntil` for Retry-After); 2 throttled details in a row pause detail fetching only (max(Retry-After, 15s→5 min backoff)), cutting the burst short; `NewPoller(src, clk, lim, pollDelay)` single-step snapshot poll (`Due`, `Wait`, `Poll`: FetchedAt+RefreshEvery+PollDelay, 304 → 15s, errors 15s→5 min).
- **atomicfile** (`internal/atomicfile`, SPEC §7.4): `Write(ctx, path, perm, func(io.Writer) error)` temp file in the same dir + sync + rename; used by `publish.Write` and `tracker.SaveFile`.
- **publish** (`internal/publish`, SPEC §7.4, §8 Published data): clockless. Key: `Build(snap, starts, now, pollDelay) File` (`Starts` = `*tracker.Tracker`), `NextUpdateAt`, `Stale`/`StaleAt`, `Write(ctx, dir, f)` atomic (via `atomicfile`) to `servers.json`.
- **web** (`internal/web`, SPEC §8 HTTP serving): embedded static files (`/`, `GET /js/{path...}` .js/.css only, unversioned `public, max-age=60, s-maxage=300`) and `/data/servers.json` with `Cache-Control`; `ServeHTTP` defaults `no-store` for mux-written responses. Key: `New(clk, dataDir) *Site` (http.Handler), `(*Site).Published(publish.File)` records nextUpdateAt/refreshSeconds; servers.json is 404 until the first publish.

## Frontend

- `internal/web/static/`: embedded via `go:embed`. `index.html` unstyled structure (navbar + `#stale-notice` slot, `#type-picker`, `#controls`, `#server-list` table with `data-sort` header buttons, `#empty`, footer); `js/app.js` thin DOM wiring: prefs → `createRefresher` (`onData` sets clock offset, re-renders keeping scroll) → render; one 1s `setInterval` updates only on-screen exact/estimate timer cells (tracked by `IntersectionObserver`) and the stale notice; `#fresh-starts` sets `prefs.freshStarts`, a header click clears it.
- `internal/web/static/js/logic/` (pure; `now` in Unix ms, JSON times in Unix s): `timer.js` `formatDuration`, `formatTimer(server, now)` → `{text, kind}`; `filters.js` `isFull`, `isEmpty`, `filterServers(servers, {type, regions, showFull, showEmpty})` (empty regions = all); `sort.js` `sortServers(servers, {column, dir})`, `sortColumns`, `freshStartsOrder`; `stale.js` `isStale(data, now)`; `clock.js` `clockOffset(dateHeader, localNow)`; `refresh.js` `freshDelay`, `backoffDelay`, `createRefresher({url, fetch, now, setTimeout, clearTimeout, random, onData, onError})` → `{start, stop, data}` (`onData` runs before the next delay is computed); `prefs.js` `loadPrefs(storage)`, `savePrefs(storage, prefs)`, `nextSort(sort, column)`, `DEFAULT_PREFS` (incl. `freshStarts`), `PREFS_KEY`; `view.js` `viewServers(servers, prefs, regions)` (Fresh Starts order when `prefs.freshStarts`), `rowCells(server, now)`, `activeFilters(prefs, regions)`, `staleNotice(data, now)`.
- Styling and assets copied from `design/`: PLAN 2.6–2.8.

## Tests and fixtures

- `internal/web/web_test.go`: `httptest` against `Site` (`newSite` helper: temp data dir + fake clock). `internal/config/config_test.go`: precedence and validation tables, files in `t.TempDir()`. `internal/tracker/tracker_test.go`: per-rule tier, apply and published tables (`srv`/`snap` helpers); `cache_test.go` round trip (`cachedTracker`: one server per published state), corrupt-input table, files in `t.TempDir()`. `internal/publish/publish_test.go`: schema, state, stale and atomic-write tables against a real tracker. `internal/scheduler/*_test.go`: `driveWaits` (fake-clock limiter driver), `newPoller`/`snap` helpers; `scheduler_test.go` `harness` (event log of requests + publishes + saves, `failPublishes`/`failSaves`, `run` steps the clock via `BlockUntil`+`Next`, `cycles()`, `checkPolite` politeness asserts, `checkRates` total rate cap + Retry-After over every request; `throttle` Source wrapper 429s details in time windows; keep `Concurrency` 1 when asserting request times). `internal/source/source_test.go`, `internal/clock/clock_test.go`: table-driven type/helper tests.
- `web-tests/`: one table-driven `node --test` file per `js/logic` module (`timer`, `filters`, `sort`, `stale`, `clock`, `refresh`, `prefs`, `view`), importing via relative path.
- `testdata/wardogservers/`: upstream fixtures recorded 2026-10-16 UTC (`snapshot.json` trimmed to 6 servers, ETag in the test; `detail-exact.json`; hand-edited `detail-not-started.json` / `detail-missing.json`; truncated `malformed.json`). `internal/source/wardogservers/wardogservers_test.go` `newFixtureServer(t, routes)` serves them by path and records requests; 429/503 are built per test.
- Fake clock `clock.NewFake` and fake Source `fake.New(clk)`: use these in scheduler/tracker tests; assert politeness on `Calls()`. No test touches the network.
- `fake.NewWorld(clk, seed)`: realistic multi-hundred-server data (rotations, player drops, not-started → exact) for scenario or publish tests that need volume.
- One package: `go test ./internal/<pkg>/`. One frontend file: `node --test web-tests/<file>.test.js`.
