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

# The manager lives in a persistent tmux session shown through a popup. Pressing
# the key while the popup is up hides it by detaching that session's client,
# which leaves the panel running so reopening restores the exact view. Pressing
# it while hidden opens the popup, attaching to the persistent session. The open
# command owns the @claude_manager_open flag: it sets it while the popup is up
# and clears it when the popup closes.
tmux set-option -g @claude_manager_open 0
tmux bind-key "$key" if-shell -F '#{==:#{@claude_manager_open},1}' \
  "detach-client -s claude-manager" \
  "display-popup -w 95% -h 95% -x C -y C -T ' claude-manager ' -E '$BIN open #{client_name}'"

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
