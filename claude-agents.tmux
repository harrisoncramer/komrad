#!/usr/bin/env bash
# TPM entrypoint. Binds the agent panel and interpolates the status placeholder.
set -euo pipefail

CURRENT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPTS="$CURRENT_DIR/scripts"

get() { tmux show-option -gqv "$1"; }

# Configurable prefix key for the panel (default: g).
key="$(get '@claude_agents_key')"
[ -z "$key" ] && key="g"

tmux bind-key "$key" display-popup -w 45 -h 12 -x C -y C \
  -E "$SCRIPTS/claude-panel #{client_name}"

# Interpolate the #{claude_agents} placeholder in status-left / status-right
# with the live aggregate, so the plugin never clobbers the user's status line.
placeholder='#{claude_agents}'
render="#($SCRIPTS/claude-status-render)"
for opt in status-right status-left; do
  val="$(tmux show-option -gqv "$opt")"
  case "$val" in
    *"$placeholder"*)
      tmux set-option -g "$opt" "${val//$placeholder/$render}"
      ;;
  esac
done
