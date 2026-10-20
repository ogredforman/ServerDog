# ServerDog

**Live at [servers.dog](https://servers.dog).** An unofficial server browser for the game Wardogs
that looks like the in-game one and adds the one thing it's missing: a live match timer on every
server, plus a **Find Fresh Starts** sort that puts matches about to start at the top.

<!-- TODO: screenshot of servers.dog with Find Fresh Starts on -->

Not affiliated with or endorsed by Bulkhead or Wardogs.

## The problem

Players want to join a match near its start, but the game doesn't show how long a match has been
running. The community API has the match start time, but only in each server's detail record, not
in the bulk list. A naive client would need thousands of requests every cycle, against an API run
by a hobbyist.

## How it works

```
            ┌──────────────── serverdog (one Go binary) ────────────────┐
provider ◀──┤ source adapter ◀─ scheduler ─▶ tracker ─▶ publisher ─▶ data/servers.json
            │                                (tiers,     (atomic       │
            │                                 cache)      write)       │
            │                      http server: embedded static + /data/ │
            └───────────────────────────────▲───────────────────────────┘
                                            │
                              nginx (TLS) ◀─ Cloudflare (cache) ◀─ browsers
```

- **Poll once per upstream cycle (180 s).** Fetch the bulk snapshot, then fetch details only for
  servers likely to have a new match: map or mode changed, players dropped, waiting for a start,
  or never checked. Everything else gets a slow sweep. Requests stay inside a fixed budget.
- **Publish one file.** Each cycle writes one `servers.json` atomically (temp file + rename).
  The page is static and reads that file.
- **Cache everything.** Cache headers line up with the upstream cycle, so Cloudflare serves almost
  every request and origin load doesn't grow with visitors.
- **Provider-neutral.** All of the upstream API's specifics live behind a `Source` interface, so
  the data provider can be swapped without touching the rest.

## Stack

- **Backend:** Go, standard library only (`net/http`, `embed`, `log/slog`), with an in-house rate
  limiter and a fake clock for deterministic tests.
- **Frontend:** HTML, CSS and vanilla ES modules, no build step or npm dependencies. Pure logic
  (sorting, filters, timers) is tested with Node's built-in test runner.
- **Hosting:** DigitalOcean droplet with systemd, nginx with a Cloudflare Origin Certificate, and
  Cloudflare in front. See [`deploy/DEPLOY.md`](deploy/DEPLOY.md) and [ADR 0001](docs/adr/0001-nginx-reverse-proxy.md).
- **CI:** GitHub Actions.

## How it's built

I wrote the spec ([`docs/SPEC.md`](docs/SPEC.md)) and plan ([`docs/PLAN.md`](docs/PLAN.md)), and
AI coding agents built it one task at a time using my
[Devflow](https://github.com/ogredforman/Devflow) workflow, with tests and a review on each task.
From idea to deployed site took about half a day.

## Run it locally

Requires Go.

```sh
go run ./cmd/serverdog
```

Then open http://localhost:8080. Config comes from flags, environment variables or a JSON file;
see [`deploy/serverdog.example.json`](deploy/serverdog.example.json).
