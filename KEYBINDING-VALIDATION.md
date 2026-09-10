# Keybindings: what a config can break, and what we can hold it to

Weave validates nothing about a keybinding. `Config.validate` checks the log level, the two
ceilings and the layout, and stops. Nothing checks that a key string is one bubbletea can
deliver, that two live actions do not take the same key, that a key the panel answers itself has
not been given to an action, or that `focus_key` is a key tmux will bind. Every case here loaded
without a word.

Measured against a throwaway instance on tmux socket `weave-test`, Postgres 5461 and the server
on 8790, with haiku agents in it. Anything that had to pass through tmux's key tables was typed
into a real client attached to that session from a third tmux server, because `send-keys` skips
the key tables and would have proved nothing.

## Three key spaces, not one

The config looks like one setting with many rows. It is three unrelated languages, and knowing
which is which is most of the reason these bugs exist.

The panel's own bindings, `keybindings.*`, are bubbletea key strings. They are matched by exact
string equality against `tea.KeyMsg.String()`, so the only valid values are the names bubbletea
prints and single runes. There is no parser and no normalisation.

`panel.focus_key` is a tmux key name, handed to `bind-key -n` on weave's own tmux server. It is
not a panel binding at all, and it is not scoped to the panel: a root binding applies to every
pane on that server, which is the panel and every agent.

The key that opens the panel is a tmux option on the user's own server rather than a config key.
That one is bound with `BindKeyChecked` and the refusal is surfaced. It is the pattern the other
two should follow.

## Four readers of one binding

Within the panel, a configured key is read in four places with different precedence, and a
person reading the config file cannot see any of it.

`routeGlobalKey` answers the help, logs, terminal and column keys before the screen's own table
is reached. A screen's `actionSpec` table answers in table order, with each arm optionally
guarded by a `when`. Some modes compare keys as literals and never consult the config at all:
the filter, the search box, the marks, the details focus, the message region, the compose
window. And the form path reads `Form.Submit` before handing the key to huh, which is why a
printable form key is destructive.

## The bugs

Keys no press can produce are accepted, and the action is silently gone. There is no press that
reaches it. Measured with `review = ["PgUp"]` and `rename = ["escape"]`: PageUp did nothing,
Escape went to the back action, and the review screen was unreachable by any key for the rest of
the session. The class is names in the wrong case, names spelled out (`escape`, `space`,
`return`), tmux spellings in a panel field (`C-d`), combinations no terminal sends
(`ctrl+shift+q`), function keys past f20, the empty string, and names padded with spaces, which
are not trimmed. Two of them look right and are not: bubbletea reports those keys as `enter` and
`tab`, so `ctrl+m` and `ctrl+i` never match, and a real `C-m` press bound to rename ran open
instead.

A two-character binding is accepted and is only reachable by paste. Typing `aa` delivers two
presses of `a`, neither of which equals `aa`, while a paste of `aa` arrives as one message and
does match. So the binding works by pasting and never by typing. It is worse when the first
character is bound elsewhere: with `archive = ["a"]` in the same file, the first press archived
the hovered agent.

One key on two live actions runs whichever the table reaches first, and the other is
unreachable. `new = ["j"]` lost to movement and the new-agent form could not be opened at all.
`help.toggle = ["ctrl+c"]` beat quit, because the global arm is read before the screen's table,
so the legend read `C-c show help` and the panel could not be quit by its own quit key. The
default config already shares `z`, `r`, `esc` and `d` between guarded actions on one screen, so
a rule that rejects any repeated key would make the shipped defaults illegal.

A printable key in the form group hijacks typing. `updateForm` reads `Form.Submit` before the
key reaches the field, so with `submit = ["s"]` the letter cannot be typed into any prompt, and
typing it submits whatever is in the form. Measured on the new-agent form: typing `test`, then
`a`, then `s` created a live agent whose prompt was `testa`.

The paired actions decide which half ran by reading one of their two keys, with the other as the
fallthrough. So a key in both `nav.up` and `nav.down` always moves down and up becomes
unreachable, and the same holds for the two filter matches. A shared `top` and `bottom` latches
the doubled top press instead.

