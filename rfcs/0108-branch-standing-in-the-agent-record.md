# Branch standing in the agent record

## Summary

The agent record should show at a glance how big an agent's change is and whether any of it is sitting unpushed. This proposes three things. A `+N −M` line count on the rule above the record. A `remote` row counting commits against the branch's upstream. Colour on the ahead and behind arrows. All of it comes from the `GetWorktree` read the record already makes when the cursor rests on an agent, so hovering costs no new call. The record also loses its `doing` row and its list of uncommitted paths.

## Problem

The record for a hovered agent is built in `agentFields` in `internal/ui/markdown.go`. Its git rows come from `worktreeStandingFields` and `worktreeHistoryFields` in `internal/ui/agentworktree.go`. Three things are wrong with them.

The size of a change is invisible. The icons in the list only say whether an agent has a worktree. To learn whether an agent wrote ten lines or two thousand, you have to open the review screen, and that opens a review round.

The `base` row, drawn by `baseField` in `internal/ui/worktreescreen.go`, prints `origin/main  ↑3 ↓12` as plain text. People read the arrows as unpushed and unpulled. They are neither. `CountAheadBehind` in `internal/git/standing.go` counts against the workflow's base branch. ↑ is what the branch adds and ↓ is what the base has moved on by. Nothing in the record says whether the agent's commits have reached its remote branch. That is the question that matters once agents push to a draft pull request early.

The record also carries rows that no longer earn their space. `doing` shows `ReportedReason`, which is state nobody is keeping up to date. The `uncommitted` list repeats, path by path, what the `changes` row already counts.

## Proposal

### What it looks like

The record sits between two rules drawn by `detailsRule` in `internal/ui/details.go`. The line count goes at the right end of the top rule. Colours are shown in brackets.

```
──────────────────────────────────────────────── +214 −87 ─
                                         [green]  [red]
  name         git-diff-info-lines
  agent        claude
  workflow     komrad
  directory    ~/.local/state/komrad/worktrees/01a0de41…
  branch       agent/01a0de41-b5eb-7995-a14c-6987bd078f08
  base         origin/main   ↑3  ↓12
                            [grn][yel]
  remote       ↑1 unpushed
               [bold orange]
  changes      2 modified, 1 untracked
  size         412 MB, measured 2m ago
  flags        --model opus
  agent_id     01a0de41-b5eb-7995-a14c-6987bd078f08
  session_id   86d09d50-0945-445b-b674-b198645f98f4
  created      today 11:02
  commits      a1b2c3d feat(ui): colour ahead and behind   3h
               d4e5f6a refactor: split baseField           5h
───────────────────────────────────────────────────────────
```

When the directory has nothing to show, each row says so in one dim word instead of zeros.

```
─────────────────────────────────────────────────── clean ─
  name         terminal-spawn
  agent        claude
  workflow     komrad
  directory    ~/.local/state/komrad/worktrees/7c3e91b2…
  branch       agent/7c3e91b2-…
  base         origin/main   even
  remote       even
  changes      none
  size         38 MB, measured 1m ago
  ...
───────────────────────────────────────────────────────────
```

If the branch has no upstream, the remote row reads `no upstream`. If the base has moved on but the agent has committed nothing, the header still reads `clean`, `base` shows only the yellow ↓, and there is no `commits` section. While the first read is in flight, the header shows nothing. After that, a stale read keeps its numbers on screen until the next read replaces them.

For colour: ↑ against base is green. ↓ against base is yellow, not red, because being behind the base is normal. Unpushed commits are bold orange, since that is the number to act on. Unpulled commits are yellow. Zero counts are dim or left off.

### Where the numbers come from

Hovering an agent already calls `inspectAgentWorktreeCmd`, which asks for `GetWorktree` and holds the answer for `worktreeReadTTL` (one minute). On the server that becomes `Inspector.Inspect` in `internal/worktrees/inspect.go`, and then `readStanding`. `readStanding` already runs `ReadChanges`, `CountAheadBehind`, `PredictRebase` and `ListCommitsSince`, and `Inspect` walks the whole directory to measure its size. Two more git commands go in `readStanding`.

