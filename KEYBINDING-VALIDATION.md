# What a bad keybinding config does today

Weave validates nothing about a keybinding. `Config.validate` checks the log level, the two
ceilings and the layout, and stops there. Nothing checks that a key string is one bubbletea can
ever deliver, that two actions on a screen do not take the same key, that a key the panel
answers itself has not been handed to an action, or that `focus_key` is a key tmux will bind.
Every case below loaded without a word and left the panel quietly doing the wrong thing.

Everything here was measured against a throwaway instance on tmux socket `weave-test`, Postgres
on 5461 and the server on 8790, with two haiku agents in it. Presses that had to go through
tmux's key tables were typed into a real client attached to that session from a third tmux
server, because `send-keys` skips the key tables and would have proved nothing.

## The two key languages

The panel's own bindings are bubbletea key strings, matched by exact string equality against
`tea.KeyMsg.String()`. `focus_key` is a tmux key name, handed to `bind-key -n`. They look alike
and are not: `ctrl+a` is only the first, `C-a` only the second. Nothing in the config file or
its documentation says which is which, and neither field is checked against its own language.

## Keys no press can ever match

A binding whose string bubbletea never produces is accepted, and the action is simply gone.
There is no press that reaches it and nothing anywhere reports it.

Measured with `review = ["PgUp"]` and `rename = ["escape"]`: pressing PageUp did nothing,
pressing Escape went to the back action, and the review screen was unreachable by any key for
the rest of the session.

The whole class: a name in the wrong case (`PgUp`), a name spelled out (`escape`, `space`,
`return`), a tmux spelling in a panel field (`C-d`, `<C-d>`), a modifier combination no terminal
sends (`ctrl+shift+q`), a function key past f20, the empty string, and a name padded with
spaces, which is not trimmed.

Two of these are worse than the rest because they look correct. bubbletea reports the carriage
return as `enter` and the tab as `tab`, so `ctrl+m` and `ctrl+i` can never match. Bound to
rename, a real `C-m` press ran the open action instead, which is what `enter` is bound to.

## A two-character binding

`top = ["aa"]` is accepted. Typing it delivers two separate presses of `a`, neither of which is
`aa`, so the binding is dead. It is worse than dead when the first character is bound to
something else: with `archive = ["a"]` in the same file, the first `a` opened the archive
confirmation. So the file reads as though jumping to the top costs two presses, and pressing it
archives the hovered agent.

## One key on two actions

The dispatch hands a press to the first arm in the screen's table that answers it, so a key
taken twice runs whichever action the table happens to reach first, and the other is
unreachable. Nothing warns, and the legend draws both.

Measured on the agents screen:

`new = ["j"]` against the movement key. Pressing `j` moved the cursor. The new-agent form could
not be opened at all.

`archive = ["a"]` against `workflows.adopt`, which is `a` by default. On a workflow row the
adopt form opened; on an agent row the archive prompt opened. Both actions were guarded, so
which one you get depends on the row under the cursor. That is the least bad version of this
bug, and it is still not something a person can predict from the file.

`help.toggle = ["ctrl+c"]` against `agents.quit`, which is `ctrl+c` by default. The global help
arm is read before the screen's table, so the legend read `C-c show help`, pressing it toggled
the legend, and the panel could no longer be quit by its own quit key.

The default config already has keys taken twice on one screen: `z` by three actions on the
agents list, `r` by two, `esc` by three, `d` and `r` on the archive. Those all work because
their guards are disjoint, so a validation rule cannot simply reject a repeated key. It has to
ask whether the two actions can be live at the same time, or restrict itself to the pairs where
at least one arm is unguarded, which is the case every measurement above.

## Keys the panel answers itself

Some keys are compared as literals rather than read from the config: `esc`, `enter`,
`backspace`, `ctrl+c`, the space, `tab`, `shift+tab`, `ctrl+g` in the message region, and `<`,
`[`, `>`, `]` in the mail. An action bound to one of those is shadowed wherever that arm is
read, which is per-mode and so not visible from the config at all.

## Keys tmux eats before the panel sees them

The panel's tmux server loads the user's own `tmux.conf`, so any root-table binding in it takes
that key from every pane, the panel's included. On this machine the user binds `C-d` and `C-u`
in the root table. With `search = ["ctrl+d"]`, a real `C-d` press put the panel's pane into
copy-mode and the search never opened.

Weave can see this: `tmux list-keys -T root` names every key the server holds. Nothing reads it.

## focus_key

`focus_key = "ctrl+a"` was accepted, and tmux refused the bind with `unknown key: ctrl+a`. The
refusal is swallowed by `Client.BindRootKey`, which logs a warning and returns nothing, so the
focus key silently did not exist. Nothing in the panel or in doctor says so.

`focus_key = "j"` was accepted, and this one is destructive. The key is bound in the root table
while an agent's pane is staged, so it is taken from every pane on weave's server. A real `j`
press in the panel jumped focus to the agent instead of moving the cursor, and a real `j` press
in the agent's pane bounced focus back to the panel instead of reaching the agent's CLI. A
letter, a digit or any printable key does this.

`focus_key` set to the user's tmux prefix is accepted and does nothing, because tmux reads the
prefix before it reads the root table. Measured with the prefix at `C-]` and `focus_key = "C-]"`:
the root binding was installed, and pressing the key still entered prefix mode.

## An empty list

`down = []` is accepted and yields an empty binding, so movement is gone. The doc comment on
`Keybindings` says a binding left unset falls back to its built-in default, and an empty list is
how TOML writes "unset". Either it should take the default or it should be refused; today it
silently removes the action, and `agents.quit = []` removes the quit the same way.

## What is not a bug

`ctrl+z` is delivered to the panel rather than suspending it, so an action bound to it works.
`pgup` and `pgdown` are real bubbletea names and are fine; it is only the casing that is not.
A non-string element in the list, such as `down = [1]`, is coerced to `"1"`, which is a
pressable key.

## The suite

Every case above is a failing test, and the suite is red on purpose.

`config/keys_test.go` asserts `Load` refuses each bad file, which is where a fail-early check
belongs: unreachable names, a key listed twice, one key on two live actions, a key the panel
answers itself, the empty list taking the default, and the two `focus_key` classes.

`internal/ui/keyshadow_test.go` proves the two consequences a config check would prevent: a
rebind shadowed by movement, and quit lost to a help rebind.

`internal/doctor/keys_test.go` asserts doctor makes a `keybindings` check at all, and that it
reads the tmux root table and the prefix, which is the half of this that cannot be judged from
the file alone.
