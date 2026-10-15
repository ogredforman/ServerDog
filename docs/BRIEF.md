# ServerDog — Brief

- **Date:** 2026-10-15
- **One-liner:** An unofficial, look-alike web server browser for Wardogs at servers.dog whose standout feature is finding matches that just started.

## Problem

Wardogs players want to join a match near its start, but the in-game server browser doesn't show how long each match has been running, so players end up joining matches that are nearly over. The data needed to fix this exists: the community API at api.wardogservers.com exposes every server, and each server's detail record includes `MatchStartUTC`. But nothing presents it in a browsable, familiar way. ServerDog shows the same list players already know from the game, adds a live match timer to every row, and adds one-click "Find Fresh Starts" sorting.

## Goals

1. Players pick a server type (official / community) and region(s), then see every matching server with players, map/mode, and a match timer that ticks every second.
2. "Find Fresh Starts" (yellow-accented button) sorts the current list so matches about to start come first, then the most recently started, without hiding anything.
3. The UI looks as close to identical to the in-game Wardogs server browser as possible (layout, colors, fonts, map thumbnails), modeled on screenshots the user provides. The one intended difference is the live match timer and the STARTING state.
4. Sorting by columns plus "Show Full" / "Show Empty" toggles behave like the game's.
5. Data refreshes in sync with the upstream 180s snapshot cycle. The origin's request rate stays flat regardless of visitor count, because Cloudflare serves almost all traffic.
6. Navbar and footer both state clearly that the site is not affiliated with or endorsed by Bulkhead or Wardogs.

## Non-goals (for now)

