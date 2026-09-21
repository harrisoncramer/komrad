# Agents

An agent is just a managed claude or codex chat that you can step away from, reboot, search through, or automate. Some ideas:

- A worktree agent. Every agent you start with weave can be configured to get an isolated checkout, and takes its worktree with it when you delete it.
- A triage agent on a schedule. Try configuring a workflow with context explaining what your on-call rotation cares about, and a [schedule](schedules.md) that creates an agent every weekday morning with a prompt like "read the overnight alerts and write up anything that needs a human".
- An orchestrator. An agent whose prompt is to fan work out: it calls weave's [MCP](mcp.md) to `create_agent` for each subagent, and then communicates through Weave's mail.

## Creating your first agent

Press `c` in the sidebar. If you don't specify a harness, weave will use the `default_harness` from your configuration. You can also create an agent via the CLI.

Agents can be thinking (generating or running a tool); waiting (blocked on a human confirmation or other interactive prompt); idle (sitting with nothing to do); or uncertain, drawn as a grey `?`, which is a pane weave captured but could not read. An uncertain agent is not a finished one: weave leaves it out of the auto-archive sweep and holds a schedule back for it, because the screen it could not read may be a question waiting on you. Its mail waits as well, since weave only types at a pane it recognises, and goes as soon as the pane reads again.

Separately from state, an agent is either running or dormant. Dormant agents' panes are closed because the CLI has exited, but can be restored, by resuming the session.

## Adopting work that already exists

Adoption puts an agent on a branch somebody else made. Hover a worktree-enabled workflow's header, press `a`, and give it a branch name. It is the workflow's key rather than an agent's, since what you are adopting into is the workflow.

A branch is git's own idea rather than a forge's, so adoption works the same on GitHub, on GitLab, on a self-hosted remote, and on a repository with no forge behind it at all. A pull request URL or number is refused, naming what to do instead.

Where the agent runs depends on what is already there. When nothing has the branch checked out, the agent gets a worktree of its own on it, fetched from origin first. When some other checkout already holds it, made by hand or by another tool, the agent is launched into that directory as it stands and provisions nothing. When the branch is checked out in the workflow's own directory, adoption refuses, since that is the checkout you are working in.

The branch belongs to whoever made it. Weave never resets it, and deleting an adopted agent removes the worktree but leaves the branch alone. Adopting a branch that already has an agent on it is refused, naming the agent that has it, since git could not check the branch out twice anyway.

The agent starts knowing what it walked into: the prompt it is given is prefixed with the branch and with the fact that the commits are not its own to rewrite. Leave the prompt blank and it is told nothing has been asked of it yet, and to say when it is ready and wait.

Agents can adopt too, through `adopt` over [MCP](mcp.md).

## Children

A child agent is attached to another agent rather than standing on its own. It runs in that agent's own directory, provisions nothing of its own, may only mail inside its family, and goes when the parent goes. The panel draws it indented under the row it belongs to.

A child is the default shape when an agent spawns another, and what decides it is whether the new agent reads the parent's tree or writes in it. Reading is a second pair of eyes on work already in progress: an author that wants its change read, or a test runner that reports what failed. Writing work of its own needs a checkout of its own, so that agent stands on its own instead. Doing the reading case with an ordinary agent means passing the parent's directory and turning its provisioner off by hand, and getting either wrong drops the agent into a fresh checkout where there is nothing to read. A child takes both from the parent, so there is nothing to get wrong.

Press `C` on the hovered agent to attach one. The form asks for the prompt, a title, the harness and the flags, and nothing about where it runs, since none of that is yours to choose here. The flags start blank rather than copied from the parent. Agents attach children too, through `create_agent` over [MCP](mcp.md) with `parentAgentId` set to their own id.

What a child cannot do is the point of it. It has no directory of its own, so it is a guest in the parent's checkout: it is told to read and run but not to commit, push, stash, or switch branches. It cannot have children of its own. And it can only reach its parent and that parent's other children by [mail](mail.md).

Archiving or deleting the parent takes the children with it, and a child named on its own goes on its own, which is how you finish with a reviewer while the agent it was reading for keeps working. Restoring is the one direction that holds the family together: a child comes back only once its parent is live again, since there would otherwise be no row for it to be drawn under.

Because they all edit the one checkout, one agent may hold only a few children at once. `max_children` in your configuration is that ceiling, and a call past it is refused rather than queued.

## Metadata

Metadata is how an agent records what a person would want to open later. It is a set of key/value rows on the agent, written by the agent.

The panel shows those in the details under the list, after what the agent was launched with, and agents write them with `set_agent_metadata`, keyed on their own id from `WEAVE_AGENT_ID` environment variable. The key `pr` is refused: a pull request is read from the forge and shown on the row on its own, so there is nothing for an agent to record.

## Pull requests