Marks own the press after them, by design. Any key given to `marks.set` or `marks.jump`
therefore swallows the next key as a mark letter, and mark letters are restricted to a to z, so
anything else reports an error. Bound to a movement key this eats the keyboard a press at a
time.

Keys the panel compares as literals cannot be rebound onto: `esc`, `enter`, `backspace`,
`ctrl+c`, the space, `tab`, `shift+tab`, `ctrl+g` in the message region, and `<`, `[`, `>`, `]`
in the mail. An action bound to one of those is shadowed wherever that arm is read, which is
per-mode and invisible from the file.

Keys the user's own tmux holds are eaten before the panel sees them. The panel's tmux server
loads the user's `tmux.conf`, and this machine root-binds `C-d` and `C-u`. With
`search = ["ctrl+d"]`, a real press put the panel's pane into copy-mode and the search never
opened. `tmux list-keys -T root` names every key the server holds, and nothing reads it.

`focus_key` is the worst of the three spaces. `ctrl+a` is accepted and tmux refuses the bind
with `unknown key: ctrl+a`; `Client.BindRootKey` swallows that into a log warning, so the focus
key silently does not exist. A printable focus key is destructive: with `focus_key = "j"`, a
real `j` in the panel jumped focus instead of moving the cursor, and a real `j` in the agent's
pane bounced focus back instead of reaching the agent's CLI. `focus_key` set to the user's tmux
prefix installs and never fires, because tmux reads the prefix before the root table, measured
with the prefix at `C-]`.

The focus key also leaks. It is bound on a tmux server that outlives the panel, and nothing
unbinds the key the previous config named or the key the panel held when it died. Four stale
bindings accumulated on the test server in one afternoon of config edits, `C-a` among them,
which is the default. Each one keeps taking its key from every pane forever: with a stale `C-y`
bound and no panel alive, a real `C-y` typed into a pane running `cat` never arrived, while a
`C-g` in the same pane echoed as `^G`.

A focus key that collides with a panel binding is state-dependent. With `focus_key = "C-t"` and
the default `details.focus = ctrl+t`, the key moved the details cursor while no agent was staged
and jumped panes while one was, because the root binding is only held over a staged pane.

An empty list unbinds silently. `down = []` yields an empty binding and movement is gone, while
the doc comment on `Keybindings` says an unset binding falls back to its default, and an empty
list is how TOML writes unset. `quit = []` removes the quit the same way.

Two existing tests already assert the invariants that matter, `TestNoScreenBindsOneKeyTwice` and
`TestNoLegendIsWiderThanThePane`, and both run only against the defaults. The first goes red the
moment a user config is passed through it. The second holds up: long key names and wide runes
draw fine, so legend width is not part of this problem.

## What is not a bug

`ctrl+z` is delivered to the panel rather than suspending it. `pgup` and `pgdown` are real
bubbletea names; only the casing is wrong. A non-string element such as `down = [1]` is coerced
to `"1"`, which is a pressable key. Sharing a key between two guarded actions works and is how
the defaults put three actions on `z`.

## What we can and cannot support

Three tiers, by what can actually be decided and where.

Decidable in the loader, with no environment. That a key string is one bubbletea can produce.
That a list does not repeat a key. That a key is not in the reserved literal set. That a
printable key is not given to a binding read inside a text field, which is the form group and
the compose keys. That two unguarded actions on one screen do not share a key, and that the two
halves of a pair do not. That an empty list means the default rather than nothing. That
`focus_key` is a tmux key name and is not printable. All of these are cheap, and all of them
fail early with a message naming the setting, which is what the unknown-key check already does.

Decidable only with tmux in front of us, which means the panel at startup and a doctor check.
Whether tmux will actually bind `focus_key`, by binding it with the checked call rather than the
silent one. Whether `focus_key` is the prefix. Whether any panel binding is a key the user's own
root table holds. This is the half that cannot live in the config file, and the right home is a
`keybindings` doctor check plus a startup failure for the focus key.

