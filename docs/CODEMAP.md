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
- **source/fake** (SPEC §10.4): scriptable fake Source and `source=fake` world generator. Placeholder until PLAN 1.1–1.2.
- **clock** (`internal/clock`, SPEC §10.3–10.4): only source of time under `internal/`. Key: `Clock{Now, After}`, `Real`, `Fake` (`NewFake`, `Set`, `Advance` fires due `After`s in deadline order, `Pending`, `BlockUntil(n)` to sync with goroutines parked on `After`).
- **config** (SPEC §8 Configuration): flags → `SERVERDOG_*` env → JSON file → defaults. Placeholder until PLAN 1.3.
- **tracker** (SPEC §7.2–7.3): per-server state, tiers P0–P3, cache file. Placeholder until PLAN 1.4 (persistence 2.3).
- **scheduler** (SPEC §7.1): poll timing, rate limiter, backoff, burst/sweep cycle. Placeholder until PLAN 1.5, 1.7.
- **publish** (SPEC §7.4, §8 Published data): `servers.json` model, stale flag, atomic write. Placeholder until PLAN 1.6.
- **web** (`internal/web`, SPEC §8 HTTP serving): embedded static files and `/data/servers.json` with `Cache-Control`. Key: `Handler() http.Handler`. Data serving arrives in PLAN 1.8.

## Frontend

- `internal/web/static/`: embedded via `go:embed`; currently only a placeholder `index.html`.
- `internal/web/static/js/logic/`: pure logic modules (data and `now` as arguments), PLAN 1.9–1.10.
- Page structure and live behavior (SPEC §11): PLAN 1.11–1.12. Styling and assets copied from `design/`: PLAN 2.6–2.8.

## Tests and fixtures

- `internal/web/web_test.go`: `httptest` against `Handler()`. `internal/source/source_test.go`, `internal/clock/clock_test.go`: table-driven type/helper tests.
- `web-tests/`: `node --test` files for `js/logic`; `smoke.test.js` is a placeholder.
- `testdata/wardogservers/`: recorded upstream fixtures for `httptest.Server`; empty until PLAN 2.1.
- Fake clock `clock.NewFake` and fake Source `fake.New(clk)`: use these in scheduler/tracker tests; assert politeness on `Calls()`. No test touches the network.
- One package: `go test ./internal/<pkg>/`. One frontend file: `node --test web-tests/<file>.test.js`.
