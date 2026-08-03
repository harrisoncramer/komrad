# Schedules

TODO: Refine schedules

A schedule is a durable timer that launches an [agent](agents.md) into a [workflow](workflows.md) on a cron expression. It is a row in Postgres rather than a crontab entry, and the service reconciles the table onto a job queue every ten seconds, so a schedule survives restarts.

Schedules hold a name, cron expression, an optional timezone, a prompt, the workflow it fires into, whether it is enabled, and whether its agents start hidden.

There is no directory or flags on it, which are read off the workflow every time.

## How firings behave

There are a few important things to consider with weave's schedules:

- There are no backfills. If the service is down across a firing, that occurrence is skipped and the next one runs normally. This is intentional.
- A late firing still counts, up to an hour past due, and runs once under the right name. Past an hour it is given up on.
- One run at a time. If the last agent is still thinking or waiting when the next tick comes, the tick is skipped. An idle agent does not hold the schedule back.
- Launches are serialized, since each one opens a tmux window and types into it. Transient failures retry up to five times.
- A firing that hits `max_agents` is cancelled rather than retried. Keep an armed schedule and never archive its runs and it will eventually fill your ceiling and quietly stop.

## Disabled versus archived

Disabling pauses a schedule and keeps everything. The next reconcile pass takes its timer off the queue and the panel shows no next run. Flip it back on for the next matching occurrence. Use it for anything you will want again.

Archiving puts it away for good. Nothing brings an archived schedule back yet. Both leave already-launched agents alone.

Hidden runs are the other switch worth knowing: `hide_agents` starts this schedule's agents hidden from the panel's list. They still run, they just do not take a row, which is what makes a chatty every-fifteen-minutes job something you can leave armed.

## From the CLI and MCP

Same operations either way: `list_schedules`, `get_schedule`, `create_schedule`, `update_schedule`, `set_schedule_enabled`, `archive_schedule`.

    weave rpc create-schedule \
      --name morning-triage \
      --cron "0 9 * * 1-5" \
      --timezone America/New_York \
      --workflow-id "$WORKFLOW" \
      --prompt "Read the issues opened since yesterday, label them, and comment on anything that looks like a duplicate." \
      --enabled

Get the workflow id from `weave rpc list-workflows`. Leave `--enabled` off to write one down without arming it. Update replaces every field, so read the schedule first. To pause one without touching anything else:

    weave rpc set-schedule-enabled --id "$SCHEDULE" --enabled=false

A scheduled agent is not special. Same launch path, same workflow directory, flags and context, same tool surface. The only difference is that its row records the schedule that made it.
