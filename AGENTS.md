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

A change to what the panel draws wants a recording on its pull request, which the throwaway
instance makes from a script of keypresses under `recordings/`:

    MISE_ENV=test mise run record recordings/tour.rec

The record-weave skill under `.claude/skills` says how to write the script and attach the gif.
