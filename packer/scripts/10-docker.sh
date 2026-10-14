#!/usr/bin/env bash
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive

install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
chmod a+r /etc/apt/keyrings/docker.asc

# shellcheck source=/dev/null
codename="$(. /etc/os-release && echo "$VERSION_CODENAME")"
echo "deb [arch=arm64 signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu $codename stable" \
	>/etc/apt/sources.list.d/docker.list

apt-get update
apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin \
	docker-ce-rootless-extras slirp4netns uidmap

install -m 0644 /tmp/files/usr.bin.rootlesskit /etc/apparmor.d/usr.bin.rootlesskit
install -m 0644 /tmp/files/komrad-docker.service /etc/systemd/system/komrad-docker.service

systemctl disable --now docker.service docker.socket
systemctl enable komrad-docker.service
