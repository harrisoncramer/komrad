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

useradd --no-create-home --home-dir /var/lib/komrade/home --shell /bin/bash komrade

install -m 0755 /tmp/files/komrade-data-volume /usr/local/sbin/komrade-data-volume
install -m 0755 /tmp/files/komrade-database-credentials /usr/local/sbin/komrade-database-credentials
install -m 0755 /tmp/files/komrade-github-key /usr/local/sbin/komrade-github-key
install -m 0755 /tmp/files/komrade-manager-session /usr/local/sbin/komrade-manager-session
install -m 0644 /tmp/files/komrade-data-volume.service /etc/systemd/system/komrade-data-volume.service
install -m 0644 /tmp/files/komrade-database-credentials.service /etc/systemd/system/komrade-database-credentials.service
install -m 0644 /tmp/files/komrade-database-credentials.timer /etc/systemd/system/komrade-database-credentials.timer
install -m 0644 /tmp/files/komrade-github-key.service /etc/systemd/system/komrade-github-key.service
install -m 0644 /tmp/files/komrade-manager-session.service /etc/systemd/system/komrade-manager-session.service
install -m 0644 /tmp/files/komrade.service /etc/systemd/system/komrade.service
mkdir -p /etc/komrade /usr/share/komrade

install -m 0644 /tmp/files/komrade.env /etc/komrade/komrade.env
install -m 0644 /tmp/files/komrade-config.toml /usr/share/komrade/config.toml

curl -fsSL -o /tmp/awscli.zip \
	https://awscli.amazonaws.com/awscli-exe-linux-aarch64.zip
unzip -q /tmp/awscli.zip -d /tmp
/tmp/aws/install
rm -rf /tmp/awscli.zip /tmp/aws

curl -fsSL -o /etc/komrade/rds-ca.pem \
	https://truststore.pki.rds.amazonaws.com/global/global-bundle.pem
chmod 0644 /etc/komrade/rds-ca.pem

# Git reaches github as the App rather than with a stored credential, so it needs no login on a
# machine nobody logs into.
git config --system credential.https://github.com.helper '!komrade github credential'
git config --system credential.https://github.com.username x-access-token

sed 's/^/export /' /tmp/files/komrade.env >/etc/profile.d/komrade-env.sh
chmod 0644 /etc/profile.d/komrade-env.sh

systemctl enable komrade-data-volume.service
systemctl enable komrade-database-credentials.service
systemctl enable komrade-database-credentials.timer
systemctl enable komrade-github-key.service
systemctl enable komrade-manager-session.service
systemctl enable komrade.service
