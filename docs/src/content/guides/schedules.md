---
title: Schedules
description: Start agents on a timer, and what happens when a run is late or busy.
group: concepts
order: 6
---

A schedule starts an [agent](../agents/) in a [workflow](../workflows/) on a timer, written as a cron expression. It is how you get work done while you are not there, like a triage agent every weekday morning or a dependency check every Monday.

A schedule is kept in komrade's database rather than your crontab, so it survives restarts and moves with the engine. It holds a prompt, the workflow to launch into, the timing and an optional time zone. Everything else, such as the directory and the CLI's flags, comes from the workflow each time it fires. The agent it starts is named after the schedule and is otherwise an ordinary agent.

<figure class="placeholder">
  <figcaption>The schedules screen, listing each schedule with its next run and the result of its last one.</figcaption>
</figure>

## How firings behave

Komrade never catches up on missed runs. If the service was down when a run was due, that run is skipped and the next one happens as normal. A run that is only a little late still happens, once.

A schedule never piles up work. If an agent from its last run is still working, waiting on you, or unreadable, the next run is skipped and recorded as skipped. An agent that has finished and gone idle does not hold anything back.

A run that fails for a passing reason is tried again a few times. A run that cannot succeed is cancelled instead, and the reason is recorded. That covers a workflow that has gone, a directory that does not exist, or a setup that failed. It also covers hitting your agent limit. A schedule that keeps firing and never has its agents archived will eventually reach that limit and stop, and the last result on the schedules screen is where you would notice.

## Pausing versus archiving

Turning a schedule off pauses it and keeps everything, ready to turn back on. Use this for anything you will want again.

Archiving puts a schedule away for good. Neither one touches the agents it already started. A workflow that a schedule still launches into cannot be archived until the schedule is.

## Driving schedules from scripts and agents

Everything you can do with schedules in the panel you can also do from the CLI with `komrade rpc`, including listing past runs. Over MCP, the schedule tools are offered only to orchestrator agents, since only they are meant to plan other agents' work. The [API reference](../../api/schedule/) lists them.
