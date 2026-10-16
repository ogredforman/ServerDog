#!/bin/sh
# Build ServerDog for the droplet, copy it over and restart the service.
#
#   DEPLOY_HOST=deploy@203.0.113.10 deploy/deploy.sh
#
# DEPLOY_HOST is the droplet's SSH target (its IP, not servers.dog, which resolves to
# Cloudflare). The remote user needs sudo; ssh -t lets sudo ask for a password. One-time
# setup of the user, config, unit and nginx site is in deploy/DEPLOY.md.
set -eu

: "${DEPLOY_HOST:?set DEPLOY_HOST to the droplet SSH target, e.g. deploy@203.0.113.10}"

cd "$(dirname "$0")/.."

version=$(git describe --tags --always --dirty)
out=bin/serverdog-linux-amd64

echo "building $version"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
	go build -trimpath -ldflags "-s -w -X main.version=$version" -o "$out" ./cmd/serverdog

echo "copying to $DEPLOY_HOST"
# Stage in the deploy user's home, not a shared /tmp path another user could swap.
scp "$out" "$DEPLOY_HOST:serverdog.new"

echo "installing and restarting"
ssh -t "$DEPLOY_HOST" '
	set -eu
	if [ -f /usr/local/bin/serverdog ]; then
		sudo cp -p /usr/local/bin/serverdog /usr/local/bin/serverdog.prev
	fi
	sudo install -o root -g root -m 0755 "$HOME/serverdog.new" /usr/local/bin/serverdog
	rm -f "$HOME/serverdog.new"
	sudo systemctl restart serverdog
	sleep 2
	sudo systemctl is-active serverdog
	curl -fsS http://127.0.0.1:8080/healthz; echo
'
echo "deployed $version"