The line count is `git diff --numstat -z <merge-base> -- . <exclude pathspecs>`, measured against the working tree. That covers committed and staged changes as well as unstaged edits to tracked files. It takes the workflow's `DiffExclude`, so the count leaves out the same paths the review screen does. `baseOf` already loads the workflow and can return the exclusions along with the base branch. The function goes in `internal/git/repo.go` beside `countChangedFiles`, written the same way as a private `diffStat(ctx, on runner, params)`. Then `Client` and `EngineClient` can both expose it, and it works whichever side of an engine it runs on.

The remote count is `git rev-list --left-right --count @{u}...HEAD`. It returns a sentinel error when the branch has no upstream. It does not fetch. The count is only as fresh as the last fetch in that worktree, and the agent's own push updates it.

`GetWorktreeResponse` in `proto/proto/worktree/v1/worktree.proto` gains `added_lines`, `removed_lines`, `has_upstream`, `unpushed` and `unpulled`. They are carried through the RPC handler and `worktreeInspectionFrom` in `internal/ui/convert.go` onto `worktreeInspection`.

### Drawing it

`recordField` is two strings today, and the value is also what `copyDetailsField` copies and what the search highlights. Painting ANSI into the value would break both. Instead `recordField` gains an optional styled rendering. The plain value stays the text that is copied and searched. The styled rendering is drawn when there is no search or flash on the row. `baseField` returns both forms, so the worktrees screen, which shares it, gets the colours too.

`detailsRule` takes an optional right-aligned tag for the top rule. The agent record passes the `+N −M` or `clean` tag when the hovered agent's worktree has been read, and nothing otherwise. The count reuses the added and removed colours from `internal/ui/diffview/render.go`, so the review screen and the record agree.

`agentFields` drops the `doing` row. `worktreeHistoryFields` drops the `uncommitted` rows and keeps `commits`.

## Alternatives

Reuse what the review screen loads. `loadReviewCmd` in `internal/ui/reviewload.go` finds a pending round or opens one with `OpenReviewRound`. Opening a round takes a snapshot, which is a write. It then runs `diff.Load` in the panel, which parses the whole patch and reads both sides of every file for syntax colouring. Doing that on hover would fill the database with rounds nobody asked for and spend seconds on two numbers. On a remote engine it is worse, because `reviewGit` swaps in `git.NewEngineClient`, and every git command becomes its own trip over the engine's transport. A future Diff RPC would not change this, since it would still return a whole patch tied to a snapshot.

Show the numbers on every row of the agent list. That needs a count for every agent, not just the hovered one. That means a background sweep on the server that stores a numstat per worktree, the way the size measurement works. It is a bigger change, and a numstat on a large monorepo worktree is not free. Hover first; a sweep can come later if the list turns out to need it.

A header in the terminal pane through tmux's `pane-border-status`. This would put the count on the shell pane komrad cannot draw into. It was dropped. The record is where people look for this, and a border costs a line of height in every info pane.

Keep the arrows and change only the label. Renaming `base` to make clear it is not the remote would stop the misreading, but it would still not answer whether the work is pushed.

## Risks and open questions

The line count leaves out untracked files, and the review screen includes them because a round's snapshot does. So `+N −M` will read lower than the review screen while an agent has new files it has not added. Counting them means reading every untracked file on each hover. The `changes` row already shows how many untracked files there are, so the gap is accepted rather than closed.

`GetWorktree` also serves the worktrees screen, so every read there pays for the two extra git commands. The directory walk in `Inspect` costs far more, so the extra time should be lost in the noise. That should be checked on a large worktree before this ships.

The remote count reflects the last fetch in the worktree, not the forge. An agent whose remote branch was pushed from elsewhere will show stale unpulled counts until something fetches. Fetching on every hover is too slow, so this is accepted.

The numbers can be up to a minute old, the same as the ahead and behind counts today.
