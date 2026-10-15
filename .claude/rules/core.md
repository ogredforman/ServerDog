---
paths:
  - "internal/tracker/**"
  - "internal/scheduler/**"
  - "internal/publish/**"
---

# Tracker, scheduler and publisher rules

- Tier classification and detail-result handling follow SPEC §7.2 and §7.3 exactly. A rule change
  updates the SPEC in the same commit.
- All timing (cycle waits, backoff, rate limiting, `StartingWindow`, eviction, `nextUpdateAt`) uses
  the injected `clock.Clock`. Tests drive it with the fake clock and never sleep.
- Detail fetches go through the scheduler's rate limiter; `burst_rate`, `sweep_rate`,
  `concurrency` and `burst_budget` come from config, never hard-coded.
- Behavior tests are multi-cycle scenarios with the fake Source and fake clock, asserting on the
  fake's recorded calls (SPEC §12).
- The publisher writes only the SPEC §8 "Published data" keys, times as Unix seconds, via
  temp file + rename in the target directory.
