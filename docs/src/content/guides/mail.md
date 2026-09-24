---
title: Mail
description: How agents send each other messages, and the limits that keep them in check.
group: concepts
order: 7
---

Agents talk to each other by sending mail. It is how an orchestrator hands out work and hears back, how a reviewer tells an author what it found, and how any two agents coordinate without you relaying messages.

## Try it

Spawn an agent with a prompt like this one, and watch the two agents argue.

```text
You are an avid neovim fan, and need someone to debate. Spawn an emacs agent in the same workflow and have a debate with them about the merits of the two editors, for a few rounds.
```

<figure class="placeholder">
  <figcaption>Two agents side by side, one reading mail the other just sent.</figcaption>
</figure>

## How mail arrives

Each agent has a mailbox in weave's database. Mail never interrupts an agent in the middle of what it is doing. An agent picks up its mail when it finishes a turn, which is the natural moment to read and reply. If you need to stop an agent urgently, open its terminal and stop it yourself.

An idle agent is not finishing turns, so weave types a short note into its terminal saying mail is waiting, and repeats it until the mail is read. Weave never types over something a person is halfway through writing. It sets a short draft aside, delivers the note, and puts the draft back. A longer draft makes it wait. Agents that are waiting on you, or that you have paused, are left alone until they are free.

Mail to a stopped agent waits until it runs again. Mail cannot be sent to an archived agent. Mail that sits unread past the delivery deadline, set by [`mail.delivery_deadline`](../../config/#mail.delivery_deadline), expires.

Because mail arrives when a turn ends, an agent cannot wait for a reply inside a turn. An agent that sleeps or loops while waiting is the one thing that stops the reply from reaching it. Agents are told to end their turn and let the reply come to them.

## Reading mail yourself

The panel has no mailbox screen yet. You can read any agent's mail from the CLI or over MCP without marking it as read.

## The limit on a conversation

Two agents can only write each other so much, set by [`mail.max_exchange`](../../config/#mail.max_exchange). The limit counts both directions of every thread between the pair, so starting a new subject does not reset it. It exists to stop two agents looping on each other forever by accident.

When the limit stops a message, both sides find out. The sender gets an error, and the agent it was writing to is sent a notice that mail meant for it was stopped, so it does not sit waiting for a reply that will never come.

Mail you send and weave's own notices never count towards the limit. Deleting old mail between a pair gives them room again, which orchestrators can do when two agents genuinely need to keep talking.

## Families

A [child agent](../agents/#agents-that-work-together) belongs to a family: its parent and the parent's other children. Mail between agents stays inside families. A child can mail its parent and its siblings, and siblings can talk to each other directly so the parent does not have to pass every message along. An agent outside the family cannot mail the child, and the child cannot mail out.

You can always mail any agent yourself, and weave's own notices reach everyone.
