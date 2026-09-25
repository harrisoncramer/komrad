# Cycle views instead of per-view focus keys

## Summary

Every place the keyboard can rest in the panel has its own key for getting there, and which key works depends on where the keyboard is now. The agent's pane is reached with C-a, the record with ctrl+t, a review thread with ctrl+f, and none of them works from everywhere. This RFC replaces those three with one pair of keys, C-a to go to the next view and C-q to go to the previous one, that works the same from the list, the record, a review thread and the agent's own pane. On the agents screen with an agent on the stage the cycle is list, agent, record. Because the cycle has three stops, every view is one press from every other, so the record is one press from the agent's pane rather than two presses that only work in order.

## Problem

Harry's words: "I think it'd be cooler if we didn't have distinct keybindings for switching to agent for agent focus, but instead had 'cycle view' forward and backward. Would be more intuitive since then you could swap back and forth, instead of remembering different keybindings across different views, also since you have to remember to do C-a and then C-t which means that info is basically only reachable from within the sidebar being focused."

The complaint is about reachability. Each focus key is read by a different program in a different place, so each one only works from some of the places the keyboard can be. What follows is every binding today that moves the keys or changes what the panel shows, with where it is defined and where it works.

### The keys that move the keyboard

C-a moves the keyboard between the sidebar and the agent's pane. It is not a panel binding. It is bound in the root key table of komrade's own tmux server by `PopupWindow.HoldFocusKey` in `internal/ui/popup.go`, as `if-shell` on the pane id: pressed in the sidebar it selects the agent's pane, pressed anywhere else it selects the sidebar. It is fixed rather than configured, since PR 505 took `panel.focus_key` out. `model.holdFocusKey` in `internal/ui/update.go` binds it only while `focusPane` answers a pane, which is only on the agents screen, only while the frame is split rather than solo, and only while the hovered agent's pane is swapped into the stage or, for a remote agent, while the stage is attached to it, which is what `Frame.FocusPane` answers. On every other screen, and on the agents screen with a workflow or a dead agent hovered, it is unbound and reaches whatever pane holds the keyboard as an ordinary C-a. The legend never names it, because the panel never answers it. `komrade doctor` checks that the user's tmux prefix is not C-a and that nothing else holds it in the root table.

ctrl+t moves the keys between the list and the record under it. It is `keybindings.details.focus` in the config, answered by `routeGlobalKey` in `internal/ui/update.go` through `jumpIntoDetails`, and handed back by the `actionDetails` arm of `detailsActions` in `internal/ui/details.go`, which also takes esc. It works on every screen that implements `detailed`: agents, archive, worktrees, schedules and the review. The record grows to its full height when it holds the keys, through `grownDetailsLines`; there is no separate key that expands it, so focusing the record is expanding it. RFC 0086 let ctrl+t work while a search is being typed and let the down key on the list's last row walk into the record. ctrl+t is read by the panel's own pane, so it does nothing from the agent's pane: there C-a is needed first. That is the C-a then ctrl+t that Harry describes, and the pair only works in that order.

ctrl+f moves the keys onto the review thread under the diff cursor and back. It is `keybindings.review.focus_thread`, read by `updateThread` in `internal/ui/reviewkeys.go`, which also takes esc. It works only on the review screen and only while the cursor is on a line carrying a thread.

f hands the keys to the sort strip over the list, `keybindings.tabs.focus`, on the three screens that draw one: agents, archive and worktrees. Inside the strip tab and shift+tab step between tabs and esc hands the keys back.

ctrl+g takes the keys into the messages region and back. It is `messagesFocusKey` in `internal/ui/messages.go`, a constant rather than a setting, and it is read in `route` ahead of forms and screens while a message is up.

enter on the agents list attaches to the hovered agent: `agentsScreen.openAgent` in `internal/ui/agents.go` selects its pane through `selectGuest`, or resumes it when it is not running. It is the one way into the agent's pane besides C-a, and it only works from the list.

T on the agents and schedules screens opens a shell in the info pane under the stage and selects it, through `openTerminal` in `internal/ui/terminal.go`. l and L page the logs into the same pane without moving the keyboard. e on the review opens the file under the cursor in an editor on the stage and selects it.

