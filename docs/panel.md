# The panel

The panel is where you browse and reach your agents. Open it with `prefix + g`. It comes up as a `claude-manager` popup, a 95% centered popup that persists as a single space. Opening an agent attaches to it inside the same popup, and detaching drops you back where you were. The popup closes only when you quit the picker.

## The picker

The picker is the first screen. It has four rows.

The `new chat` row prompts you for a title, a directory, extra flags, and an opening prompt, then launches a background Claude session with them.

The `chats (N)` row opens the browser, where N is how many agents exist.

The `workflows (N)` row opens the workflow picker, where N is how many workflows exist.

The `search` row opens search.

Move with `j`/`k` and choose with `enter`, or jump straight to a row: `n` for new, `c` for chats, `w` for workflows, `/` for search.

## Search

Search finds a past chat by what was said in it. Press `/` from the picker to open it, type to filter, and the matches rank underneath with a snippet of the transcript around your terms.

Search spans every tracked chat, including deleted ones, which are tagged so you can tell them apart. It matches on the chat's name and title as well as its transcript, and a term found in the name or title ranks that chat first. A query matches only when every term appears, so adding words narrows the results.

The transcript text comes from Claude Code's own session files, which this tool reads but does not store. Opening a result attaches to it, restoring the session if its pane is gone and undeleting it if it was deleted.

Keys in search: `enter` opens the highlighted chat, `up`/`down` (or `C-p`/`C-n`) move, and `esc` goes back to the picker.

## The chats browser

The browser is split. An agent list is on the left, and a live preview of the hovered agent's pane is on the right. For a dormant agent, whose pane is gone, the preview shows its stored fields instead of a live capture. Below both, a footer shows the hovered chat's stored record, its state, pane, working directory, claude flags, resume id, and opening prompt, so those details are legible without opening the chat.

Keys in the browser:

- `enter` or `l` opens that agent in place, inside the same popup. Detach with `prefix + d` to return to the picker.
- `r` renames the agent, which renames its tmux session too.
- `d` deletes the agent. This is a soft delete: it hides the agent and kills the session. If you are inside it when you delete it, you get bounced to the previous session first.
- `m` moves the agent into a workflow. It opens the workflow picker with a leading no-workflow row, and choosing a target reassigns the chat, or unassigns it.
- `w` jumps into the workflow the hovered chat belongs to, opening that workflow's scoped chats view. In the root chats list, a chat that belongs to a workflow is marked so you can tell it apart.
- the arrow keys or `C-n`/`C-p` move the selection. Scrolling keys are shared across every menu; see [configuration](configuration.md).
- `C-d` and `C-u` scroll the preview.
- `esc` goes back to the picker.

## Workflows

A workflow is a named bucket of related chats. The workflow picker lists them, with `n` to create one, `r` to rename, `d` to delete, and `enter` to open a workflow.

Opening a workflow drops you into a scoped view that is a clone of the top-level picker, except everything is bound to that workflow. New chat creates a chat inside it, chats lists only its chats, and search matches only its transcripts. The header shows which workflow you are in. Press `esc` to step back out to the workflow picker.

The root chats row still lists every chat regardless of workflow, so the flat view is intact and workflows are additive. Back-navigation follows the view you were in, not the chat's workflow: open a chat from inside a workflow and detaching returns you there, while a chat opened from the root list or a global search returns you to the root, even if it belongs to a workflow.

Deleting a workflow prompts you to choose what happens to its chats. Keeping them orphans them back to unassigned, where they still show in the global chats list and search. Deleting them soft-deletes the chats along with the workflow, so they leave the live view but stay findable in search, tagged deleted. The abort key cancels the delete entirely.

## Persistence

The popup is one persistent space, not a stack. Opening an agent attaches to it without closing the popup, and detaching drops you back at the picker. Pressing the panel key again while you are already viewing an agent detaches back to the picker rather than nesting another popup. Because an agent's record outlives its tmux session, the list survives tmux restarts and shows anything dormant. See [concepts](concepts.md) for how dormant agents are restored.
