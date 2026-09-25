---
title: Mail
description: How agents send each other messages, and the limits that keep them in check.
group: start
order: 6
---

So far each [agent](../agents/) has worked alone. Agents can also talk to each other, by mail. It is how an orchestrator hands out work and hears back, and how a reviewer tells an author what it found.

## Try it

Spawn an agent with this prompt and watch the two argue.

```text
You are an avid neovim fan, and need someone to debate. Spawn an emacs agent in the same workflow and have a debate with them about the merits of the two editors, for a few rounds.
```

<figure class="placeholder">
  <figcaption>Two agents side by side, one reading mail the other just sent.</figcaption>
</figure>

## How mail arrives

Mail never interrupts an agent mid-turn. An agent picks up its mail when it finishes a turn. An idle agent is not finishing turns, so komrad types a short note into its terminal saying mail is waiting, and repeats it until the mail is read. It never types over something you are halfway through writing.

Because mail arrives when a turn ends, an agent cannot wait for a reply inside a turn. Agents are told to end their turn and let the reply come to them.

Mail to a stopped agent waits until it runs again. Mail unread past [`mail.delivery_deadline`](../../config/#mail.delivery_deadline) expires. You can read any agent's mail from the CLI or over MCP.

## The limit on a conversation

Two agents can only write each other so much, set by [`mail.max_exchange`](../../config/#mail.max_exchange). It exists to stop two agents looping on each other forever. When the limit stops a message, the sender gets an error and the other agent is told, so it does not wait for a reply that will never come. Your own mail never counts. Deleting old mail between a pair gives them room again.

## Families

A [child agent](../agents/#agents-that-work-together) can mail its parent and its siblings, and nobody else. An agent outside the family cannot mail the child. You can always mail anyone.

Mail is how agents talk. [MCP](../mcp/) is how one agent starts another in the first place.