Getting out of the agent's pane, a shell or an editor without C-a means tmux's own pane keys or the mouse. On the review screen and every screen other than agents, C-a is not bound, so an editor or shell opened there can only be left that way or by quitting it.

### The keys that change what the panel shows

S opens the schedules, Z the archive, t the worktrees, s the search and d the review of the hovered agent, all from the agents list, and esc leaves each for the agents list. J on the worktrees screen jumps to the agent in the hovered directory. prefix+g on the user's own tmux server opens and hides the popup the panel lives in, bound by `BindPanelKey` in `internal/ui/popup.go`, and the same key on komrade's server detaches it. These change which screen is up rather than where the keys are inside one, and this RFC leaves them alone.

Whether the frame is split or solo is not a key. `reconcileSolo` in `internal/ui/column.go` zooms the sidebar over the whole window when the screen draws a block beside its list and the info pane is empty. The archive, the search and the worktrees are always solo. The schedules, and the agents screen with no pane on the stage, are solo until T or l puts something in the info pane, and then split with the info pane beside them. The review is solo until e opens an editor, since `reviewColumn` holds the window only while `pass.atEditor` is false. On every screen but the review the block is scrolled by the mouse wheel only and never holds the keys. On the review the block is the diff itself, and it does hold the keys: its cursor is what ctrl+f reads.

### Which views are reachable from where

From the list: the record by ctrl+t or by scrolling off the last row, the agent by enter or C-a, the sort strip by f, the messages by ctrl+g.

From the record: the list by ctrl+t, esc or scrolling off the first row, and the agent by C-a, which selects the agent's pane and leaves the record holding the panel's keys underneath.

From the agent's pane: the sidebar by C-a, landing on whichever of the list or the record last held the panel's keys. Nothing reaches the record from the agent's pane in one press, and nothing reaches the review thread or the sort strip at all.

From a review thread: the diff by ctrl+f or esc, and the record by ctrl+t in one press, since `routeGlobalKey` reads the details key ahead of the review's own keys. `focusDetails` does not clear the thread's focus flag, so the thread is still focused underneath and a second ctrl+t lands back on it rather than on the diff.

So the record is only reachable from the sidebar, the review thread is only reachable from the review diff, and the agent's pane is only reachable from the agents screen in a split frame. Every one of those rules is a fact about which program reads which key rather than about what a person wants to look at.

## Proposal

### What is in the cycle

A stop is a place that holds the keys and shows something about the hovered row. On the agents screen with an agent on the stage there are three: the list, the agent's pane and the record. On the review there are three: the diff, the thread under the cursor and the record. On any screen drawn solo, and on the agents screen with nothing on the stage, there are two: the list and the record.

On the search screen, which draws no record, there is one stop, and a cycle press does nothing there.

A stop that is not drawn is skipped. The record is skipped when `isDetailsDrawn` is false, which is when the hovered row has no fields or the screen has no room. The review thread is skipped when the cursor is on no thread, which is the case where ctrl+f is refused today with "no thread here to read". Moving into the record has to clear `threads.focus`, which `focusDetails` does not do today; otherwise the thread is still focused under the record and the next press is decided by a stale flag.

The sort strip, the messages region, the info pane and the block drawn beside a solo list are not stops. The strip is a control over the list rather than a view of the row, and RFC 0086 kept it off the list's scroll-through for the same reason. The messages region comes and goes with a failure, and a cycle whose length changes when an error lands is one nobody can learn. The info pane holds a shell, a log or an editor that someone opened on purpose and leaves by quitting. The solo block is scrolled by the wheel and could be made a stop later, which would give it keyboard scrolling, but that is a feature of its own.

### The order

Forward goes list, agent, record, and back to the list. Backward goes the other way. On the review forward goes diff, thread, record.

With three stops and a key in each direction, every stop is one press from every other. The order decides which press. Putting the agent right after the list keeps what C-a does from the list today, which is the most common move in the panel. Putting the record right after the agent is Harry's case: from the agent's pane, C-a lands on its record. From the agent's pane back to the list is C-q. From the record back to the agent is C-q.

The cost is the round trip. Today C-a is a toggle, so the list to the agent and back is C-a, C-a. Under this order it is C-a then C-q, and C-a, C-a from muscle memory ends on the record rather than the list. Spatial order, list then record then stage, would read more naturally on the screen and makes the round trip C-q then C-a. With three stops the commonest round trip needs both keys whichever order is chosen, so the only choice is which half keeps C-a. This order keeps it for the way in, which is the half people press first and the half the list's legend will show them.

