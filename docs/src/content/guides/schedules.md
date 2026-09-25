---
title: Schedules
description: Start agents on a timer, and what happens when a run is late or busy.
group: start
order: 5
---

The `myapp` [workflow](../workflows/) now launches agents with their dependencies ready, thanks to the [hook](../hooks/). Suppose you want one every weekday morning to read the overnight alerts and write up what needs a person. Open the schedules screen, press `c`, pick the workflow, write the prompt, and give it a cron expression. That is a schedule: it starts an [agent](../agents/) in a [workflow](../workflows/) on a timer, so work gets done while you are not there.

A schedule holds a prompt, a workflow, the timing and an optional time zone. Everything else comes from the workflow each time it fires. It lives in komrad's database, not your crontab, so it survives restarts and moves with the engine.

<figure class="placeholder">
  <figcaption>The schedules screen, listing each schedule with its next run and the result of its last one.</figcaption>
</figure>

## How firings behave

Komrad never catches up on missed runs. If the service was down when a run was due, that run is skipped.

A schedule never piles up work. If the agent from its last run is still working or waiting on you, the next run is skipped and recorded as skipped.

A run that cannot succeed is cancelled with a reason, for instance because the workflow is gone or you have hit your agent limit. A schedule whose agents are never archived will eventually hit that limit, and the last result on the schedules screen is where you notice.

## Pausing versus archiving

Turning a schedule off pauses it and keeps everything. Archiving puts it away for good. Neither touches the agents it already started.

## From scripts and agents

Everything you can do in the panel you can do with `komrad rpc`, including listing past runs. Over MCP, the schedule tools go only to orchestrators. The [API reference](../../api/schedule/) lists them.

One agent at a time is only part of it. Agents can also talk to each other by [mail](../mail/).
