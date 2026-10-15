---
paths:
  - "internal/web/*.go"
  - "cmd/serverdog/**"
---

# HTTP serving rules

- Every response sets `Cache-Control` explicitly (SPEC §7.4, §8 HTTP serving):
  - `servers.json`: `public, max-age=N, s-maxage=N`, N = seconds to `nextUpdateAt`, kept between 5 and `RefreshEvery`;
  - `index.html`: `public, max-age=60, s-maxage=300`;
  - versioned assets: `public, max-age=31536000, immutable`;
  - `/healthz` and errors: `no-store`.
- Static assets are served from the `go:embed` FS; data files from `data_dir`. Never serve
  arbitrary filesystem paths.
- Tests assert headers on `httptest` responses; Cloudflare is not faked.
- No per-visitor dynamic endpoints beyond `/healthz` (SPEC §5 Static to the visitor).