- Joining a server from the web. Browsers can't launch the game; we offer a copy-join-code button instead.
- Accounts, favorites synced across devices, or any user data on the server. Region and type choices live in localStorage only.
- Historical charts, player-count stats, or server detail pages (the API has them; we don't need them for the core goal).
- A JS framework or build pipeline for the frontend.
- Hiding stale or full servers in Fresh Starts mode. It's a sort, not a filter.
- Supporting hosts other than a single droplet behind Cloudflare.

## Users and use

Wardogs players (PC) who open the site just before or between matches, pick their region(s), and press "Find Fresh Starts" to find a server to type into the in-game search. Sessions are short: a few minutes, with the page left open while they decide. Traffic is unknown and could spike if the site gets shared. Hosting must stay cheap either way.

## Key decisions

| Decision | Why | Alternatives rejected |
|---|---|---|
| Static page + one `servers.json` data file, refreshed by a Go service | Timers, sorting and toggles need a little JS anyway. A static shell with one JSON file caches perfectly at Cloudflare and keeps the droplet idle | Rendering the server list into HTML every cycle (no gain, worse caching); a dynamic API per visitor (origin load scales with traffic) |
| Poll `/v1/snapshot` (one request) on the upstream cycle, using ETag | The full list has players, maps and join codes in one cheap request; ETag makes "not ready yet" retries free | Paging `/v1/servers` (more requests, same data) |
| Fetch the per-server detail (`/v1/servers/{serverId}`) only for servers likely to have a new match | `MatchStartUTC` exists only on the detail endpoint; fetching ~2,860 servers with players every cycle would mean ~16 req/s against a hobby API | Fetching every server's detail each cycle (abusive); guessing starts from map changes alone (off by 0–15 min) |
| Priority tiers: P0 map/mode/instance changed or sharp player drop → P1 still `-1`/pre-change start (recheck each cycle, max ~20 min) → P2 never checked → P3 slow sweep, oldest check first; never fetch empty servers | Sample of 30 servers: match start lands within ~0–8 min of an observed map change (one case: 15 min), so map change predicts a new match well. The sweep catches same-map matches and missed changes | Fixed-interval round robin (slow to notice new matches) |
| Fetch each detail at most once per snapshot | The detail endpoint returns the same `snapshotId` as the list, so a second fetch within a cycle gains nothing | Polling details on a seconds-based timer |
| Burst P0/P1 checks (~150 at ~4/s) right after a new snapshot, publish once, then sweep slowly (~1.5/s) | New matches appear on the site ~50–60s after the upstream snapshot, with one publish per cycle | Publishing several times per cycle (complicates caching) |
| Sync the whole chain to the 180s cycle: the poller waits until `fetchedAt + refreshSeconds + ~10s`; the JSON carries `generatedAt`/`nextUpdateAt`; `s-maxage` is set to time-until-next-publish; clients fetch at `nextUpdateAt` + 0–10s random delay, then back off if the data isn't new yet | Every request is useful; Cloudflare holds exactly one copy per cycle; the random delay prevents everyone fetching at once | A blind 60s client poll (two of every three fetches wasted) |
| Timers count from the absolute match start, corrected using `generatedAt` | Counters are exact even though the data is up to ~3 min old, and every visitor sees the same value | Showing "minutes ago" from data age |
| STARTING state = map changed within ~20 min and start still `-1` with players | Those servers are the freshest of all. But low-pop community servers sit at `-1` for long periods, so `-1` alone can't mean "starting" | Treating every `-1` as starting |
| Show an estimated start (map-change time, muted "~" display) when a changed server hasn't been checked yet | It's a safety net if the burst is cut short (throttling, cold cache); better than a blank | Showing "—" until checked |
| Fresh Starts order: STARTING → newest exact timer → estimates → "—" | If nothing good is fresh, the user can still pick from the same list without turning anything off | Filtering to "fresh only" |
| Include official and community servers, with type chosen at entry like in-game | Matches the game's flow; community servers report `MatchStartUTC` too | Official only |
| Save the match-start cache to disk each cycle | A restart doesn't trigger a ~10–20 min cold refill against upstream | In-memory only |
| Hide all upstream specifics behind one "match start for server" function | The API owner is looking into adding the match start to the full list; if they do, the detail fetching can be switched off with no other changes | Spreading detail-fetch logic through the poller |
| Go serves HTTPS directly with a Cloudflare Origin Certificate; Cloudflare SSL "Full (strict)"; firewall the droplet to Cloudflare IPs | One process, no Apache, no Certbot renewals (origin certs last up to 15 years) | Apache + Certbot (extra moving parts); Go + Let's Encrypt autocert (renewal and challenge plumbing behind a proxy, for no benefit) |
| Use real map thumbnails and game fonts supplied by the user; fall back to a close-looking free font | The goal is near-identical look; the user believes the devs won't mind, and the site is clearly marked unofficial | Drawing our own map art |

## Stack

- **Backend:** Go, one binary: upstream poller + detail scheduler, JSON writer (atomic write via temp file + rename), static file server with HTML/CSS/JS built in. Single binary, no runtime dependencies, cheap on a small droplet.
- **Frontend:** plain HTML/CSS/vanilla JS, no framework or build step. The page is small, and the only interactivity is sorting, toggles, region/type pickers and a once-per-second timer.
- **Storage:** none beyond a JSON cache file on disk for match starts. Client preferences in localStorage.
- **Hosting:** DigitalOcean droplet running the binary under systemd, serving HTTPS on 443 with a Cloudflare Origin Certificate. Cloudflare in front at **servers.dog** with a Cache Rule making `/data/*` cacheable (Cloudflare doesn't cache JSON by default).

## External dependencies

- **api.wardogservers.com.** Public reads, no key. Uses `/v1/snapshot` (full list, ETag, `meta.refreshSeconds` = 180, `meta.stale`) and `/v1/servers/{serverId}` (detail; `attributes.MatchStartUTC` = `int64:<.NET ticks>`, `-1` = unknown or not started). Requests carry a User-Agent with contact URL `https://servers.dog` and respect 429/503/`Retry-After`. Needs a fake (recorded fixtures plus a scriptable fake server) for development and tests.
- **Cloudflare.** Proxy, cache, origin certificate. No fake needed; behavior is driven by our response headers.
- **User-supplied assets.** Screenshots of the in-game browser (in `design/screenshots/`), map thumbnails, and fonts if available.

## Principles

- The browser must never call the upstream API; it only ever loads our own static files.
- Origin load must not grow with visitor count: everything a visitor fetches can be cached by Cloudflare.
- Be a polite upstream client: one list request per cycle, at most one detail fetch per server per snapshot, a capped and configurable request rate, back off on throttling, never fetch empty servers.
- The disclaimer is visible in both the navbar and the footer on every page.
- Fresh Starts sorts and never hides. The user can always pick something without changing filters.
- Never show stale data as current: if upstream `meta.stale` is set or our last successful poll is too old, show a visible notice.
- Other than the match timer and STARTING state, copy the in-game UI rather than redesigning it.

## Open questions

- **In-game look:** exact layout, colors, region display names, type-picker flow, column set and states. Blocks the UI build; answered by the screenshots the user will drop into `design/screenshots/`.
- **Fonts:** can the game's font be obtained and legally self-hosted? If not, pick the closest free font. Blocks final styling, not structure.
- **Upstream change:** the API owner is considering adding the match start to the full list. If it ships, detail fetching becomes a fallback. Doesn't block anything.
- **Tuning:** the sharp-drop threshold (~30% / ≥25 players), the STARTING window (~20 min), burst and sweep rates. Start with these defaults and tune once running against live data.

## Scope guardrail

The MVP is done when servers.dog is live behind Cloudflare and shows official and community servers for selected regions with live ticking match timers, column sorting, Show Full / Show Empty toggles, and a working Find Fresh Starts button, all styled from the user's screenshots. Data must refresh on the 180s cycle, and the droplet's request rate must stay flat regardless of visitor count.
