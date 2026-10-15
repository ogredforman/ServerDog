---
paths:
  - "internal/source/**"
  - "testdata/wardogservers/**"
---

# Data provider rules

- `internal/source` holds only provider-neutral types and the `Source` interface (SPEC §10.3). No
  provider URLs, JSON tags or encodings there.
- `internal/source/wardogservers` maps upstream JSON to `source` types and returns only `source`
  errors (`ErrNotModified`, `RetryAfterError`, wrapped errors). `MatchStartUTC`: `int64:<.NET ticks>`,
  `-1` → NotStarted, missing → Unknown (SPEC §8 Data provider).
- Adapter tests use `httptest.Server` with recorded fixtures in `testdata/wardogservers/`. Fixtures
  are recorded once by hand; never fetch them in a test or `go generate` step.
- `internal/source/fake` stays deterministic: seeded randomness and the injected `clock.Clock` only.
  It records every call so scheduler tests can assert politeness (SPEC §10.4).
- A new provider gets its own `internal/source/<name>` package and fixtures; nothing outside
  `internal/source/...` and `cmd/serverdog` changes to support it.
