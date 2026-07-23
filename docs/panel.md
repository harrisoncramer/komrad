# The panel

The panel is where you browse and reach your agents. Open it with `prefix + g`. It comes up as a `claude-manager` popup, a 95% centered popup that persists as a single space: opening an agent attaches to it inside the same popup, and detaching drops you back where you were. The popup closes only when you quit the picker.

## The picker

The picker is the first screen. It has three rows.

The `new chat` row prompts you for a title, a directory, extra flags, and an opening prompt, then launches a background Claude session with them.

The `chats (N)` row opens the browser, where N is how many agents exist.

The `search` row opens search.

Move with `j`/`k` and choose with `enter`, or jump straight to a row: `n` for new, `c` for chats, `/` for search.

## Search

Search finds a past chat by what was said in it. Press `/` from the picker to open it, type to filter, and the matches rank underneath with a snippet of the transcript around your terms.

Search spans every tracked chat, including deleted ones, which are tagged so you can tell them apart. It matches on the chat's name and title as well as its transcript, and a term found in the name or title ranks that chat first. A query matches only when every term appears, so adding words narrows the results.

The transcript text comes from Claude Code's own session files, which this tool reads but does not store. Opening a result attaches to it, restoring the session if its pane is gone and undeleting it if it was deleted.

Keys in search: `enter` opens the highlighted chat, `up`/`down` (or `C-p`/`C-n`) move, and `esc` goes back to the picker.

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
