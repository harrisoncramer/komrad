# Installation

Install is just the tmux plugin, which draws the status bar and the panel. Agent state is worked out by the tool itself, so there is nothing to wire into Claude.

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

Then press `prefix + I` to install. The panel key defaults to `g`, and the key that steps back out of a focused chat defaults to `Escape`. Change either with:

```
set -g @claude_agents_key 'g'
set -g @claude_agents_back_key 'Escape'
```

## Agent state

There is nothing to configure. The `serve` process watches every agent it launched and works out each one's state by reading its tmux pane, so state stays correct on its own. This only covers agents created through the tool. A Claude session you start yourself is never tracked.

## Notify (optional)

To mark an agent done from within a turn, have it run this as its final step:

```
$HOME/.tmux/plugins/tmux-claude-agents/scripts/claude-notify "one line describing what finished"
```

The `notify-when-done` skill under `skill/` instructs an agent to do this.
