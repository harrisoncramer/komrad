# Mail

Agents communicate with each other in weave by sending mail. Each agent will, at the end of its turn, check its mailbox for new mail. You can use mail to coordinate or orchestrate across multiple agents.

Because weave uses a durable database and job runner, mail is guaranteed to be delivered.

## An Example

Try spawning an agent, called "neovim fan" with the following prompt:

```text
You are an avid neovim fan, and need someone to debate. Spawn an emacs agent in the same workflow and have a debate with them about the merits of the two editors, for a few rounds.
```

The agent will spawn a second agent, and the two will send messages back and forth to determine who is better.

## How it Works

Each agent has a mailbox in Postgres. When agents need to send mail, they write a mail item to their recipient's mailbox, and enqueue a job to ensure the mail is delivered, in the same transaction.

Agents check their mailboxes for any pending mail at the end of each turn. Mail is therefore not an "interrupt" activity. If you need to urgently interrupt a running agent and tell it something, you should switch into the CLI view and halt the process.

If an agent is dormant when a mail item is written, it'll get "nudged" the next time it wakes up to check its mailbox. Mail cannot be sent to agents that have been archived.

An idle agent ends no turn, so the nudge is the only way mail reaches one. Keys typed at a pane can be swallowed, and an agent that never saw the line has no reason to do anything, so a nudge stands for a minute and is then sent again while the mail is still unread.

A prompt already holding text is left alone, because the newline that submits the nudge would send whatever somebody was halfway through typing along with it. Such an agent is nudged once the prompt is empty again, and its mail expires unread if that never happens.

## Reading a mailbox

The panel has no mailbox screen. Read an agent's mail with `weave rpc list-mail`, or with the
list_mail tool over MCP, either of which lists it without claiming it.

## The exchange limit

There is a limit on how much mail two agents may write each other. It counts the pair in both
directions and over every thread, so a send with no reply_to is refused exactly like a reply, and
opening a new subject is not a way around it. Forty is the default, which is twenty rounds each
way.

That number is chosen for the cost of being wrong in each direction rather than for tidiness. The
longest exchange the panel has recorded is fifteen messages, and it was a review that finished, so
a limit of twenty would sever a review one round longer than the worst real one at the moment it
was trying to conclude, and the verdict is the thing that would be lost. A limit set too high
costs one long loop that somebody notices anyway. A limit set too low costs a conclusion.

It counts the pair rather than a thread on purpose. Weave used to cap the hops down a reply
chain, and that never bounded anything, because a send without a reply_to opens a fresh thread
and the count started again at one. None of the ways that happens is misbehaviour: an agent that
forgets to set reply_to, a person re-prompting one side mid-cycle, a reviewer opening a new
subject after a rebase. Most mail carries no reply_to at all, so the thread cap was counting a
structure that most mail does not have, and in the panel's whole history it never once refused
anything.

A refusal is told rather than silent, which is what keeps it from stranding the work. The sender
learns from the error it gets back. The agent that was written to is sent a notice saying mail
meant for it was stopped, so it is not left waiting on a reply that a limit has quietly
prevented. And one row is marked for attention so the person hears about it: the
[family](agents.md)'s parent where there is one, since that is the row a person is looking at and
the agent able to break a loop between two of its children, and the sender itself where there is
not.

Weave's own mail is never refused. A lifecycle notice carries no sending agent, so it is not one
half of a pair and does not spend the allowance either.

Deleting mail lowers the count, since the count is read off the rows that still exist. That is an
escape hatch rather than an oversight, and it is how a pair that genuinely needs to keep talking
is given room. It is worth being plain that the agents themselves can use it: `delete_mail` is
not scoped to the caller's own mailbox, so a capped pair that means to carry on is able to. The
limit therefore bounds an exchange that runs away by accident, which is the way exchanges
actually run away, rather than one determined to get past it.

## Children and their families

A [child agent](agents.md) may only mail inside its own family, which is its parent and that parent's other children. Anything else is refused, and the refusal says to reply to the parent instead.

The unit is the family rather than the pair, so two children of one parent can talk directly. That saves the parent having to relay every message between them, which would spend its context on being a switchboard. Two children of different parents are not in one family, and a child and an unrelated agent are not either.

The rule bounds what a child may do rather than what may be done to it. Weave's own lifecycle mail reaches any child, and so does mail you send yourself, since neither carries a sending agent.

Two children of one parent are held to the same exchange limit as any other pair, and when it refuses a send between them their parent is the row marked for attention, so they cannot loop indefinitely without the parent hearing about it.

Because delivery happens as a turn ends, an agent cannot wait for mail inside a turn. Sitting in a sleep or a loop waiting for a reply is the one thing that stops the reply arriving. Agents are told to finish their turn instead.

> Note, in the future, mail may be supplanted by or supplemented with [channels](https://code.claude.com/docs/en/channels), which push events into a running session rather than waiting for it to end a turn. They are a research preview today, plugin-only and behind an Anthropic allowlist, so the Stop hook remains how mail arrives.
