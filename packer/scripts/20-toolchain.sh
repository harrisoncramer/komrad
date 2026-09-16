#!/usr/bin/env bash
set -euo pipefail

install -d -o weave -g weave /opt/mise /opt/mise/home /opt/mise/data /opt/mise/config

sudo -u weave env HOME=/opt/mise/home MISE_INSTALL_PATH=/opt/mise/mise \
	bash -c 'curl -fsSL https://mise.run | sh'
ln -sf /opt/mise/mise /usr/local/bin/mise

cat >/etc/profile.d/mise.sh <<'PROFILE'
eval "$(/usr/local/bin/mise activate bash)"
PROFILE

sudo -u weave env HOME=/opt/mise/home MISE_DATA_DIR=/opt/mise/data MISE_CONFIG_DIR=/opt/mise/config \
	/usr/local/bin/mise use -g go@1.25.1 node@22
