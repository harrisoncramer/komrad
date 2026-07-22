# The panel

The panel is where you browse and reach your agents. Open it with `prefix + g`. It comes up as a `claude-manager` popup, a 95% centered popup that persists as a single space: opening an agent attaches to it inside the same popup, and detaching drops you back where you were. The popup closes only when you quit the picker.

## The picker

The picker is the first screen. It has two rows.

The `new chat` row prompts you for a title, a directory, extra flags, and an opening prompt, then launches a background Claude session with them.

The `chats (N)` row opens the browser, where N is how many agents exist.

## The chats browser

The browser is split. An agent list is on the left, and a live preview of the hovered agent's pane is on the right. For a dormant agent, whose pane is gone, the preview shows its stored fields instead of a live capture.

Keys in the browser:

- `enter` or `l` opens that agent in place, inside the same popup. Detach with `prefix + d` to return to the picker.
- `r` renames the agent, which renames its tmux session too.
- `d` deletes the agent. This is a soft delete: it hides the agent and kills the session. If you are inside it when you delete it, you get bounced to the previous session first.
- `j` and `k` move the selection.
- `C-d` and `C-u` scroll the preview.
- `esc` goes back to the picker.

## Persistence

The popup is one persistent space, not a stack. Opening an agent attaches to it without closing the popup, and detaching drops you back at the picker. Pressing the panel key again while you are already viewing an agent detaches back to the picker rather than nesting another popup. Because an agent's record outlives its tmux session, the list survives tmux restarts and shows anything dormant. See [concepts](concepts.md) for how dormant agents are restored.
