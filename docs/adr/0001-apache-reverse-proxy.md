# 0001. Apache terminates TLS and reverse-proxies to ServerDog

- **Status:** Accepted
- **Date:** 2026-10-16
- **PLAN task:** 2.9

## Context

SPEC §10.1 had the Go binary load the Cloudflare Origin Certificate itself and bind :443, with
"no Apache or Certbot". The droplet that hosts servers.dog already runs Apache for
redforman.dev, and only one process can own :443. Both domains go behind Cloudflare, with the
web ports firewalled to Cloudflare's ranges (SPEC §8 Deployment).

## Options considered

1. **Go owns :443 and proxies redforman.dev.** No Apache, but ServerDog would grow into a general
   reverse proxy for an unrelated site, and the portfolio would depend on it.
2. **Second IP or port for ServerDog.** Cloudflare's proxy only reaches origin HTTPS on a fixed
   list of ports, and a second IP costs money and complicates DNS for no gain.
3. **Apache terminates TLS for both sites and proxies servers.dog to the binary on loopback.**
   Uses what's already running. Apache gets one more site. The binary needs no privileges.

## Decision

Option 3. Apache serves servers.dog on :443 with the Cloudflare Origin Certificate and
`ProxyPass`es to ServerDog on `127.0.0.1:8080` (plain HTTP). The binary's `tls_cert`/`tls_key`
support (PLAN 2.5) stays for setups without a front proxy.

## Consequences

- The systemd unit drops `CAP_NET_BIND_SERVICE` and runs with no capabilities.
- The Apache site must not set or rewrite `Cache-Control`, `Expires` or similar: Cloudflare
  caches by ServerDog's headers (SPEC §7.4, §8 HTTP serving). DEPLOY.md's `curl -I` checks
  confirm this after each change.
- Visitor IPs reach Apache via `mod_remoteip` (`CF-Connecting-IP`, trusted only from Cloudflare's
  ranges). ServerDog doesn't log or use visitor IPs.
- `deploy/` documents Apache setup and a Cloudflare tutorial covering both domains.
