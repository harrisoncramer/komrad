#!/usr/bin/env bash
# TPM entrypoint. Binds the agent panel and interpolates the status placeholder.
set -euo pipefail

CURRENT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPTS="$CURRENT_DIR/scripts"

get() { tmux show-option -gqv "$1"; }

# Configurable prefix key for the panel (default: g).
key="$(get '@claude_agents_key')"
[ -z "$key" ] && key="g"

# Implementation: gum (bash, default) or bubbletea (Go binary). Toggle with
#   set -g @claude_agents_impl 'bubbletea'
impl="$(get '@claude_agents_impl')"
[ -z "$impl" ] && impl="gum"

panel="$SCRIPTS/claude-panel"
w="45"; h="12"
if [ "$impl" = "bubbletea" ]; then
  bin="$CURRENT_DIR/bin/claude-panel"
  if [ ! -x "$bin" ] && command -v go >/dev/null 2>&1; then
    ( cd "$CURRENT_DIR/go" && go build -o "$bin" . ) >/dev/null 2>&1 || true
  fi
  if [ -x "$bin" ]; then
    panel="$bin"
    w="60"; h="18"
  fi
fi

tmux bind-key "$key" display-popup -w "$w" -h "$h" -x C -y C \
  -E "$panel #{client_name}"

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
