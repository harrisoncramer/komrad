---
name: debug-weave
description: Debug weave's own runtime - pane frames and how an agent's state was decided, the recorded captures on disk, the log, and the two instances. Use when a weave agent shows the wrong state, a pane reads as uncertain, or something in the panel makes no sense.
---

# Debugging weave

## How a state is decided

Every couple of seconds the service captures each agent's pane and hands the frame to that
agent's harness. `ReadInput` in `internal/harness/claude/activity.go` returns an
`harness.Activity` (`internal/harness/harness.go`), and `internal/agents/reconcile.go` maps it
onto the `agent_state` enum: working to thinking, waiting to waiting, idle and composing to
idle, unrecognized to uncertain, unknown leaves the agent where it was.

The classifier anchors on the input box, not on the bottom of the frame. It finds the rule with
the prompt marker under it, then reads only the lines between that box and the conversation
above. A greyed-out prompt marker also counts as working.

There is no error state. A retry banner (`· Retrying in 1s · attempt 2/10`) matches the working
line pattern and is deliberately read as working; see `api_retry_banner.txt` in the test table.

## The recorded frames

Turned off by default. `[debug] frames = true` in `~/.config/weave/config.toml`, with
`frame_history` capping how many are kept per agent.

They land in `<cache dir>/weave-frames/<agent id>/`, so
`~/Library/Caches/weave/weave-frames/<agent id>/` for the development instance and
`~/Library/Caches/weave-test/weave-frames/<agent id>/` for the throwaway one. Each file is named
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

`~/Library/Caches/weave/weave.log` explains a failure to launch, reconcile, or index. Filter it
by agent id. `weave-run-logs/` holds a provisioner's or hook's own output, `weave-rounds/` the
checkouts cut for a review.

`weave version` prints this binary's build and the one the server is answering on. A rebuild
changes nothing until the server restarts.

## Asking the database

The weave MCP server's `query` reads the whole Postgres directly, which beats hunting through
the panel for anything shaped like an aggregate. `agents.state` is an enum, so a pattern match
over it needs `state::text`.

## Which instance

`~/weave` on port 8787, tmux socket `weave`, Postgres 5460, cache `~/Library/Caches/weave`.
`~/weave-test` on 8790, socket `weave-test`, Postgres 5461, cache `~/Library/Caches/weave-test`,
and every command there is prefixed `MISE_ENV=test`. The MCP tools in a normal session point at
the development instance, so a call meant for the throwaway one has to go to the `weave-test`
server.
