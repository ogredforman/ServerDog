# ServerDog

ServerDog (servers.dog) is an unofficial look-alike of the Wardogs in-game server browser that adds
a live match timer to every server and a yellow **Find Fresh Starts** sort. One Go binary
(standard library only) polls a server-list provider (api.wardogservers.com) behind a
provider-neutral `Source` interface, works out each server's match start while making few upstream
requests, publishes one `servers.json` per provider cycle, and serves an embedded static
HTML/CSS/vanilla-JS page. Cloudflare caches everything in front of a single droplet.

- Product and architecture: `docs/SPEC.md` (source of truth for *what* and *why*)
- Build order and task list: `docs/PLAN.md` (source of truth for *what to do now*)
- Decisions: `docs/adr/`
- Where the code lives, key signatures, test fixtures: `docs/CODEMAP.md` (from Stage 0 onward)
- In-game UI references: `design/screenshots/` and the owner's review notes in `design/screenshots/NOTES.md`

## Before any task

- Read the task in `docs/PLAN.md` and only the SPEC sections it touches. Don't read the whole SPEC by default.
- Orient with `docs/CODEMAP.md`, then read only the functions and files you'll change or call
  (signatures, `grep -n`, line ranges), not whole packages. Everything read stays in context for every later turn.
- The current stage is the one named at the top of `docs/PLAN.md`. Do not implement anything tagged
  for a later stage. Add it to the "Parked" section of PLAN.md instead.
- If a task is ambiguous or conflicts with the SPEC, stop and ask. Don't guess on data model or API shape.
- For anything touching more than 2 files, propose a plan and wait for approval before writing code.

## Commands

- `go run ./cmd/serverdog`: run locally (plain HTTP on `:8080`; add `--source fake` for offline data)
- `go test ./...`: all Go tests. Must pass before a task is finished.
- `node --test "web-tests/**/*.test.js"`: frontend logic tests. Must pass before a task is finished.
- `go vet ./...`: static checks
- `gofmt -l .`: formatting check; must print nothing
- `go build -o bin/serverdog ./cmd/serverdog`: production build (Linux deploy: `GOOS=linux GOARCH=amd64`)

## Hard rules

- Never add AI attribution to commits or PRs: no `Co-Authored-By: Claude…`, no "Generated with Claude Code", nothing similar (owner rule).
- The browser loads only our own same-origin static files and `/data/servers.json`. No JS request to the provider or any third-party host, no external fonts/CDNs/analytics, no cookies (SPEC §5 Static to the visitor, §9 Privacy).
- Provider specifics (URLs, JSON shapes, `int64:` tick decoding, ETags) live only in `internal/source/wardogservers`. Outside `internal/source/...`, only `cmd/serverdog` may import a provider package. Everything else uses `source.Source` and its types (SPEC §5 Provider-agnostic core, §10.3).
- No test touches the network or the real provider. Use `internal/source/fake`, or `httptest.Server` with fixtures from `testdata/wardogservers/` (SPEC §10.4).
- Code under `internal/` gets time only from `clock.Clock`. No `time.Now`, `time.Sleep`, `time.After`, `time.NewTimer` or `time.Tick` outside `internal/clock` (SPEC §10.4).
- Upstream politeness:
  - at most one detail fetch per server per snapshot ID;
  - never fetch details for servers with 0 players;
  - every upstream request goes through the rate limiter;
  - honor `RetryAfterError`;
  - send the ServerDog User-Agent.

  (SPEC §5 Polite upstream client, §7.2)
- Every HTTP response sets an explicit `Cache-Control` as specified in SPEC §7.4 and §8 HTTP serving (SPEC §5 Flat origin load).
- `servers.json` and the tracker cache file are written atomically (temp file in the same dir + rename) (SPEC §7.4).
- Changing the published `servers.json` shape requires bumping `v` and updating SPEC §8 "Published data" in the same commit (SPEC §8).
- Find Fresh Starts only reorders; it never filters out servers (SPEC §5 Fresh Starts sorts, never hides).
- The non-affiliation disclaimer stays visible in both navbar and footer (SPEC §5 Unmistakably unofficial).
- Never present stale data as current: the stale flag and notice logic from SPEC §7.1 and §7.5 must not be bypassed (SPEC §5 Honest data).
- Go code uses only the standard library. The shipped frontend has no npm packages, framework or build step. Pure Go: never set `CGO_ENABLED=1` (SPEC §9 Portability, §10.1).
- With no flags, env or config file, the binary must run locally on plain HTTP `:8080` (SPEC §5 Defaults suit local development).
- Product principles in SPEC §5 are requirements, not style.
- New dependencies need a one-line reason in the commit message. Significant ones get an ADR.

## Style

- Go: `gofmt`, `log/slog` with structured fields, errors wrapped with context. Table-driven tests next to the code.
- Frontend: ES modules. Pure logic in `internal/web/static/js/logic/` takes data and `now` as arguments. DOM code stays thin.
- Tests check behavior from SPEC §7/§8 (tiers, politeness, order, headers), not implementation details.
- Names follow the SPEC glossary (§15): snapshot, cycle, join code, tier P0–P3, STARTING, estimate, publish.

## Finishing a task

1. Tests added or updated; the test and lint commands pass.
2. Run the `devflow:reviewer` agent on the diff and fix any Blocking findings.
3. Tick the task `[x]` in `docs/PLAN.md`.
4. If a design decision changed, update `docs/SPEC.md` or add `docs/adr/NNNN-title.md` in the same commit.
   If a package, public entry point, or test fixture was added or moved, update `docs/CODEMAP.md`.
5. One task per commit. Message format: `<area>: <what changed> (PLAN <stage>.<task>)`. No AI attribution lines.
