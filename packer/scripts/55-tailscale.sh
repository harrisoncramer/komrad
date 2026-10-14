#!/usr/bin/env bash
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive

install -m 0755 -d /etc/systemd/system/tailscaled.service.d
install -m 0644 /tmp/files/tailscaled-komrad.conf /etc/systemd/system/tailscaled.service.d/komrad.conf
install -m 0755 /tmp/files/komrad-tailscale /usr/local/sbin/komrad-tailscale
install -m 0644 /tmp/files/komrad-tailscale.service /etc/systemd/system/komrad-tailscale.service

curl -fsSL -o /usr/share/keyrings/tailscale-archive-keyring.gpg \
	https://pkgs.tailscale.com/stable/ubuntu/noble.noarmor.gpg
curl -fsSL -o /etc/apt/sources.list.d/tailscale.list \
	https://pkgs.tailscale.com/stable/ubuntu/noble.tailscale-keyring.list

apt-get update
apt-get install -y tailscale

systemctl stop tailscaled
rm -rf /var/lib/tailscale

systemctl enable tailscaled.service
systemctl enable komrad-tailscale.service
