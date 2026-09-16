#!/usr/bin/env bash
set -euo pipefail

sudo -u weave env HOME=/opt/mise/home MISE_DATA_DIR=/opt/mise/data MISE_CONFIG_DIR=/opt/mise/config \
	bash -c 'mise exec node@22 -- npm install -g @anthropic-ai/claude-code @openai/codex'

curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
	-o /etc/apt/keyrings/githubcli.gpg
chmod a+r /etc/apt/keyrings/githubcli.gpg
echo "deb [arch=arm64 signed-by=/etc/apt/keyrings/githubcli.gpg] https://cli.github.com/packages stable main" \
	>/etc/apt/sources.list.d/github-cli.list

export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y gh

install -m 0755 /tmp/files/weave-gh /usr/local/bin/gh
