---
title: Schedules
description: Launch agents into a workflow on a cron expression.
group: concepts
order: 6
---

A schedule launches an [agent](../agents/) into a [workflow](../workflows/) on a cron expression. It is a row in Postgres rather than a crontab entry, and the service copies the table onto its job queue every ten seconds, so schedules survive restarts.

A schedule has a name, a cron expression, an optional timezone, a prompt, the workflow it launches into, whether it is on, and whether its agents start hidden. It has no directory or flags of its own. Those are read from the workflow each time it fires.

The schedules screen shows each schedule's next run and the result of its last one.

## How firings behave

A firing missed while the service was down is skipped, and the next one runs as normal. Weave never backfills on purpose.

A late firing still runs, once, if it is less than an hour overdue. Past an hour it is dropped.

Only one run happens at a time. If the last agent is still thinking, waiting, or on a pane weave could not read, the next tick is skipped. An idle agent does not hold the schedule back.

Launches happen one after another, because each one opens a tmux window and types into it. Temporary failures are retried up to five times.

A firing that would go over `max_agents` is cancelled rather than retried. A schedule that runs forever and never has its agents archived will eventually fill your limit and stop launching. The last result on the schedules screen is where you would notice.

## Turning a schedule off versus archiving it

Turning a schedule off pauses it and keeps everything. It has no next run until you turn it back on, and then it fires at the next matching time. Use this for anything you will want again.

Archiving puts a schedule away for good, and nothing brings it back yet. Neither one touches agents it already launched.

## From the CLI and MCP

The same operations are available both ways, such as `create_schedule`, `list_schedules` and `get_schedule`.

```sh
weave rpc create-schedule \
  --name morning-triage \
  --cron "0 9 * * 1-5" \
  --timezone America/New_York \
  --workflow-id "$WORKFLOW" \
  --prompt "Read the issues opened since yesterday, label them, and comment on anything that looks like a duplicate." \
  --enabled
```

A scheduled agent is an ordinary agent. It is launched the same way with the workflow's directory, flags and context, and gets the same tools. The only difference is that its row records which schedule made it.
