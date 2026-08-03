# Schedules

A schedule is a durable timer that launches an [agent](agents.md) into a [workflow](workflows.md) on a cron expression. It's a row in Postgres, not a cron entry on your machine, and the service turns it into a launch when its time comes.

Nothing runs when you create one. The agent appears the next time the expression matches, and again on every match after that.

TODO TODO TODO: Write schedule file.

## What a schedule holds

A name, a cron expression, an optional timezone, a prompt, the workflow it launches into, whether it's enabled, and whether the agents it produces start hidden from the panel's list.

That's the whole record. There's no directory, no CLI flags, no context file on a schedule, because it reads all of that off the workflow every time it fires. Move a workflow to a different directory and its schedules move with it.

The name does double duty: it labels the schedule in the panel and it names each agent the schedule launches. If a live agent in that workflow already holds the name, the new one gets a `-2`, `-3` suffix. Names are unique among live schedules, at most 200 characters, and control characters are refused rather than stripped, since the name becomes a tmux window name and a bad one would break every firing rather than just the create.

The prompt is required. It's the entire point of the schedule, unlike an agent's prompt which is optional.

There's no last-run column. A schedule points at no agent; each agent it launched records the schedule that made it, so the history is on the agents. The next run isn't stored either. It's derived from the cron expression when a listing is rendered, so it can't go stale, and it's blank for a paused schedule because a paused schedule has no next run.

## The cron format

Five whitespace-separated fields: minute, hour, day of month, month, day of week. Nothing else. Shorthands like `@daily` and `@every 1h` are rejected by the request validation before the parser ever sees them, so write `0 0 * * *` instead.

The expression is parsed at create and update time, not at fire time. A schedule that could never fire is refused where you wrote it rather than sitting armed in the table doing nothing. That includes dates the calendar never reaches, like February 30th, which parse cleanly and then never come round.

Some expressions to copy:

    0 9 * * 1-5     9am on weekdays
    30 2 * * *      2:30am every day
    0 */2 * * *     every two hours, on the hour
    0 7 * * 1       7am on Mondays
    */15 * * * *    every fifteen minutes

## Timezones

The timezone field is an IANA zone name, like `America/New_York` or `Europe/London`. Leave it blank and the expression is read in the machine's local zone, which is what someone typing "9am" on their laptop means.

It's a zone rather than a fixed offset on purpose. A 9am schedule stays at 9am across a daylight saving change, which an offset would not. An unknown zone is refused at create time and reported as an unknown zone, not as a cron syntax error.

## Why a row and a loop, not a crontab

A crontab entry is a second source of truth. It lives outside the database, it doesn't know what workflow it belongs to, and it goes out of step the moment somebody edits one side and not the other.

Weave keeps the schedule in Postgres and reconciles it onto a job queue. Every ten seconds the service re-reads the schedules table and makes the queue's registered timers match: enabled schedules get registered, paused and archived ones get taken off, and a schedule whose expression changed is re-registered against the new one. That loop is why a schedule created from the panel, the [CLI](commands.md), or an [MCP](mcp.md) tool is picked up without any of them knowing the queue exists. Ten seconds of lag before a change takes effect is invisible for a record of future work.

It also survives everything. Restart the service, close the panel, reboot the machine: the table is still there and the timers rebuild themselves from it on the next pass. See [how it works](concepts.md) for the rest of the loops.

Each firing is written down as one job keyed by the schedule plus the occurrence it stands for, so the same 9am can't be queued twice however it got inserted, and a retry of today's 9am finds the agent it already made instead of making a second one.

## Missed windows

Weave doesn't backfill. If the service is down across a firing, that occurrence is simply skipped and the next one runs normally. Nothing piles up waiting to be caught up on.

A firing that's merely late still counts. If the machine was asleep or the queue was busy, a job built up to an hour after its due time is still recorded as that occurrence, so it runs once, late, under the right name, rather than colliding with the previous run or blocking the next one. Past an hour it's given up on.

Launches are serialized, one at a time, because each one opens a tmux window and types into it and a burst of them would fight over the same session. A launch that fails for a transient reason, like a tmux server that's briefly gone, is retried up to five times. A launch that fails for a reason retrying can't fix isn't retried at all.

## One run at a time

A schedule runs one agent at a time. If an hourly job is still working when the next hour comes round, the tick is skipped rather than queued behind it, since the run already going will pick up whatever the skipped one would have done. Otherwise a job that takes ninety minutes would stack a new agent beside the last one forever.

