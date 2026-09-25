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

useradd --no-create-home --home-dir /var/lib/komrad/home --shell /bin/bash komrad

install -m 0755 /tmp/files/komrad-data-volume /usr/local/sbin/komrad-data-volume
install -m 0755 /tmp/files/komrad-database-credentials /usr/local/sbin/komrad-database-credentials
install -m 0755 /tmp/files/komrad-github-key /usr/local/sbin/komrad-github-key
install -m 0755 /tmp/files/komrad-manager-session /usr/local/sbin/komrad-manager-session
install -m 0644 /tmp/files/komrad-data-volume.service /etc/systemd/system/komrad-data-volume.service
install -m 0644 /tmp/files/komrad-database-credentials.service /etc/systemd/system/komrad-database-credentials.service
install -m 0644 /tmp/files/komrad-database-credentials.timer /etc/systemd/system/komrad-database-credentials.timer
install -m 0644 /tmp/files/komrad-github-key.service /etc/systemd/system/komrad-github-key.service
install -m 0644 /tmp/files/komrad-manager-session.service /etc/systemd/system/komrad-manager-session.service
install -m 0644 /tmp/files/komrad.service /etc/systemd/system/komrad.service
mkdir -p /etc/komrad /usr/share/komrad

install -m 0644 /tmp/files/komrad.env /etc/komrad/komrad.env
install -m 0644 /tmp/files/komrad-config.toml /usr/share/komrad/config.toml

curl -fsSL -o /tmp/awscli.zip \
	https://awscli.amazonaws.com/awscli-exe-linux-aarch64.zip
unzip -q /tmp/awscli.zip -d /tmp
/tmp/aws/install
rm -rf /tmp/awscli.zip /tmp/aws

curl -fsSL -o /etc/komrad/rds-ca.pem \
	https://truststore.pki.rds.amazonaws.com/global/global-bundle.pem
chmod 0644 /etc/komrad/rds-ca.pem

# Git reaches github as the App rather than with a stored credential, so it needs no login on a
# machine nobody logs into.
git config --system credential.https://github.com.helper '!komrad github credential'
git config --system credential.https://github.com.username x-access-token

sed 's/^/export /' /tmp/files/komrad.env >/etc/profile.d/komrad-env.sh
chmod 0644 /etc/profile.d/komrad-env.sh

systemctl enable komrad-data-volume.service
systemctl enable komrad-database-credentials.service
systemctl enable komrad-database-credentials.timer
systemctl enable komrad-github-key.service
systemctl enable komrad-manager-session.service
systemctl enable komrad.service