With two stops, forward and backward are the same move, and either key toggles the list and the record, which is what ctrl+t does today.

### The keys

Forward is C-a, which is already the panel's one root binding, already checked by doctor, and already what people press to leave the agent's pane. Backward is C-q.

The keys have to be root bindings on komrade's tmux server, because the agent's pane gives every byte to the CLI running in it and only the root table sees a press there. That rules out tab and shift+tab, which would read best inside the panel: shift+tab is Claude's mode switch and tab is completion in every shell. The keys also have to be control keys that tmux spells the same way it receives them, which rules out shifted control keys. C-q was chosen because it is flow control or quoted-insert in shells and is typed rarely. The cost of each root binding is that every agent and shell in the panel loses that key while an agent is on the stage, which is what RFC 0033 weighed. C-a already pays that cost today; C-q adds a second key paying it.

Both keys stay fixed rather than configured, for the reason PR 505 fixed C-a: a configurable root binding can take a printable key from every pane. They become two constants beside `FocusKey` in `internal/ui/popup.go`.

### How a press reaches the panel

Today the root binding decides in tmux, with `if-shell` on the pane id. Under this proposal tmux decides nothing. Both keys are bound as `send-keys -t <sidebar> C-a` and `send-keys -t <sidebar> C-q`, so the press lands in the panel's own pane whichever pane it was made in, without moving the keyboard. Nothing in komrade does this today. The nearest thing is `passKeyToPanel` under `Client.TakePanelKeys` in `internal/tmux/keys.go`, which binds `if-shell` on the panel's pane id with a bare `send-keys` of the key into the pane it was pressed in, and so only passes a press that was already made in the panel.

The panel then works out where the keys are and moves them. It already tracks the record and the review thread itself. `frame.HasKeys` and `keysElsewhere` are one bool, sidebar or not, so they cannot tell the agent's pane from the info pane. The press has to ask tmux for the window's active pane id and compare it with the stage and the info pane. That is one tmux call per cycle press. Moving to the agent's pane is the `selectGuest` that enter already calls. Moving into the sidebar is `SelectPane` on the sidebar, and then focusing or blurring the record.

RFC 0033 rejected a key that typed itself into the panel. What made that one expensive was that it jumped screens, so it needed a stash of where to come back to and a growing list of exceptions. This one never changes screen and needs no memory: the next stop is a function of the stop the keys are on now and the stops that are drawn. It does bring back the other cost 0033 named, the translation between tmux's name for the key and bubbletea's, C-a against ctrl+a, since tmux sends the press and the panel matches it. `tmux.SpellKey` already does that translation for the keys `TakePanelKeys` passes, so it is one mapping komrade keeps anyway rather than a new one.

The keys are bound while an agent's pane is on the stage, as C-a is today, and unbound otherwise so the panel reads them as ordinary presses on its own pane. That keeps the review's editor and every shell on a solo screen free of both keys.

### Modes that take the keyboard

Once tmux sends every press to the panel, a press made in the agent's pane arrives in whatever mode the sidebar is in, and `route` would swallow it before any cycle arm: a held write answers only esc, a form reads C-a as beginning-of-line, and an open search closes. Today's `if-shell` leaves the agent's pane in all of those. So a cycle press whose keys were in another pane is answered ahead of every mode in `route`, right after the mark read. It moves the keyboard to the sidebar and leaves the mode standing: a form, a held write, a search being typed or the composer is where the keys land, and the stop the cycle would have chosen is not applied. That keeps the key out of the agent's pane working in every state the sidebar can be in, which is the one promise C-a makes today.

A press made in the sidebar itself is read where modes allow. A form, the review composer and the review release take every key and keep doing so: the cycle keys do nothing there, and inside a form C-a stays beginning-of-line. A search being typed closes on a cycle key and keeps its pattern, as ctrl+t does after RFC 0086. The sort strip and the messages region hand the keys back to the list first, then move on, so a cycle press from either lands where it would from the list. A press made in the info pane lands on the list going forward and on the record going backward, the same as a press made in the agent's pane minus the agent.

### What goes