"Still working" means thinking or waiting. An idle agent doesn't hold the schedule back, whether it finished or never got going, so a launch that died on the spot can't wedge a schedule permanently.

## The agent ceiling

`max_agents` in your [config](configuration.md) caps how many unarchived agents can exist at once, 50 by default. Archived ones don't count.

A firing that hits the ceiling doesn't wait and doesn't retry. It's cancelled, and the next occurrence is the retry. That's deliberate: only a person archiving something empties the panel, so retrying five times over the next few minutes would just fail five times. If you keep an armed schedule and never clear its runs, it'll eventually fill your ceiling and quietly stop launching, so archive what you're done reading.

## Disabled versus archived

Disabling pauses a schedule. The row stays, the history stays, the next reconcile pass takes its timer off the queue, and the panel shows no next run. Flip it back on and it resumes on the next matching occurrence. Use this for anything you'll want again: a triage schedule you don't want firing while you're on holiday, a noisy job you're switching off for an afternoon.

Archiving puts the schedule away. It stops firing, drops off the panel, and keeps its record so the agents it launched still point at the thing that made them. There's nothing that brings an archived schedule back yet, so in practice it's the end of it. Use it when you're done with the schedule for good.

Both leave already-launched agents alone. They're real conversations and they keep running.

One related rule: a workflow can't be archived while a live schedule still launches into it. Turning the schedules off is a decision you make deliberately rather than something an archive does to you on the side.

## Hidden runs

`hide_agents` decides whether the agents this schedule launches start hidden from the panel's list. They still run, they just don't take a row. It's what makes a chatty every-fifteen-minutes job something you can leave armed without it burying everything else.

Each agent gets its own copy of the flag at launch, so unhiding one afterwards is that agent's business and doesn't change the schedule.

## From the panel

Press `S` from the agents list to open the schedules screen. It's its own screen rather than part of the agents tree, because a schedule is a record of future work with nothing running to preview beside it.

On that screen: `n` writes a new one, `enter` edits the one under the cursor, space pauses or resumes it, `z` archives it behind a confirmation, and `esc` goes back. The list shows each schedule's name and cron expression, and the detail beside it shows when it next fires. All of those keys are rebindable under `[keybindings.schedules]`.

The form asks for the name, the cron expression, the timezone, the prompt, the workflow, whether it's armed, and whether to hide its runs. A cron expression the server refuses comes back on the form, which is where you fix it.

## From the CLI and MCP

Same operations, same names, whichever surface you use: `list_schedules`, `get_schedule`, `create_schedule`, `update_schedule`, `set_schedule_enabled`, `archive_schedule`. Over [MCP](mcp.md) they're tools; from the [CLI](commands.md) they're subcommands with the underscores swapped for dashes and one flag per field.

A morning triage agent, weekdays at 9am:

    weave rpc create-schedule \
      --name morning-triage \
      --cron "0 9 * * 1-5" \
      --timezone America/New_York \
      --workflow-id "$WORKFLOW" \
      --prompt "Read the issues opened since yesterday, label them, and comment on anything that looks like a duplicate." \
      --enabled

A nightly dependency check at 2am, kept out of the list because most nights it finds nothing:

    weave rpc create-schedule \
      --name nightly-deps \
      --cron "0 2 * * *" \
      --workflow-id "$WORKFLOW" \
      --prompt "Check for outdated or vulnerable dependencies. If anything needs updating, open a PR. Otherwise stop." \
      --enabled --hide-agents

A weekly cleanup, Sundays at 6am:

    weave rpc create-schedule \
      --name weekly-cleanup \
      --cron "0 6 * * 0" \
      --workflow-id "$WORKFLOW" \
      --prompt "Delete merged branches older than a month and report what you removed." \
      --enabled

Get the workflow id from `weave rpc list-workflows`. Leave `--enabled` off to write a schedule down without arming it, which is a good way to get the prompt right before it starts firing on its own.

Update replaces every field, so read the schedule first and send back the ones you aren't changing. To pause one without touching anything else, use `set-schedule-enabled`:

    weave rpc set-schedule-enabled --id "$SCHEDULE" --enabled=false

## What a scheduled agent can do

Nothing about it is special. It's the same agent a person would have created, launched by the same code under the same rules, so it gets the workflow's directory, flags and context, the same MCP tool surface, and it can fire an [action](actions.md) with `run_action` like anything else. The only difference is that its row records the schedule that launched it.
