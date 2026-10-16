#!/usr/bin/env bash
set -euo pipefail

claude_version=2.1.300
claude_sha256=ae2295ae2da156936bf5831ab01971ce924d0c9a0af174e28d96fffa8d5fa0a6
codex_version=0.163.0
codex_sha256=9d8ecf2d19d3e184877983902d6c85de48b3888dcb72c382a8a0c975845927fb

curl -fsSL -o /tmp/claude \
	"https://downloads.claude.ai/claude-code-releases/$claude_version/linux-arm64/claude"
echo "$claude_sha256  /tmp/claude" | sha256sum -c -
install -m 0755 /tmp/claude /usr/local/bin/claude
rm -f /tmp/claude

curl -fsSL -o /tmp/codex.tar.gz \
	"https://github.com/openai/codex/releases/download/rust-v$codex_version/codex-package-aarch64-unknown-linux-musl.tar.gz"
echo "$codex_sha256  /tmp/codex.tar.gz" | sha256sum -c -
install -d /opt/codex
tar -xzf /tmp/codex.tar.gz -C /opt/codex
ln -sf /opt/codex/bin/codex /usr/local/bin/codex
install -m 0644 /tmp/files/opt.codex.codex-resources.bwrap /etc/apparmor.d/opt.codex.codex-resources.bwrap
rm -f /tmp/codex.tar.gz

curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
	-o /etc/apt/keyrings/githubcli.gpg
chmod a+r /etc/apt/keyrings/githubcli.gpg
echo "deb [arch=arm64 signed-by=/etc/apt/keyrings/githubcli.gpg] https://cli.github.com/packages stable main" \
	>/etc/apt/sources.list.d/github-cli.list

export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y gh
