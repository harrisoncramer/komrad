# Commands

The binary is `claude-agents`. Running it with no subcommand opens the [panel](panel.md). The subcommands exist for the plugin and the Claude Code hooks to call, though you can run them by hand. This page says what each is for; for the exact usage line and every flag, see [reference/](reference/claude-agents.md) or run `claude-agents <command> --help`.

## serve

The `serve` command runs the ConnectRPC/HTTP server that owns the SQLite store. Everything else is a client that talks to it. You do not normally run this yourself: the panel spawns a detached `serve` process the first time it needs one and waits for it to become reachable.

## new

The `new` command creates a background agent session without the interactive picker. It takes a title, directory, extra flags, initial prompt, and the invoking tmux client as flags. This is what the picker's new-chat form calls once you fill it in, and it is the entry point to script agent creation.

## notify

The `notify` command records the calling agent as done with a one-line message. Have an agent run it as its final step so the panel shows a custom done message instead of the plain stopped state. The `notify-when-done` skill under `skill/` instructs an agent to do exactly this.

## status

The `status` command prints the status-bar aggregate, the single count across every session, for example `1 working  1 done`. The tmux status line calls it to render the `#{claude_agents}` placeholder.

## hook

The `hook` command advances an agent's state from a Claude Code lifecycle event. It takes one argument, `prompt` or `stop`. On `prompt` the agent is marked thinking and Claude's session id is recorded off the hook's stdin JSON; on `stop` the agent is marked done. These are wired into Claude Code's `UserPromptSubmit` and `Stop` hooks. See [installation](install.md) for the settings.
