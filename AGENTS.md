# Working on weave

Weave's built-in database default is `localhost:5460`, which is the panel you are running
inside. Any command that opens the store migrates it, so a checkout carrying a migration of
its own rewrites the panel's schema and leaves it unable to read its own tables. Weave now
refuses that once the panel's server is on a build holding this guard, but do not rely on the
refusal: work against the throwaway instance instead, which has its own Postgres, server, and
tmux socket.

    MISE_ENV=test mise run docker:up
    MISE_ENV=test mise run restart

Never run `mise run docker:up`, `mise run db:apply`, `mise run restart`, or a bare `weave`
command from a worktree without it.

## Looking at a screen before it is built

A layout argument is easier to settle on a screen than in prose, and a block of ASCII in a
document cannot tell you whether a sidebar and a column both fit at eighty columns. `mise run
demo` starts the real panel against a fake service, so what you are looking at is what the panel
draws rather than a drawing of it. It needs no database, no `weave serve`, and no tmux.

`mise run demo -- -list` names the scenarios it can draw and `-scenario <name>` picks one, so a
screen can be looked at busy or empty without editing anything.

Two things it does not do yet. The review screen does not draw: pressing the review key reports
that `ListReviewThreads` is not implemented, which is the tool and not your branch. And the
command sets no size of its own, so the width a screen is drawn at is the width of the terminal
you start it in, and eighty columns means sizing that terminal by hand.

The fake lives in `cmd/paneldemo`. Its services embed the generated unimplemented handlers and
are served over an `httptest` server, so a call nobody wrote comes back as an error the panel
draws rather than as a panic. The data is `cmd/paneldemo/scenario`, one file per scenario, which
is where a new one is added. The panel asks tmux only about the panes beside it, so the stub
answers that there are none and every screen draws both its columns itself. An agent's own
terminal on the stage is another process, so the screens that put one there draw their sidebar
and nothing beside it.
