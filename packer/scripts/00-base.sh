#!/usr/bin/env bash
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive

apt-get update
apt-get upgrade -y
apt-get install -y \
	build-essential \
	ca-certificates \
	curl \
	git \
	gnupg \
	jq \
	postgresql-client \
	ripgrep \
	tmux \
	unzip

useradd --create-home --shell /bin/bash weave

install -m 0755 /tmp/files/weave-data-volume /usr/local/sbin/weave-data-volume
install -m 0644 /tmp/files/weave-data-volume.service /etc/systemd/system/weave-data-volume.service
install -m 0644 /tmp/files/weave.service /etc/systemd/system/weave.service
mkdir -p /etc/weave
install -d -o weave -g weave /home/weave/.config /home/weave/.config/weave

install -m 0644 /tmp/files/weave.env /etc/weave/weave.env
install -m 0644 -o weave -g weave /tmp/files/weave-config.toml /home/weave/.config/weave/config.toml

sed 's/^/export /' /tmp/files/weave.env >/etc/profile.d/weave-env.sh
chmod 0644 /etc/profile.d/weave-env.sh

systemctl enable weave-data-volume.service
systemctl enable weave.service
