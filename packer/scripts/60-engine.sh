#!/usr/bin/env bash
set -euo pipefail

install -m 0755 /tmp/files/komrad-engine-credentials /usr/local/sbin/komrad-engine-credentials
install -m 0755 /tmp/files/komrad-manager-session /usr/local/sbin/komrad-manager-session
install -m 0644 /tmp/files/komrad-engine-credentials.service /etc/systemd/system/komrad-engine-credentials.service
install -m 0644 /tmp/files/komrad-manager-session.service /etc/systemd/system/komrad-manager-session.service
install -m 0644 /tmp/files/komrad.service /etc/systemd/system/komrad.service
install -m 0644 /tmp/files/nftables-engine.conf /etc/nftables.conf
install -m 0644 /tmp/files/komrad.env /etc/komrad/komrad.env

# User data names the control plane here, and the firewall lets only it reach the server.
install -d -m 0755 /etc/komrad/nftables
: >/etc/komrad/nftables/control-plane.nft

sed 's/^/export /' /tmp/files/komrad.env >/etc/profile.d/komrad-env.sh
chmod 0644 /etc/profile.d/komrad-env.sh

# Git sends every GitHub request through the forge proxy, which swaps the placeholder for the
# person's login, and reaches an ssh remote on GitHub over https.
git config --system http.https://github.com/.sslCAInfo /etc/komrad/forge-proxy-ca.pem
git config --system url.https://github.com/.insteadOf git@github.com:
git config --system --add url.https://github.com/.insteadOf ssh://git@github.com/
git config --system credential.https://github.com.username x-access-token
# shellcheck disable=SC2016
git config --system credential.https://github.com.helper \
	'!f() { if [ "$1" = get ]; then echo password=komrad-forge-proxy; fi; }; f'
git config --system include.path /etc/komrad/git-proxy.gitconfig
git config --system --add include.path /var/lib/komrad/home/.local/state/komrad/git/identity

# Git reaches gitlab.com with the token its forge connection holds.
git config --system credential.https://gitlab.com.helper '!komrad gitlab credential'
git config --system credential.https://gitlab.com.username oauth2

systemctl enable komrad-engine-credentials.service
systemctl enable komrad-manager-session.service
systemctl enable komrad.service