Not decidable statically, and not worth trying. Whether two guarded actions can be live at the
same time. The drift test already models this by pressing keys against a built model, and that
is the shape any collision rule for guarded arms should take.

Not supportable at all, and my recommendation is to stop offering it. A chord, meaning any
binding longer than one key: the dispatch is string equality on a single message, so `"aa"`
cannot work by typing, and making it work needs a pending-key state machine like the one the
marks already have. Reject strings that are not a single key until someone wants to build that.
The form and compose bindings: they are read inside a text field, where every printable key is
someone's typing and the destructive case creates agents, so either fix them to control keys or
restrict them to non-printable keys. And `focus_key` as free text: it is a tmux root binding
that reaches every agent, so it should be an allowlist of a few control keys, or a setting that
only turns the feature on and off, with the key itself fixed at `C-a`.

The rest of `keybindings` is worth keeping. Rebinding a key on a list screen is the ordinary
case, it is what the file is for, and every measurement above is caught by the loader-level
checks in the first tier.

## What was built

The findings above are the state of the tree before this branch. What follows is what it does
now, which is the plan agreed after them.

The focus key is no longer a setting. It is fixed at `C-a` in the panel, `panel.focus_key` is
gone from the config, and the whole class of bugs around it went with it: nothing to spell in
the wrong language, nothing printable to bind, no stale key when the config changes. The panel
also gives the key back on its way out, which it never did, so a panel that has gone no longer
leaves that key swallowed in every pane on the server.

The form keys are no longer settings either. `keybindings.form` is gone and the four keys are
fixed at esc, ctrl+e, ctrl+c and ctrl+s. `review.send_now` went with them, because it is read
inside the compose box for the same reason. That is the class where a printable key hijacks
typing, and it is now unreachable from a config file.

The loader refuses the rest. A key no press produces, which covers chords, wrong casing, tmux
spellings, `ctrl+m` and `ctrl+i`, the empty string and a padded name. A key listed twice. A key
on two settings of one group, which is how the halves of a pair collide. A key the panel answers
itself, which may stay where the defaults put it and may not be moved elsewhere. And a binding
that names no key at all, which used to unbind an action and leave it drawn in the legend.

The collisions that depend on the dispatch tables are refused at panel startup by
`ui.ValidateKeybindings`, and `weave doctor` runs the same check so it reports what the panel
would refuse. The rule is about order rather than guards: an arm with no guard answers every
press, so nothing bound to that key after it can run. That leaves the layering the defaults rely
on alone, where esc clears a standing filter before it steps back and three actions share the
archive key behind guards that read the row.

The half that needs tmux is a new `keybindings` doctor check. It reads `list-keys -T root` on
weave's own server and fails when a key the panel is bound to is held there, since the press
never arrives. It fails when the tmux prefix is `C-a`, because the prefix is read before the
root table and the focus key would be dead. And it fails when something else in the root table
already holds `C-a`, while ignoring weave's own focus binding.

Two of the measured bugs are not fixed and are not refused, because both are the marks working
as designed: a key given to `marks.set` or `marks.jump` still swallows the press after it as a
mark letter, and a mark letter is still a to z only.

## The suite

`config/keys_test.go` covers the loader: each unpressable spelling, the key listed twice, the
key on two settings of a group, the key the panel answers itself, and the empty binding.
`internal/ui/keyconflict_test.go` covers the collision check, including that the shipped
bindings pass it and that a guarded arm in front of an unguarded one still stands.
`internal/ui/focuskey_test.go` covers the key being given back on the way out.
`internal/doctor/keys_test.go` covers the tmux half.

The tests written for the findings are gone where the fix made the failure impossible: the
focus-key spellings, the printable focus key, the form key hijacking a typed letter, the stale
key on a config change, and the reachability tests for configs the loader now refuses. Each rule
was checked by breaking it and watching the suite go red rather than by reading it: 11 cases for
the unpressable names, 4 for the keys the panel answers itself, 2 for the empty binding, and 2
for the collision check.
