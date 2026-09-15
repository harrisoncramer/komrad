#!/usr/bin/env bash
set -euo pipefail

install -d -o weave -g weave /opt/mise

sudo -u weave -H env MISE_INSTALL_PATH=/opt/mise/mise bash -c 'curl -fsSL https://mise.run | sh'
ln -sf /opt/mise/mise /usr/local/bin/mise

cat >/etc/profile.d/mise.sh <<'PROFILE'
eval "$(/usr/local/bin/mise activate bash)"
PROFILE

sudo -u weave -H /usr/local/bin/mise use -g go@1.25.1 node@22
