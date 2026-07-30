#!/usr/bin/env bash
# TPM entrypoint. Builds the weave binary if needed and binds the panel.
set -euo pipefail

CURRENT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN="$CURRENT_DIR/bin/weave"

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
#
# That session lives on a tmux server of its own, so it stays out of this server's
# session list and cannot be attached to outside the popup. Hiding therefore goes
# through the binary, which knows that server's socket, and open is handed the
# socket path of this one so the panel can reach back for the working directory of
# whoever opened it.
#
# -EE, rather than -E, closes the popup only when the command succeeds. Detaching
# to hide the panel is a success, so that still closes it, but a startup failure
# leaves the popup up with its error on screen instead of destroying the popup in
# the same instant the message is written. Dismiss a held-open popup with Escape
# or C-c.
tmux set-option -g @claude_manager_open 0
tmux bind-key "$key" if-shell -F '#{==:#{@claude_manager_open},1}' \
	"run-shell -b '$BIN hide'" \
	"display-popup -w 95% -h 95% -x C -y C -T ' claude-manager ' -EE '$BIN open #{client_name} #{socket_path}'"
