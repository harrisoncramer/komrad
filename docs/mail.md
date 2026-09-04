# Mail

Agents communicate with each other in weave by sending mail. Each agent will, at the end of it's turn, check it's mailbox for new mail. You can use mail to coordinate or orchestrate across multiple agents.

Because weave uses a durable datbase and job runner, mail is guaranteed to be delivered.

## An Example

Try spawning an agent, called "neovim fan" with the following prompt:

```text
You are an avid neovim fan, and need someone to debate. Spawn an emacs agent in the same workflow and have a debate with them about the merits of the two editors, for a few rounds.
```

The agent will spawn a second agent, and the two will send messages back and forth to determine who is better.

## How it Works

Each agent has a mailbox in Postgres. When agents need to send mail, they write a mail item to their recipient's mailbox, and enqueue a job to ensure the mail is delivered, in the same transaction.

Agents check their mailboxes for any pending mail at the end of each turn. Mail is therefore not an "interrupt" activity. If you need to urgently interrupt a running agent and tell it something, you should switch into the CLI view and halt the process.

If an agent is dormant when a mail item is written, it'll get "nudged" the next time it wakes up to check it's mailbox. Mail cannot be send to agents that have been archived.

An idle agent ends no turn, so the nudge is the only way mail reaches one. Keys typed at a pane can be swallowed, and an agent that never saw the line has no reason to do anything, so a nudge stands for a minute and is then sent again while the mail is still unread.

A prompt already holding text is left alone, because the newline that submits the nudge would send whatever somebody was halfway through typing along with it. Such an agent is nudged once the prompt is empty again, and its mail expires unread if that never happens.

## Reading a mailbox

Press `m` on an agent in the panel to read its mail. The sidebar draws the whole
correspondence as one list in the order it happened, what came in and what went out together,
because a conversation split into two lists by direction puts a reply in one of them and the
message it answers in the other.

An arrow on each row says which way it went and a glyph says what became of it: taken, still
waiting, or never read before its deadline ran out. A rail down the left ties a reply to the
message it answers, and runs past whatever landed in the middle of the thread, so a
conversation is readable against the mail that arrived while it was running.

The stage draws the whole thread the cursor is standing in, with that message marked, rather
than the one message on its own. `t` and `T` step through the thread's own messages, over
anything that landed between them. `ctrl+t` moves the cursor into the record under the list,
which carries the ids, the deadlines, the kind, and what the thread is standing on: a last
word that went out and was never taken is a stalled handoff.

## Children and their families

A [child agent](agents.md) may only mail inside its own family, which is its parent and that parent's other children. Anything else is refused, and the refusal says to reply to the parent instead.

The unit is the family rather than the pair, so two children of one parent can talk directly. That saves the parent having to relay every message between them, which would spend its context on being a switchboard. Two children of different parents are not in one family, and a child and an unrelated agent are not either.

The rule bounds what a child may do rather than what may be done to it. Weave's own lifecycle mail reaches any child, and so does mail you send yourself, since neither carries a sending agent.

A thread between two children is counted against the same hop limit as any other, so a pair cannot loop indefinitely without their parent.

Because delivery happens as a turn ends, an agent cannot wait for mail inside a turn. Sitting in a sleep or a loop waiting for a reply is the one thing that stops the reply arriving. Agents are told to finish their turn instead.

> Note, in the future, mail may be supplanted by or supplemented with [channels](https://code.claude.com/docs/en/channels), which push events into a running session rather than waiting for it to end a turn. They are a research preview today, plugin-only and behind an Anthropic allowlist, so the Stop hook remains how mail arrives.