`keybindings.details.focus` goes, leaving `keybindings.details` with only `copy`. `keybindings.review.focus_thread` goes. The `if-shell` form of the root binding in `HoldFocusKey` goes. `jumpIntoDetails` stays as what the forward key calls on the record, and `focusThread` stays as what it calls on the thread. esc keeps leaving the record and the thread, since esc is the way out of everything in the panel rather than a focus key. enter keeps attaching, since it also resumes a stopped agent and starts an orchestrator. f, ctrl+g, T and the screen keys are unchanged.

### The legend and the drift test

The legend gains one row on every screen with more than one stop, which is every screen but the search, `C-a/C-q` with the description "next/prev view", in the place `detailsBinding` holds today in `listHelp` and in the review's help. That is the first time the key out of the agent's pane appears anywhere in the panel, which is half of Harry's complaint. The row is only drawn in the expanded legend, as the details row is now.

`TestHelpKeyDrift` in `internal/ui/help_test.go` checks only keys read from `config.DefaultKeybindings`, so fixed keys like ctrl+g are outside it today. The two cycle keys should be added to its pressable set. They are answered on every screen with more than one stop and belong in those legends. On the search screen the panel does not answer them, so `IsKeyTaken` is false there and the test skips them without an excuse. `globalArms` in `internal/ui/keyconflict.go` gains one arm for the pair, so a user binding either key to something else is refused at load. The doctor's keybindings check in `internal/doctor/keys.go` checks C-q the same way it checks C-a.

### Split and solo

In a split frame on the agents screen with an agent on the stage the cycle has three stops and uses the root binding. In a solo frame it has two stops, the keys are unbound in tmux, and the panel reads them as ordinary presses. Three screens can also be split with no agent on the stage: the schedules and the agents screen with the info pane open, and the review with its editor open. There the cycle is still the sidebar's two or three stops, the keys stay unbound in tmux as C-a is today, and a press made in the shell, log or editor goes to that program. Leaving those panes stays tmux's pane keys, the mouse, or quitting what runs in them. Moving the cursor from an agent with a pane to a workflow row turns one into the other, which `holdFocusKey` already does for C-a on every update. Nothing about the cycle changes the zoom. A cycle press never solos or splits the frame; it only moves the keyboard and the panel's own focus flags.

## Alternatives

Keep C-a as the only root key and make it the forward cycle, with no backward key in the agent's pane. It costs agents no second key. From the agent's pane the list becomes two presses away, which is the same shape of problem moved to a different view, and back-and-forth between two views would mean going the long way round.

Use tab and shift+tab inside the panel and keep C-a only for leaving the agent's pane. That reads best in the sidebar and takes nothing new from the agents. It is also exactly what Harry asked to get rid of: two sets of keys depending on which pane you are in.

Spatial order, list then record then agent. It matches what is on screen. It makes the move from the list to the agent the backward key, and that is the move made most often.

Put the sort strip, the messages region and the info pane in the cycle too, so there is one key for all focus. The cycle would change length whenever a message appeared or a terminal was opened, and the stops that matter would move under a person's fingers. Each of those already has a key that names what it does.

Keep the `if-shell` binding and add the direction to it. tmux cannot see whether the record holds the panel's keys, so the binding could only ever choose between two panes, and the record would still need the panel to decide. Sending the key to the panel and letting it decide is one path instead of two.

## Risks and open questions

C-q has not been measured against Claude and Codex. Before building, press it in both CLIs and in the shells the panel opens, and read what it does. If either binds it to something people use, the fallback is to keep C-a alone and accept the two-press trip from the agent's pane to the list.

A pane in copy mode has its own key table, so neither key fires there, as RFC 0033 found for C-a. A user whose tmux prefix is C-q loses the backward key the way one whose prefix is C-a loses the forward key today; doctor says so for both.

The press lands in the panel before tmux reports the focus change that follows it, and the panel reads where the keyboard is at that moment. If `frame.SyncKeys` on every cycle press turns out to be too slow, the panel can read `keysElsewhere` instead, which is only wrong for the moment between a mouse click in another pane and the blur it causes.

The review draws its diff itself and has no agent on the stage, so on the review the keys are never root bindings and never reach an editor opened with e, even while that editor splits the frame. That keeps nvim's C-a working, and it means the editor is still left by quitting it.
