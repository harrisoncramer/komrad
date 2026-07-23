# Installation

There are two sides to install: the tmux plugin, which draws the status bar and the panel, and the Claude Code hooks, which report each agent's state.

## Requirements

- tmux
- Go, to build the binary. The plugin builds it on first load.

## tmux side

With TPM, add the plugin to `~/.tmux.conf`:

```
set -g @plugin 'git@github.com:harrisoncramer/tmux-claude-agents'
```

The SSH form is used because the repo is private. Use `harrisoncramer/tmux-claude-agents` if it becomes public.

Put the status placeholder wherever you want the summary to show, for example:

```
set -g status-right "#{claude_agents}%I:%M:%S "
```

Then press `prefix + I` to install. The panel key defaults to `g`; change it with:

```
set -g @claude_agents_key 'g'
```

## Claude Code side

The status hooks and the notify command ship in `scripts/`. Wire the hooks in Claude Code `settings.json`, pointing at the plugin path:

```
"hooks": {
  "UserPromptSubmit": [
    { "matcher": "", "hooks": [ { "type": "command", "command": "$HOME/.tmux/plugins/tmux-claude-agents/scripts/claude-tmux-status.sh prompt" } ] }
  ],
  "Notification": [
    { "matcher": "", "hooks": [ { "type": "command", "command": "$HOME/.tmux/plugins/tmux-claude-agents/scripts/claude-tmux-status.sh notification" } ] }
  ],
  "PostToolUse": [
    { "matcher": "", "hooks": [ { "type": "command", "command": "$HOME/.tmux/plugins/tmux-claude-agents/scripts/claude-tmux-status.sh posttooluse" } ] }
  ],
  "Stop": [
    { "matcher": "", "hooks": [ { "type": "command", "command": "$HOME/.tmux/plugins/tmux-claude-agents/scripts/claude-tmux-status.sh stop" } ] }
  ]
}
```

To mark an agent done from within a turn, have it run this as its final step:

```
$HOME/.tmux/plugins/tmux-claude-agents/scripts/claude-notify "one line describing what finished"
```

The `notify-when-done` skill under `skill/` instructs an agent to do this.
