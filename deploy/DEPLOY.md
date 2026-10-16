# Deploying ServerDog

ServerDog runs on the same DigitalOcean droplet as redforman.dev:

```
visitor ──HTTPS──▶ Cloudflare (cache) ──HTTPS──▶ Apache :443 ──HTTP──▶ serverdog 127.0.0.1:8080
                                        Origin Certificate          (systemd, user serverdog)
```

- **Cloudflare** serves almost every request from its cache. It obeys the `Cache-Control`
  headers ServerDog sets (SPEC §7.4, §8 HTTP serving).
- **Apache** terminates TLS with the Cloudflare Origin Certificate and proxies to the binary
  (ADR 0001).
- **serverdog** listens on loopback only, so it's reachable from outside the droplet only
  through Apache.

Putting the domains behind Cloudflare is its own tutorial, [CLOUDFLARE.md](CLOUDFLARE.md). It
covers servers.dog and redforman.dev and the firewall they share. Do the steps in this order:

1. CLOUDFLARE.md steps 1–4: add both domains to Cloudflare, switch nameservers, and create the
   Origin Certificates once the domains are Active.
2. This file, steps 1–5: install ServerDog and its Apache site.
3. CLOUDFLARE.md steps 5–9: SSL settings, firewall, caching, verify.

Commands below assume Ubuntu with Apache already serving redforman.dev, run as a sudo user.

## Files in `deploy/`

| File | Goes to | What it is |
|---|---|---|
| `serverdog.service` | `/etc/systemd/system/serverdog.service` | systemd unit: user `serverdog`, restart on failure, state in `/var/lib/serverdog` |
| `serverdog.example.json` | `/etc/serverdog/serverdog.json` | config: loopback listen, data dir, User-Agent contact, rates |
| `apache-servers.dog.conf` | `/etc/apache2/sites-available/servers.dog.conf` | Apache site: TLS + reverse proxy |
| `cloudflare-ips.sh` | run on the droplet | ufw rules + Apache real-IP config for Cloudflare's ranges |
| `deploy.sh` | run on your machine | build, copy, install, restart |

## 1. User and directories

```sh
sudo useradd --system --no-create-home --shell /usr/sbin/nologin serverdog
sudo mkdir -p /etc/serverdog
```

systemd creates `/var/lib/serverdog` (owned by `serverdog`) on first start, from
`StateDirectory=` in the unit.

## 2. Config and unit

Copy `deploy/serverdog.example.json` and `deploy/serverdog.service` to the droplet, then:

```sh
sudo install -o root -g serverdog -m 0640 serverdog.example.json /etc/serverdog/serverdog.json
sudo install -o root -g root -m 0644 serverdog.service /etc/systemd/system/serverdog.service
sudo systemctl daemon-reload
sudo systemctl enable serverdog
```

Leave `tls_cert`/`tls_key` unset: Apache does TLS. Tune the rates and windows in
`/etc/serverdog/serverdog.json` and restart (PLAN 2.10 records the chosen values). Every key
can also be overridden with a `SERVERDOG_*` environment variable in a systemd drop-in
(`sudo systemctl edit serverdog`).

## 3. First deploy

From the repo on your machine (Git Bash, WSL, macOS or Linux):

```sh
DEPLOY_HOST=you@<droplet-ip> deploy/deploy.sh
```

It builds a static Linux binary with the version from `git describe`, copies it over, keeps the
old one as `/usr/local/bin/serverdog.prev`, restarts the service, and prints `/healthz`. Use the
droplet's IP, not `servers.dog`: once that's behind Cloudflare it doesn't reach the droplet over
SSH. The remote user needs `sudo`; the script uses `ssh -t`, so sudo can ask for a password.

Check it on the droplet:

```sh
systemctl status serverdog
journalctl -u serverdog -f            # structured logs: cycles, upstream requests, publishes
curl -s http://127.0.0.1:8080/healthz # {"snapshotAgeSeconds":..., "stale":false}
```

`snapshotAgeSeconds` is `null` until the first publish, a few seconds after start.

## 4. Origin Certificate

Create it and save it as `/etc/ssl/cloudflare/servers.dog.pem` and `.key` (CLOUDFLARE.md step 4).
Apache reads the key as root before dropping privileges, so `0600 root` is enough.

## 5. Apache site

```sh
sudo a2enmod ssl proxy proxy_http remoteip
sudo install -m 0644 apache-servers.dog.conf /etc/apache2/sites-available/servers.dog.conf
sudo a2ensite servers.dog
sudo apache2ctl configtest && sudo systemctl reload apache2
```

Don't add `Header`, `Expires` or `mod_cache` rules to this site. ServerDog's `Cache-Control`
headers must reach Cloudflare unchanged. Check from the droplet before Cloudflare is in front
(`-k` because the Origin Certificate is only trusted by Cloudflare):

```sh
curl -skI --resolve servers.dog:443:127.0.0.1 https://servers.dog/ | grep -i cache-control
# cache-control: public, max-age=60, s-maxage=300
```

Now continue with CLOUDFLARE.md step 5.

## Routine deploys

```sh
DEPLOY_HOST=you@<droplet-ip> deploy/deploy.sh
```

Asset URLs carry a content hash (`?v=`), so a deploy needs no Cloudflare purge. The new
`index.html` reaches visitors within 5 minutes (`s-maxage=300`). To roll back:

```sh
sudo install -m 0755 /usr/local/bin/serverdog.prev /usr/local/bin/serverdog
sudo systemctl restart serverdog
```

## Header checks

After CLOUDFLARE.md step 8, from any machine. Run each request twice: the second should be a
`HIT`.

```sh
curl -sI https://servers.dog/ | grep -iE 'cache-control|cf-cache-status'
curl -sI https://servers.dog/data/servers.json | grep -iE 'cache-control|cf-cache-status|age'
css=$(curl -s https://servers.dog/ | grep -o '/css/app\.css?v=[^"]*' | head -1)
curl -sI "https://servers.dog$css" | grep -iE 'cache-control|cf-cache-status'
curl -sI https://servers.dog/healthz | grep -iE 'cache-control|cf-cache-status'
```

| URL | `Cache-Control` | `cf-cache-status` on repeat |
|---|---|---|
| `/` | `public, max-age=60, s-maxage=300` | `HIT` |
| `/data/servers.json` | `public, max-age=N, s-maxage=N`, N = 5…refresh interval (seconds to the next update) | `HIT` until N runs out, then one `EXPIRED`/`MISS` |
| `/css/app.css?v=<hash>` | `public, max-age=31536000, immutable` | `HIT` |
| `/healthz` | `no-store` | `BYPASS` or `DYNAMIC` (never cached, on purpose: it must report the droplet as it is now) |

If `/` or `/data/servers.json` never shows `HIT`, the Cache Rule (CLOUDFLARE.md step 8) is
missing or doesn't match.

Check memory (SPEC §9: under 64 MB RSS):

```sh
ps -o rss=,cmd= -C serverdog   # RSS in KB
```
