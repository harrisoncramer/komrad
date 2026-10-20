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
	nftables \
	postgresql-client \
	ripgrep \
	tmux \
	unzip \
	vim

useradd --no-create-home --home-dir /var/lib/komrad/home --shell /bin/bash komrad

install -m 0755 /tmp/files/komrad-data-volume /usr/local/sbin/komrad-data-volume
install -m 0644 /tmp/files/komrad-data-volume.service /etc/systemd/system/komrad-data-volume.service
mkdir -p /etc/komrad /usr/share/komrad

install -m 0644 /tmp/files/komrad-config.toml /usr/share/komrad/config.toml

curl -fsSL -o /tmp/awscli.zip \
	https://awscli.amazonaws.com/awscli-exe-linux-aarch64.zip
unzip -q /tmp/awscli.zip -d /tmp
/tmp/aws/install
rm -rf /tmp/awscli.zip /tmp/aws

curl -fsSL -o /etc/komrad/rds-ca.pem \
	https://truststore.pki.rds.amazonaws.com/global/global-bundle.pem
chmod 0644 /etc/komrad/rds-ca.pem

systemctl enable nftables.service
systemctl enable komrad-data-volume.service