The third glyph on an agent's row is the pull request opened from its branch, read from the forge by the [pull request sync](forges.md#reading-pull-requests). The row shows one, the current one: the newest opened that is not finished, else the newest opened of the finished. Open is the pull request glyph greyed, one the forge would merge now is a green check, changes requested is the pull request glyph in yellow, a draft has its own glyph greyed, a failing check is a red cross, a branch that conflicts with its base is a red stop sign, merged is purple, and closed without merging is red. Nothing is drawn while the repository has not been read yet, or once it has and nothing was opened from the branch; the details under the list say which.

The details carry a `pull request` row for the current one, such as "#740 merged 2h ago", and a `pull requests` row listing the rest when the branch holds more than one. `list_agent_pull_requests` over [MCP](mcp.md) answers the same listing.

## The details

The details under the list are the record of the hovered row. For an agent they hold what it was launched with, its branch and pull requests, its worktree's standing against the base, its metadata, and then the uncommitted paths and the commits since the base, ten of each at most. The worktree rows come from `get_worktree`, read when the cursor rests on the row and again once a minute, so they can be a minute behind.

`ctrl+t` moves the keys onto the record, which grows to show every row the sidebar has room for. `/` then searches the rows as it does the list, on the same line: `tab` turns the search into a filter, `enter` keeps it, `n` and `N` step between matches, and `esc` clears it before a second `esc` hands the keys back, taking the search with them.

## The git menu

`b`, for branch, draws the git actions beside the list: `r` rebases the hovered agent's worktree onto the latest base, and `o` opens the current pull request in your browser. An entry is picked with its own letter or with the cursor, and `esc` leaves without doing anything.

An entry that would do nothing on this agent says so in the menu rather than leaving the key dead. An agent whose workflow makes no worktrees has no base to rebase onto, and one whose branch has no pull request has none to open.

## Marks

A mark is a letter you put on a row so you can jump straight back to it, the way marks work in vim. Press `m` and then a letter to mark whatever the cursor is on, and press `'` and then that letter from anywhere in the list to put the cursor back on it. The letter is drawn in the gutter down the left of the list, so the rows you have named read as a column rather than something to hunt for along a row.

Agents and workflows are marked the same way and share the same letters, since a letter names one place. `'a` is one row, never a choice between two.

A letter is held by one row at a time. Marking a row with a letter another row already holds moves the mark rather than refusing it. Pressing `m` and then the letter a row already holds takes the mark off it, which is how a mark is cleared.

`'` pressed twice goes back to wherever the cursor was before the last jump, so looking at a marked row and getting back to what you were reading are the same gesture. A jump always lands: if the marked row is hidden, the jump reveals it rather than doing nothing.

Marks are yours. Nothing an agent can call sets one, and a row loses its mark when it is archived, since the letter is an address for a row on the screen rather than anything about the work.

## Searching the list

Press `/` and type to search the list. It highlights rather than narrows: every row stays where it was, and the matched text is drawn lit up in the list and in the record under it, so you keep the shape of the tree while you look. Nothing moves while you type.

Enter applies the search to both sections at once: in filter mode the list and the record narrow to their matches, and the cursor of the section holding the keys walks onto its nearest match. `ctrl+t` applies it the same way but moves the keys to the other section first, so a search typed on the list lands on the record's match with the list's cursor still on the agent you were on. Escape gives up: the search goes and the cursor stays where it was.

`n` and `N` step forward and back through the matches, the list's rows and then the record's fields, wrapping at either end and moving the keys into whichever section the next match is in. They only answer while a search is up, since they are ordinary letters the rest of the time.

## Archiving, restoring, deleting

Archiving an agent (the `z` key by default) stops the agent, kills its pane, and takes it off the panel, keeping the row, the metadata, and the transcript. Archiving is the ordinary way to finish with an agent.

The prompt that key opens offers archiving and "Archive and delete", which archives and then destroys the agent outright.

To see the full archive press the `Z` key. You can restore agents from the archive, or permanently delete them. Be careful: deletion destroys the row, the metadata, the flattened transcript, and its search index, and it removes the agent's worktree, so a delete can tear down real directories on disk. Search will never find that conversation again. All of these can also be done in bulk via API, or via the panel, by selecting multiple agents with the `v` or `V` keys.

An archived workflow can be deleted the same way: hover its header row in the archive and press `d`. Its archived agents go with it through the same deprovision handling as deleting them directly, so a row whose deprovision is still running, or whose deprovision fails, is left behind and the workflow stays archived until nothing is left in it.

To clear the whole archive at once press `D`. It asks first, and then hands the work to a background job rather than holding the panel open, since deleting that many agents means removing a worktree apiece. Rows whose worktree is still being removed are left behind until it finishes, so press it again if anything is still there. A row whose removal failed stays in the archive too, marked with a red `✗`, since the sweep could not destroy it: it is that mark, and the stage beside it, that say what went wrong. Any archived workflow the sweep leaves with nothing inside it is deleted too, rather than sitting there as an empty shell.
