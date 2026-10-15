---
paths:
  - "internal/web/static/**"
  - "web-tests/**"
---

# Frontend rules

- Vanilla HTML, CSS and ES modules. No npm packages, framework, bundler or build step (SPEC §10.1).
- Modules in `js/logic/` are pure: no DOM, no direct `fetch`, no `Date.now()`/`Math.random()`. `now`,
  data, `fetch`, randomness and timers are passed in. Each has `node --test` coverage in `web-tests/`.
- The only network request is same-origin `/data/servers.json`. No third-party hosts, fonts, CDNs,
  analytics or cookies; preferences go in localStorage (SPEC §5, §9).
- One shared 1s ticker updates visible timer cells in place; never rebuild the list per tick.
  Filter, sort and type/region changes never trigger a fetch (SPEC §7.5, §11).
- Visuals copy `design/screenshots/` and the notes recorded from them. The only intended deviations
  are the match timer, STARTING state and Find Fresh Starts button (SPEC §5 Copy the game).
- Fonts and map images are self-hosted under `internal/web/static/` (copied from `design/`).
