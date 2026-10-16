# Putting servers.dog and redforman.dev behind Cloudflare

A step-by-step tutorial for the shared droplet. When you're done:

- Both domains use Cloudflare's nameservers, and their websites are proxied (orange cloud).
- Visitors connect to Cloudflare. Cloudflare connects to Apache over HTTPS using a Cloudflare
  Origin Certificate, with SSL mode "Full (strict)".
- The droplet accepts web traffic (ports 80/443) only from Cloudflare. SSH is unchanged.
- servers.dog is cached by Cloudflare according to ServerDog's own `Cache-Control` headers.
  redforman.dev gets Cloudflare's default caching, which caches static files and never HTML.

redforman.dev needs **no changes to the portfolio app**. Everything happens in DNS, the
Cloudflare dashboard and Apache. Its notes are in step 6.

Everything here works on Cloudflare's Free plan. Menu names are from the Cloudflare dashboard
as of 2026; if one has moved, the setting name is the same.

## 1. Write down your current DNS records

Before anything moves, open each domain's current DNS (usually at the registrar) and note
**every** record. That matters most for anything that isn't the website:

- `MX` records and mail-related `TXT` records (SPF `v=spf1…`, DKIM, DMARC). Losing these
  breaks email for the domain.
- Verification `TXT` records (Google Search Console, etc.).
- Any subdomains (`www`, `mail`, `api`, …).

Cloudflare imports records automatically in the next step, but the import misses records
sometimes. This list is how you check it.

## 2. Add both domains to Cloudflare

For each of `servers.dog` and `redforman.dev`:

1. Sign up or log in at dash.cloudflare.com → **Add a domain** (also labeled "Add a site").
2. Enter the domain and leave **Quick scan for DNS records** on. Choose the **Free** plan.
3. Review the imported DNS records against your list from step 1. Add anything missing.
4. Set the proxy status for each record:

   | Record | Proxy status |
   |---|---|
   | `A  @  <droplet IP>` (and `AAAA` if the droplet has IPv6) | **Proxied** (orange cloud) |
   | `www` (`CNAME` to the apex, or `A` to the droplet) | **Proxied** |
   | `MX`, and any `A` record mail uses (e.g. `mail`) | **DNS only** (grey cloud). Mail can't go through the proxy |
   | Anything you SSH to by name | **DNS only**, or just SSH to the IP |

   servers.dog is new, so it probably has no records yet. Add `A @ <droplet IP>` (proxied) and
   `CNAME www servers.dog` (proxied).
5. Cloudflare then shows **two nameservers** (like `ada.ns.cloudflare.com`). Keep that page
   open; step 4 uses them. Don't change nameservers yet.

## 3. Create the Origin Certificates

Do this before switching nameservers, so Apache is ready when traffic arrives.

For each domain: **SSL/TLS → Origin Server → Create Certificate**.

Origin Certificates belong to one zone. Open the domain's own zone first (pick it on the account
home page), then go to SSL/TLS. Its hostnames must be in that zone: from the servers.dog zone you
can't create a `redforman.dev` certificate. If Cloudflare says the zone "is not part of your
account", you're in the wrong zone, or the domain hasn't been added yet (step 2). redforman.dev's
certificate can also wait until after step 4, because it keeps its Let's Encrypt certificate
until then (step 6).

- Key type: **RSA (2048)**.
- Hostnames: `servers.dog, *.servers.dog` (or `redforman.dev, *.redforman.dev`).
- Validity: **15 years**.
- Click **Create**. Copy the **Origin Certificate** and the **Private Key** right away. The key
  is shown only once.

Save them on the droplet:

```sh
sudo mkdir -p /etc/ssl/cloudflare
sudo nano /etc/ssl/cloudflare/<domain>.pem   # paste the certificate
sudo nano /etc/ssl/cloudflare/<domain>.key   # paste the private key
sudo chmod 0644 /etc/ssl/cloudflare/<domain>.pem
sudo chmod 0600 /etc/ssl/cloudflare/<domain>.key
```

An Origin Certificate is trusted only by Cloudflare, not by browsers. That's fine, because
browsers only ever talk to Cloudflare.

- **servers.dog:** now install ServerDog and its Apache site (DEPLOY.md steps 1–5). They
  already point at `/etc/ssl/cloudflare/servers.dog.pem` and `.key`.
- **redforman.dev:** see step 6. You can keep its existing Let's Encrypt certificate for now.

## 4. Switch the nameservers

At each domain's registrar:

1. If **DNSSEC** is on at the registrar, turn it off first and wait for the DS record to
   disappear (it can take a few hours). Switching nameservers with an old DS record in place
   makes the domain unreachable.
2. Replace the registrar's nameservers with the two Cloudflare gave you in step 2.
3. Back in Cloudflare, click **Check nameservers**. The domain turns **Active**, usually within
   an hour (up to 24h). Cloudflare emails you when it does.

When it's Active, turn DNSSEC back on through Cloudflare: **DNS → Settings → Enable DNSSEC**,
then add the DS record it shows at your registrar.

The site keeps working throughout. Visitors reach the droplet either directly (old
nameservers) or through Cloudflare (new ones), and both paths end at the same Apache.

## 5. SSL/TLS settings (both domains)

- **SSL/TLS → Overview → Configure:** set the encryption mode to **Full (strict)**. Cloudflare
  then connects to Apache over HTTPS and checks the origin certificate. Never use "Flexible":
  it sends traffic to the droplet unencrypted.
- **SSL/TLS → Edge Certificates:**
  - **Always Use HTTPS**: On.
  - **Minimum TLS Version**: TLS 1.2.
  - **Automatic HTTPS Rewrites**: On.
  - **HSTS**: optional. Only turn it on once HTTPS works everywhere on the domain, because
    browsers remember it for the max-age you set.

