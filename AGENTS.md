# Working on komrade

Komrade's built-in database default is `localhost:5460`, which is the panel you are running
inside. `komrade migrate` and `mise run restart` migrate it as its owner, so a checkout carrying
a migration of its own rewrites the panel's schema and leaves the panel refusing to start on its
older build. Work against the throwaway instance instead, which has its own Postgres, server,
and tmux socket.

    MISE_ENV=test mise run docker:up
    MISE_ENV=test mise run restart

Never run `mise run docker:up`, `mise run db:apply`, `mise run restart`, or a bare `komrade`
command from a worktree without it.

A change to what the panel draws wants a recording on its pull request, which the throwaway
instance makes from a script of keypresses under `recordings/`:

    MISE_ENV=test mise run record recordings/tour.rec

The record-komrade skill under `.claude/skills` says how to write the script and attach the gif.

## Tests

Each behaviour is tested at the lowest layer that can see it, and only there. A higher layer
tests what it adds and nothing it inherits.

| Layer | Where | Tested against | Owns |
|---|---|---|---|
| Store | `internal/store/...` | real Postgres through `storetest` | query semantics, ordering, constraints, tenancy |
| Service | `agents`, `mail`, `reviews`, `checkouts`, `worktrees`, `schedules`, `pullrequests`, `transcripts` | the real store, with only tmux, the harness, git, the forge and the clock faked | business rules |
| Adapter | `internal/rpc/...`, `internal/mcp` | a fake service | validation, error to code mapping, caller scoping, rendering |
| Scenario | `internal/e2e`, through `komradetest` | a real service, tmux and Postgres | a feature as a user or an agent meets it, one file per feature |
| Outside world | `internal/git`, `internal/tmux`, harness frame readers | the real binary or recorded frames | what the other program does |
| Panel | `internal/ui`, in `package ui_test` through `panelHarness` | the frame drawn and the requests sent | keys, screens and forms |

- A regression is a new case in the table or story that owns the behaviour, not a new function.
- A test file sits beside the source file it tests, or is named for the feature or screen it
  describes, never for the change that added it.
- No doc comment that restates a test's name.
- Before adding a test at a higher layer, check the lower layer does not already own it.

A reviewer treats a test at the wrong layer, or one repeating a lower layer, as a finding.

The test-komrade skill under `.claude/skills` says how to use each layer's helpers.
