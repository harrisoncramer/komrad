#!/usr/bin/env bash
# Maintains the Claude agent registry (rendered as a global tmux status summary)
# from Claude Code lifecycle hooks. Wire into Claude Code settings.json:
#   UserPromptSubmit -> claude-tmux-status.sh prompt
#   Stop            -> claude-tmux-status.sh stop
set -euo pipefail

mode="${1:-}"
cat >/dev/null 2>&1 || true   # drain the hook JSON payload on stdin

command -v tmux >/dev/null 2>&1 || exit 0
[ -n "${TMUX:-}" ] || exit 0

# Same keying as claude-notify: owning session for popup Claudes, else the
# session this pane lives in.
key="${MAIN_SESSION:-$(tmux display-message -p '#{session_name}' 2>/dev/null || echo)}"
[ -n "$key" ] || exit 0
pane="${TMUX_PANE:-}"
safe=$(printf '%s' "$key" | tr -c 'A-Za-z0-9._-' '_')
reg="${CLAUDE_AGENTS_DIR:-$HOME/.claude/agents}"
file="$reg/$safe"
mkdir -p "$reg"

field() { sed -n "s/^$1=//p" "$file" 2>/dev/null | head -1; }

case "$mode" in
  prompt)
    {
      printf 'SESSION=%s\n' "$key"
      printf 'PANE=%s\n' "$pane"
      printf 'STATE=%s\n' "thinking"
      printf 'MSG=%s\n' ""
    } > "$file"
    ;;
  stop)
    # If claude-notify already set STATE=done with a message this turn, keep it.
    # Otherwise the turn just finished: mark done with no message.
    if ! { [ -f "$file" ] && [ "$(field STATE)" = "done" ]; }; then
      {
        printf 'SESSION=%s\n' "$key"
        printf 'PANE=%s\n' "$pane"
        printf 'STATE=%s\n' "done"
        printf 'MSG=%s\n' ""
      } > "$file"
    fi
    ;;
esac
