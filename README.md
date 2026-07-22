# tmux-claude-agents

A tmux panel for background Claude Code agents running across your tmux sessions.

Each Claude session reports its state (working, done) into a small registry. tmux shows a live global summary in the status bar, and a popup panel (`prefix + g`) lets you pick any agent and jump to it, open its live chat in a large popup, or send it a message. You step away, and you can see and reach every agent from anywhere.

## What it does

- Status bar shows a global count across all sessions, for example `1 working  1 done`.
- `prefix + g` opens a small centered popup listing every live agent with its state and its last "done" message.
- Pick an agent, then pick an action:
  - jump: switch to that agent's session and pane
  - chat: open the agent's live session in a large 90% popup so you can talk to it without leaving your current view (close it with `prefix + d` to detach)

## How state is tracked

State lives in a registry directory, `~/.claude/agents` by default (override with `CLAUDE_AGENTS_DIR`). Two Claude Code lifecycle hooks maintain it:

- On each prompt, the agent is marked `thinking` (counts as working).
- On each stop, the agent is marked `done`. If the agent called `claude-notify` during the turn, its custom message is kept.

The done state persists until you send that agent a new prompt, so a finished agent keeps showing until you come back to it.

## Install (tmux side)

With TPM, add to `~/.tmux.conf`:

```
set -g @plugin 'git@github.com:harrisoncramer/tmux-claude-agents'
```

(SSH form because the repo is private; use `harrisoncramer/tmux-claude-agents` if it becomes public.)

Put the status placeholder wherever you want the summary to appear, for example:

```
set -g status-right "#{claude_agents}%I:%M:%S "
```

Then `prefix + I` to install. The panel key defaults to `g`; change it with:

```
set -g @claude_agents_key 'g'
```

## Install (Claude Code side)

The status hooks and the notify command ship in `scripts/`. Wire the hooks in Claude Code `settings.json`, pointing at the plugin path:

```
"hooks": {
  "UserPromptSubmit": [
    { "matcher": "", "hooks": [ { "type": "command", "command": "$HOME/.tmux/plugins/tmux-claude-agents/scripts/claude-tmux-status.sh prompt" } ] }
  ],
  "Stop": [
    { "matcher": "", "hooks": [ { "type": "command", "command": "$HOME/.tmux/plugins/tmux-claude-agents/scripts/claude-tmux-status.sh stop" } ] }
  ]
}
```

To let an agent attach a custom "done" message, have it run as its final step:

```
$HOME/.tmux/plugins/tmux-claude-agents/scripts/claude-notify "one line describing what finished"
```

A Claude Code skill that instructs the agent to do this is a good companion; see the plugin's skill example.

## Requirements

- tmux
- [gum](https://github.com/charmbracelet/gum) for the panel prompts
