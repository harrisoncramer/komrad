#!/usr/bin/env bash
# TPM entrypoint. Builds the claude-agents binary if needed, binds the panel,
# and interpolates the status placeholder.
set -euo pipefail

CURRENT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN="$CURRENT_DIR/bin/claude-agents"

if [ ! -x "$BIN" ] && command -v go >/dev/null 2>&1; then
  (cd "$CURRENT_DIR" && go build -o "$BIN" .) >/dev/null 2>&1 || true
fi

get() { tmux show-option -gqv "$1"; }

# Configurable prefix key for the panel (default: g).
key="$(get '@claude_agents_key')"
[ -z "$key" ] && key="g"

tmux bind-key "$key" display-popup -w 60 -h 18 -x C -y C -E "$BIN panel #{client_name}"

# Interpolate the #{claude_agents} placeholder in status-left / status-right
# with the live aggregate, so the plugin never clobbers the user's status line.
placeholder='#{claude_agents}'
render="#($BIN status)"
for opt in status-right status-left; do
  val="$(tmux show-option -gqv "$opt")"
  case "$val" in
    *"$placeholder"*)
      tmux set-option -g "$opt" "${val//$placeholder/$render}"
      ;;
  esac
done
