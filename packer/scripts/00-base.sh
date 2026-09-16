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

useradd --no-create-home --home-dir /var/lib/weave/home --shell /bin/bash weave

install -m 0755 /tmp/files/weave-data-volume /usr/local/sbin/weave-data-volume
install -m 0755 /tmp/files/weave-database-credentials /usr/local/sbin/weave-database-credentials
install -m 0755 /tmp/files/weave-github-key /usr/local/sbin/weave-github-key
install -m 0755 /tmp/files/weave-manager-session /usr/local/sbin/weave-manager-session
install -m 0644 /tmp/files/weave-data-volume.service /etc/systemd/system/weave-data-volume.service
install -m 0644 /tmp/files/weave-database-credentials.service /etc/systemd/system/weave-database-credentials.service
install -m 0644 /tmp/files/weave-database-credentials.timer /etc/systemd/system/weave-database-credentials.timer
install -m 0644 /tmp/files/weave-github-key.service /etc/systemd/system/weave-github-key.service
install -m 0644 /tmp/files/weave-manager-session.service /etc/systemd/system/weave-manager-session.service
install -m 0644 /tmp/files/weave.service /etc/systemd/system/weave.service
mkdir -p /etc/weave /usr/share/weave

install -m 0644 /tmp/files/weave.env /etc/weave/weave.env
install -m 0644 /tmp/files/weave-config.toml /usr/share/weave/config.toml

curl -fsSL -o /tmp/awscli.zip \
	https://awscli.amazonaws.com/awscli-exe-linux-aarch64.zip
unzip -q /tmp/awscli.zip -d /tmp
/tmp/aws/install
rm -rf /tmp/awscli.zip /tmp/aws

curl -fsSL -o /etc/weave/rds-ca.pem \
	https://truststore.pki.rds.amazonaws.com/global/global-bundle.pem
chmod 0644 /etc/weave/rds-ca.pem

# Git reaches github as the App rather than with a stored credential, so it needs no login on a
# machine nobody logs into.
git config --system credential.https://github.com.helper '!weave github credential'
git config --system credential.https://github.com.username x-access-token

sed 's/^/export /' /tmp/files/weave.env >/etc/profile.d/weave-env.sh
chmod 0644 /etc/profile.d/weave-env.sh

systemctl enable weave-data-volume.service
systemctl enable weave-database-credentials.service
systemctl enable weave-database-credentials.timer
systemctl enable weave-github-key.service
systemctl enable weave-manager-session.service
systemctl enable weave.service
