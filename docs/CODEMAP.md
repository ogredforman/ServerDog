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

Today `cmd/serverdog/main.go` does: signal context (SIGINT/SIGTERM) → `http.Server` on `:8080` with `web.Handler()` → graceful `Shutdown` (10s).
Target (PLAN 1.8): config → Source (`fake` or `wardogservers`; `cmd/serverdog` is the only non-`source` importer of a provider) → tracker → scheduler → publish → web.

## Packages

- **source** (`internal/source`, SPEC §6.1, §10.3): provider-neutral interface and types. Key: `Source{Snapshot(ctx, prevTag), MatchStart(ctx, serverID)}`, `Snapshot`, `Server`, `ServerType`, `MatchStart`, `MatchStartState`, `Map`, `Mode`, `ErrNotModified`, `RetryAfterError`.
- **source/wardogservers** (SPEC §8 Data provider, §10.3): api.wardogservers.com adapter; all provider URLs, JSON, `int64:` ticks, ETags live here. Placeholder until PLAN 2.1.
- **source/fake** (SPEC §10.4): `New(clk)` scriptable fake Source for tests (records `Calls()`); `NewWorld(clk, seed)` deterministic generated world for `source=fake` (`WorldServers`, `WorldRefresh`; pure function of seed + clock time, no call record).
- **clock** (`internal/clock`, SPEC §10.3–10.4): only source of time under `internal/`. Key: `Clock{Now, After}`, `Real`, `Fake` (`NewFake`, `Set`, `Advance` fires due `After`s in deadline order, `Pending`, `BlockUntil(n)` to sync with goroutines parked on `After`).
- **config** (`internal/config`, SPEC §8 Configuration): `Load(args, getenv) (Config, error)`; flags (`--burst-rate`) → env (`SERVERDOG_BURST_RATE`) → JSON file (`--config`/`SERVERDOG_CONFIG`, key `burst_rate`) → defaults. One `settings()` table holds key, default and validation.
- **tracker** (`internal/tracker`, SPEC §7.2–7.3): clockless per-join-code state machine (callers pass times). Key: `New(Options)`, `Observe(snap)`, `Queue() []Due` (P0, P1, P2, P3 by oldest `CheckedAt`), `Apply(id, ms, checkedAt)`, `Published(id, now)` (exact/estimate/starting/unknown), `Get`. Set `ChangeObservedAt` = unconfirmed estimate. Cache file in PLAN 2.3.
- **scheduler** (`internal/scheduler`, SPEC §7.1): `NewLimiter(clk, perSec)` one shared upstream limiter (`Wait`, `SetRate` burst/sweep, `PauseUntil` for Retry-After); `NewPoller(src, clk, lim, pollDelay)` single-step snapshot poll (`Due`, `Wait`, `Poll`: FetchedAt+RefreshEvery+PollDelay, 304 → 15s, errors 15s→5 min). Burst/sweep cycle in PLAN 1.7.
- **publish** (`internal/publish`, SPEC §7.4, §8 Published data): clockless. Key: `Build(snap, starts, now, pollDelay) File` (`Starts` = `*tracker.Tracker`), `NextUpdateAt`, `Stale`, `Write(ctx, dir, f)` atomic temp + rename to `servers.json`.
- **web** (`internal/web`, SPEC §8 HTTP serving): embedded static files and `/data/servers.json` with `Cache-Control`. Key: `Handler() http.Handler`. Data serving arrives in PLAN 1.8.

## Frontend

- `internal/web/static/`: embedded via `go:embed`; currently only a placeholder `index.html`.
- `internal/web/static/js/logic/`: pure logic modules (data and `now` as arguments), PLAN 1.9–1.10.
- Page structure and live behavior (SPEC §11): PLAN 1.11–1.12. Styling and assets copied from `design/`: PLAN 2.6–2.8.

## Tests and fixtures

- `internal/web/web_test.go`: `httptest` against `Handler()`. `internal/config/config_test.go`: precedence and validation tables, files in `t.TempDir()`. `internal/tracker/tracker_test.go`: per-rule tier, apply and published tables (`srv`/`snap` helpers). `internal/publish/publish_test.go`: schema, state, stale and atomic-write tables against a real tracker. `internal/scheduler/*_test.go`: `driveWaits` (fake-clock limiter driver), `newPoller`/`snap` helpers. `internal/source/source_test.go`, `internal/clock/clock_test.go`: table-driven type/helper tests.
- `web-tests/`: `node --test` files for `js/logic`; `smoke.test.js` is a placeholder.
- `testdata/wardogservers/`: recorded upstream fixtures for `httptest.Server`; empty until PLAN 2.1.
- Fake clock `clock.NewFake` and fake Source `fake.New(clk)`: use these in scheduler/tracker tests; assert politeness on `Calls()`. No test touches the network.
- `fake.NewWorld(clk, seed)`: realistic multi-hundred-server data (rotations, player drops, not-started → exact) for scenario or publish tests that need volume.
- One package: `go test ./internal/<pkg>/`. One frontend file: `node --test web-tests/<file>.test.js`.