Full (strict) works with the Origin Certificate, and also with a valid Let's Encrypt
certificate (which redforman.dev may still be using).

## 6. redforman.dev notes

The portfolio app doesn't need to change. Things to know:

- **Certificate.** Its current Let's Encrypt certificate keeps working under Full (strict), and
  certbot renewals still work through Cloudflare. To drop certbot, point redforman.dev's Apache
  site at the Origin Certificate from step 3:

  ```apache
  SSLCertificateFile    /etc/ssl/cloudflare/redforman.dev.pem
  SSLCertificateKeyFile /etc/ssl/cloudflare/redforman.dev.key
  ```

  Remove any `Include /etc/letsencrypt/options-ssl-apache.conf` line, or keep it for its cipher
  settings. Reload Apache, then `sudo certbot delete --cert-name redforman.dev`. Only do this
  after the domain is Active on Cloudflare.
- **Visitor IPs.** Behind Cloudflare, Apache sees Cloudflare's address for every request. Step 7
  sets up `mod_remoteip` so Apache's logs, and the `X-Forwarded-For` it passes to a proxied
  app, carry the real visitor address. The app needs no change.
- **Caching.** With no Cache Rule, Cloudflare caches only static file types (images, CSS, JS,
  fonts) and never HTML, so pages, forms and logins behave as before. If you redeploy the
  portfolio and changed CSS/JS keeps the same file name, purge the cache:
  **Caching → Configuration → Purge Everything**.
- **No servers.dog Cache Rule here.** Step 8's rule is for servers.dog only.

## 7. Firewall: only Cloudflare reaches ports 80/443

Once **both** domains are Active and proxied, nobody needs to reach the droplet's web ports
directly. Limiting them to Cloudflare stops anyone from bypassing the cache and hitting Apache
or ServerDog head-on.

On the droplet, as root (`sudo -i`):

```sh
# 1. Keep SSH open first, so the next steps can't lock you out. ufw denies every other
#    incoming port once enabled: list what listens publicly and allow anything else you use
#    (mail, etc.). Services bound to 127.0.0.1, like ServerDog, need no rule.
ufw allow OpenSSH
ss -tlnp
grep IPV6 /etc/default/ufw      # must be IPV6=yes, or the Cloudflare IPv6 rules do nothing

# 2. Allow 80/443 from Cloudflare's ranges, and write Apache's real-IP config.
sh cloudflare-ips.sh            # from deploy/; copy it over first
a2enmod remoteip
a2enconf cloudflare-remoteip

# 3. Remove the old "anyone can reach 80/443" rules.
ufw status numbered             # find rules like "Apache Full", "80/tcp", "443/tcp" from Anywhere
ufw delete <number>             # one at a time, highest number first; re-run status between deletes

# 4. Turn the firewall on (if it isn't) and check.
ufw enable
ufw status verbose
```

Then make Apache's logs show the visitor address. In `/etc/apache2/apache2.conf`, change `%h`
to `%a` at the start of the `combined` and `common` `LogFormat` lines, then:

```sh
apache2ctl configtest && systemctl reload apache2
```

Check that both sites still load in a browser. `curl -I http://<droplet IP>` from your machine
should now time out. That's the point.

Cloudflare rarely changes its ranges. To pick up new ones, re-run `cloudflare-ips.sh` and
`systemctl reload apache2`.

If you also use a DigitalOcean Cloud Firewall on the droplet, it must allow 80/443 too (from
anywhere or from the Cloudflare ranges), or it blocks traffic before ufw sees it.

## 8. Caching for servers.dog

Cloudflare doesn't cache HTML or JSON by default. ServerDog already says exactly how long each
response may be cached, so let Cloudflare follow those headers.

**Caching → Cache Rules → Create rule** (in the servers.dog zone):

- Rule name: `ServerDog: cache by origin headers`
- When incoming requests match: **Hostname** *equals* `servers.dog`
- Then:
  - Cache eligibility: **Eligible for cache**
  - Edge TTL: **Use cache-control header if present, bypass cache if not**
  - Browser TTL: **Respect origin TTL**
- Deploy.

This covers `/`, `/data/servers.json` and the assets. `/healthz` sends `no-store`, so Cloudflare
never caches it.

**Caching → Tiered Cache:** turn on **Smart Tiered Caching**. Without it, each Cloudflare
location fetches `servers.json` from the droplet separately. With it, locations fetch through
one upper tier, so the droplet sees about one request per update in total.

**Turn off anything that injects scripts into the page.** ServerDog promises visitors that the
page loads nothing from third parties (SPEC §5, §9). In the servers.dog zone, check that these
are **off**:

- **Speed → Optimization → Rocket Loader**
- **Analytics & Logs → Web Analytics** (automatic setup adds a `cloudflareinsights.com` beacon)
- **Scrape Shield → Email Address Obfuscation** (rewrites the footer's mailto link and adds a script)
- **Security → Bots → Bot Fight Mode** (can serve JavaScript challenges to visitors)
- **Zaraz**: no tools configured

Cloudflare's dashboard analytics (requests, cache hit ratio) are collected at the edge and need
none of these.

## 9. Verify

- Run the `curl -I` checks in DEPLOY.md "Header checks". The second request to `/` and
  `/data/servers.json` should say `cf-cache-status: HIT`.
- Open https://servers.dog and https://redforman.dev. The browser shows a Cloudflare-issued
  certificate (padlock → certificate details).
- **Caching → Overview** (or the zone's Analytics) shows the cached-request share climbing
  over the first day.
- On the droplet, `tail -f /var/log/apache2/servers.dog-access.log` shows visitor IPs (not
  Cloudflare's), and only a trickle of requests compared with the traffic Cloudflare reports.
