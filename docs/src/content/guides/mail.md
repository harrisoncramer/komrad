---
title: Mail
description: How agents send each other messages, and the limits on it.
group: concepts
order: 7
---

Agents talk to each other by sending mail. Use it to coordinate several agents, or to let one agent hand work to others and hear back.

Mail is always delivered. It is written to Postgres, and a job to deliver it is queued in the same transaction.

## Try it

Spawn an agent called "neovim fan" with this prompt.

```text
You are an avid neovim fan, and need someone to debate. Spawn an emacs agent in the same workflow and have a debate with them about the merits of the two editors, for a few rounds.
```

It spawns a second agent, and the two send messages back and forth.

## How delivery works

Each agent has a mailbox. An agent checks it at the end of each turn, so mail never interrupts an agent mid-turn. To stop a running agent urgently, open its pane and halt it yourself.

An idle agent ends no turns, so weave types a short nudge into its pane telling it mail is waiting. A nudge can be swallowed, so it is sent again every minute while the mail is still unread. A dormant agent gets the nudge when it wakes up. Mail cannot be sent to archived agents.

Weave never types into a prompt that already holds text, because the key that sends the nudge would also send what someone was typing. That agent is nudged once its prompt is empty. If that never happens, the mail expires unread.

Because mail arrives when a turn ends, an agent cannot wait for mail inside a turn. Sleeping or looping while waiting for a reply is exactly what stops the reply arriving. Agents are told to end their turn instead.

## Reading a mailbox

The panel has no mailbox screen. Read an agent's mail with `weave rpc list-mail`, or the `list_mail` tool over MCP. Both show the mail without marking it read.

## The exchange limit

Two agents may only write each other so much mail. The limit counts both directions and every thread together, so starting a new subject does not get around it. The default is forty messages, which is twenty each way.

When the limit refuses a send, everyone involved hears about it. The sender gets an error. The agent it was writing to is told its mail was stopped, so it does not wait forever for a reply. And one row is flagged so a person notices. That is the parent when the two agents are siblings, since the parent can break the loop, and the sender otherwise.

Weave's own notices never count against the limit, because they come from no agent.

Deleting mail lowers the count, because the count is read from the mail that still exists. That is how a pair that really needs to keep talking gets more room. Agents can do this themselves, since `delete_mail` is not limited to the caller's own mailbox. So the limit stops conversations that run away by accident, not ones set on getting past it.

## Children and families

A [child agent](../agents/#children) may only mail its family, which is its parent and its parent's other children. Anything else is refused, and the error says to reply to the parent instead.

Two children of the same parent can mail each other directly, so the parent does not have to relay every message. Children of different parents are not family.

The rule limits what a child sends, not what it receives. Weave's own notices and mail you send yourself reach any child.

Siblings are held to the same exchange limit as any pair. When it refuses a send between them, the parent is the row flagged, so they cannot loop without the parent finding out.
