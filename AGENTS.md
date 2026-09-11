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

`mise run demo` with no arguments asks which scenario to draw, and `-scenario <name>` names one
outright. There is no default: a screen is worth looking at for a reason, and the reason is the
scenario, so the binary on its own refuses rather than guessing. `mise run demo -- -list` names
them. The quit key closes it, since there is no tmux client here to detach and leave it running
behind.

The review screen runs git in the agent's own directory, so the `review` scenario builds a
throwaway repository first and points its agents at it. That is what `NeedsRepo` on a scenario
asks for, and `WithRepo` is what fills the commits in. The repository goes when the demo closes.

One thing it does not do yet. The command sets no size of its own, so the width a screen is drawn
at is the width of the terminal you start it in, and eighty columns means sizing that terminal by
hand.

The fake lives in `cmd/paneldemo`. Its services embed the generated unimplemented handlers and
are served over an `httptest` server, so a call nobody wrote comes back as an error the panel
draws rather than as a panic. The data is `cmd/paneldemo/scenario`, one file per scenario, which
is where a new one is added. A scenario that writes as well as reads keeps what the panel wrote,
so a comment typed into the demo is there when the sidebar draws again. The panel asks tmux only
about the panes beside it, so the stub answers that there are none and every screen draws both
its columns itself. An agent's own terminal on the stage is another process, so the screens that
put one there draw their sidebar and nothing beside it.
