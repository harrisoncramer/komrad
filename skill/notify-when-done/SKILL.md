---
name: notify-when-done
description: Alert the user in tmux when a task finishes. Run the plugin's claude-notify with a one-line summary as the final action after completing any non-trivial task, so the tmux status bar and the prefix+g agent panel show a custom "done" message. Load when finishing work the user is waiting on, or when the user asks to be notified/pinged/alerted when done.
---

# Notify when done

The user runs multiple Claude sessions across tmux sessions and steps away while they work. This skill lets a finishing agent raise a passive alert they can find later, via the tmux-claude-agents plugin.

## When to fire

Run the notify command as your last action when you finish a task the user was waiting on: a build, a fix, a PR, a review, a long or background job. Skip it for quick conversational replies where the answer is already on screen.

## How to fire

Run once, at the end:

```
$HOME/.tmux/plugins/tmux-claude-agents/scripts/claude-notify "one line describing what finished"
```

Keep the message to a single line, concrete and specific, for example "Fixed the auth middleware token check, tests pass" rather than "done". The message is what the user reads in the panel. It is fire-and-forget: do not block, poll, or wait for a response.

## What it does

The plugin's Stop hook already marks your session done at the end of every turn. Calling claude-notify replaces that with your custom message so the user sees what finished, not just that something did. The done state clears on its own the next time you receive a prompt.

## How the user responds

The user presses `prefix + g` to open a panel of agents, picks yours, and chooses jump, chat, or send message. If they send you a message, you receive it as normal input. Nothing special is required on your side.
