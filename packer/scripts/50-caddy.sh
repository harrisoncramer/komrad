#!/usr/bin/env bash
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive

install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://dl.cloudsmith.io/public/caddy/stable/gpg.key | gpg --dearmor -o /etc/apt/keyrings/caddy.gpg
chmod a+r /etc/apt/keyrings/caddy.gpg

echo "deb [signed-by=/etc/apt/keyrings/caddy.gpg] https://dl.cloudsmith.io/public/caddy/stable/deb/debian any-version main" \
	>/etc/apt/sources.list.d/caddy.list

apt-get update
apt-get install -y caddy

install -m 0755 -d /etc/systemd/system/caddy.service.d
install -m 0644 /tmp/files/caddy-komrad.conf /etc/systemd/system/caddy.service.d/komrad.conf

systemctl disable --now caddy
