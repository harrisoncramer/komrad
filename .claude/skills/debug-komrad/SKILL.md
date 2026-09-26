---
name: debug-komrad
description: Debug komrad's own runtime - pane frames and how an agent's state was decided, the recorded captures on disk, the log, and the two instances. Use when a komrad agent shows the wrong state, a pane reads as uncertain, or something in the panel makes no sense.
---

# Debugging komrad

## How a state is decided

Every couple of seconds the service captures each agent's pane and hands the frame to that
agent's harness. `ReadInput` in `internal/harness/claude/activity.go` returns a
`harness.Input`, whose `Activity` (`internal/harness/harness.go`) `internal/agents/reconcile.go`
maps onto the `agent_state` enum: working to thinking, waiting to waiting, idle and composing to
idle, unrecognized to uncertain, unknown leaves the agent where it was.

Only half of that mapping is immediate. Thinking and waiting are applied on the first frame that
says so, but idle and uncertain go through `settle`: they need `reconcileMisses` passes in a row
reading the same thing, or for idle a fresh turn boundary reported by the CLI's own hook.
Uncertain holds twice over, since `observeUnrecognized` keeps an unrecognized frame that is still
changing rather than demoting the agent at all. So a pane that has plainly stopped while the row
still says thinking is usually the reconciler counting, not the classifier misreading.

The classifier anchors on the input box, not on the bottom of the frame. It finds the rule with
the prompt marker under it, then reads only the lines between that box and the conversation
above. A greyed-out prompt marker also counts as working.

There is no error state. A retry banner (`· Retrying in 1s · attempt 2/10`) matches the working
line pattern and is deliberately read as working; see `api_retry_banner.txt` in the test table.

## The recorded frames

Turned off by default, under `[debug]`: `frames = true` records them and `frame_history` caps
how many are kept per agent. Which file to set it in depends on the instance. The development one
reads `~/.config/komrad/config.toml`; the throwaway one reads `komrad.test.toml` in that checkout,
named by `KOMRAD_CONFIG` in `mise.test.toml`, which replaces the user's config rather than layering
on it. That file already turns the recording on, so the throwaway instance has captures waiting.

They land in `<cache dir>/komrad-frames/<agent id>/`, so
`~/Library/Caches/komrad/komrad-frames/<agent id>/` for the development instance and
`~/.local/state/komrad-test/instances/<slot>/cache/komrad-frames/<agent id>/` for the throwaway one. Each file is named
`<utc stamp>-<seq>-<activity>.txt` and holds the capture exactly as the harness read it, escapes
and all, so the name tells you what the classifier decided without opening it:

    ls <dir> | sed 's/.*-//' | sort | uniq -c

A frame is kept when it differs from the last one kept for that agent, or when the reading
disputes the state on record (`doesReadingDispute`). A still pane writes nothing.

Read one with `cat -v`. The raw bytes matter: colour is part of the classification.

## Replaying a frame

There is no replay command. Copy the frame into
`internal/harness/claude/testdata/` and add a row to the table in `frames_test.go`, then
`go test ./internal/harness/claude/`. A frame that misled the panel belongs in that table
whether or not you change the classifier.

## Everything else on disk

`~/Library/Caches/komrad/komrad.log` explains a failure to launch, reconcile, or index. Filter it
by agent id. `komrad-run-logs/` holds a provisioner's or hook's own output, `komrad-rounds/` the
checkouts cut for a review.

`komrad version` prints this binary's build and the one the server is answering on. A rebuild
changes nothing until the server restarts.

## Asking the database

The komrad MCP server's `query` reads the whole Postgres directly, which beats hunting through
the panel for anything shaped like an aggregate. `agents.state` is an enum, so a pattern match
over it needs `state::text`.

## Which instance

`~/komrad` on port 8787, tmux socket `komrad`, Postgres 5460, cache `~/Library/Caches/komrad`.
Every other checkout, `~/komrad-test` and each worktree, runs a throwaway instance of its own
under `MISE_ENV=test`, on the numbered slot it claimed under `~/.local/state/komrad-test/instances`:
server 8800 plus the slot, socket `komrad-test-<slot>`, Postgres 5470 plus the slot, and a cache in
the slot's directory. `MISE_ENV=test mise env` prints them. The MCP tools in a normal session point
at the development instance, so a call meant for a throwaway one has to go to a server registered
at that instance's address.
