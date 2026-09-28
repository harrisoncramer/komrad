#!/usr/bin/env bash
set -euo pipefail

install -d -o komrad -g komrad /opt/mise /opt/mise/home /opt/mise/data /opt/mise/config

sudo -u komrad env HOME=/opt/mise/home MISE_INSTALL_PATH=/opt/mise/mise \
	bash -c 'curl -fsSL https://mise.run | sh'
ln -sf /opt/mise/mise /usr/local/bin/mise

# activate only sets PATH from the prompt hook, so a shell that never prompts, such as the one
# a script or a hook runs in, finds no tool through it; the shims are what reach those.
cat >/etc/profile.d/mise.sh <<'PROFILE'
export PATH="/opt/mise/data/shims:$PATH"
eval "$(/usr/local/bin/mise activate bash)"
PROFILE

sudo -u komrad env HOME=/opt/mise/home MISE_DATA_DIR=/opt/mise/data MISE_CONFIG_DIR=/opt/mise/config \
	/usr/local/bin/mise use -g go@1.25.1 node@26
sudo -u komrad env HOME=/opt/mise/home MISE_DATA_DIR=/opt/mise/data MISE_CONFIG_DIR=/opt/mise/config \
	/usr/local/bin/mise settings add trusted_config_paths /var/lib/komrad
