#!/usr/bin/env bash
set -euo pipefail

curl -fsSL https://mise.run | MISE_VERSION=v2026.10.11 MISE_INSTALL_PATH=/usr/local/bin/mise sh

install -d /etc/mise
cat >/etc/mise/config.toml <<'CONFIG'
[settings]
trusted_config_paths = ["/var/lib/komrad"]
CONFIG

# activate only sets PATH from the prompt hook, so a shell that never prompts, such as the one
# a script or a hook runs in, finds no tool through it; the shims are what reach those.
cat >/etc/profile.d/mise.sh <<'PROFILE'
if [ "$(id -u)" -ne 0 ]; then
	export PATH="/var/lib/komrad/mise/shims:$PATH"
	eval "$(/usr/local/bin/mise activate bash)"
fi
PROFILE
