# Spike: periodic status reconciliation

Status: superseded. The reconciler shipped in the serve process, and the design
then went further than this doc proposed: the Claude Code hooks were removed
entirely and the reconciler became the sole source of state, reading it from the
pane every couple of seconds. See the "How state is worked out" section of
concepts.md for the shipped behaviour. This document is kept only as the record
of how the reconciler idea started; the hybrid it describes, hooks as the fast
path plus a reconciler safety net, is not what was built.

## Problem

An agent's state is advanced only by Claude Code lifecycle hooks: the prompt hook
sets thinking, the notification hook sets waiting, the post-tool-use hook moves
waiting back to thinking, and the stop hook sets done. This is an edge-triggered
state machine. Every transition depends on an event actually firing.

The trouble is that some things that end an agent's work do not fire any hook. The
clearest one is interrupting Claude with escape. When you abort a turn mid-thought,
Claude stops working, but the stop hook does not fire, because the stop hook is for
Claude finishing a response, not for the user cancelling one. So the last event the
store ever saw was the prompt hook that set thinking, and the agent shows as working
forever. The same gap exists for a crash, a machine going to sleep, or the tmux
session being killed from outside the panel. Any of these ends the work without an
event, and the state is left frozen at whatever it was.

This is not fixable with more hooks. There is no hook for "the user pressed escape."
The only way to know an agent is no longer working is to look at it, rather than to
wait to be told.

## The shape of the fix

The current design trusts the event stream completely. It never re-checks. That is
the same trap a purely event-driven system always falls into: if you miss one edge,
you are wrong until the next edge happens to correct you, and here the next edge may
never come.

The fix is to stop trusting the event stream as the only source of truth and add a
loop that periodically asks a simpler question about every active agent: is this
agent actually working right now? If the stored state disagrees with what is really
true, correct the stored state. The hooks stay as the fast path, because when they do
fire they are instant and precise. The loop is the safety net that catches the edges
the hooks missed.

This is the same reconciliation idea from the Kubernetes notes. The store row is the
resource. The desired signal is whatever the hooks last recorded. The observed truth
is what the agent's pane and transcript actually show right now. The loop reads the
observed truth on a timer and reconciles the row toward it.

## What we can actually observe

Everything the loop needs is on the same host as the agents, so it can look directly.

The cheapest signal is liveness. tmux can tell us whether an agent's session still
exists with has-session, which the code already wraps as SessionAlive. If the session
is gone, the agent is dormant, and it should not be counted as working no matter what
the row says.

The next signal is the pane content. The panel already captures panes for its preview
with capture-pane. A working Claude shows its spinner and an "esc to interrupt" line;
an idle Claude shows the input box; a blocked Claude shows a permission prompt. The
loop could capture the pane and classify it. This is the most direct read of "is it
working," but it is also the most fragile, because it depends on matching Claude's UI
text, which changes.

The most stable signal is the session transcript. Claude Code appends to a JSONL
transcript on disk as it streams, under ~/.claude/projects. If the file is still
growing, the agent is generating; if its modification time has not moved for several
seconds, it is not. The transcript path is handed to hooks on stdin as
transcript_path, alongside session_id, so we can capture and store it the same way we
already store the Claude session id. If we would rather not add a column, the path can
be derived from the working directory and session id, but that relies on the
undocumented naming scheme, so parsing and storing it is safer.

The transcript modification time cannot tell waiting apart from interrupted, because
both are idle. That is fine. We do not need to. Waiting is already a stable state that
the notification hook sets reliably, and an interrupt leaves the agent at thinking,
not waiting. The only thing the loop has to fix is a thinking agent that is not
actually thinking.

## Proposed loop

Run a reconciler as a goroutine inside the serve process. serve is the right home
because it already owns the database as the single writer, and it stays up in the
background even when the panel is closed, which is exactly when the status bar still
needs the counts to be correct. The panel is the wrong home because it is only open
sometimes.

On a timer, every five to ten seconds, the loop lists the non-done agents and for each
one:

If its tmux session is dead, it is dormant. It must not be counted as working. Either
demote it to done, or make the status count treat dead sessions as not working. The
status aggregate today counts the stored state directly, so a killed-while-thinking
agent inflates the working count until this is handled.

If its session is alive and the row says thinking, check whether it is really working,
using the transcript modification time as the primary signal and, optionally, the pane
capture as a confirmation. If it is not working, demote it. If a permission prompt is
on screen, demote to waiting; otherwise demote to done.

To avoid racing a single mid-render frame, require two consecutive idle observations
before demoting. A brief pause between tool calls should not flip a genuinely working
agent to done.

Because the loop writes state through the same store, everything downstream corrects
itself for free. The status bar, the panel icons, and the search all read the same
rows, so fixing the row fixes all of them at once.

## Open questions

Whether to add a transcript_path column or derive the path. Storing it is more robust;
deriving it avoids a migration.

The idle threshold and the number of confirmations before a demotion. Too eager and it
demotes a slow tool call; too lazy and the stuck state lingers.

Whether a demoted-to-done agent should be distinguishable from a normally finished one,
so the user can tell "this stopped on its own" from "I interrupted this."

## A related bug worth separating

While tracing the state path, a second issue surfaced that is not about missed edges.
Identity in the store is the tmux session name, not the stable id column. State is
keyed by name everywhere: the hook key is the session name, and every write is an
upsert on conflict of name. Soft-delete leaves the row in place with its name intact,
and auto-named agents pick their name by checking for a live tmux session, not by
checking the store. So a freed name can be reused, and the upsert on conflict of name
can write a new agent's events onto the old soft-deleted row without clearing its
deleted flag.

This is a plausible mechanism for the report that deleting one chat moved the working
state onto another. It should be reproduced on its own rather than folded into the
reconciliation work. Steps to test: create two agents with no title so they both
auto-name, note their ids in the database, delete one, create another, and check
whether any row's name collides, whether is_deleted is left set on a row that is now
being written to, and whether a state event lands on the wrong id. If it reproduces,
the direction is to key identity on the stable id and stop reusing names of rows that
still exist, deleted or not.
