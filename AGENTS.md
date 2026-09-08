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

# Comments

Most declarations need no comment. Only exported functions are required to carry one, and a name
that already says what the thing is wants nothing more.

When you do write one, say what the thing is in one short sentence and stop. Why it is that way
goes in the commit message, where it does not have to be maintained alongside the code.

When the linter rejects a second sentence, delete that sentence. Do not fold it into the first
with a comma and a "since", "because", "so that" or "rather than". That trailing justification
clause is the most common defect in this repository's comments, and the compression is what
produces it.

    // bad
    // action is one arm of a screen's dispatch, the description living on the legend
    // row instead because a row can name two actions and has room for one wording.
    // good
    // action is one arm of a screen's dispatch.

    // bad
    // DefaultKeybindings are the bindings used when the config omits them, read from
    // the default tag on each field so that a binding and the key it ships with are
    // written in one place.
    // good
    // DefaultKeybindings are the bindings used when the config omits them.

    // bad, on a self-evident interface
    // Opener is what anything with a URL to show a person calls, declared here rather
    // than beside each caller so one stand-in serves all of them in tests.
    // good
    // no comment

Never point a comment at a test, a caller or another file. Names rot, and the reader has to look
somewhere else to finish the line.

`mise run go:comments` counts the trailing clauses against a checked-in baseline and fails when a
package gains one. The baseline only comes down.
