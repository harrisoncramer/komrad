# Working on komrad

Komrad's built-in database default is `localhost:5460`, which is the panel you are running
inside. `komrad migrate` and `mise run restart` migrate it as its owner, so a checkout carrying
a migration of its own rewrites the panel's schema and leaves the panel refusing to start on its
older build. Work against the throwaway instance instead, which has its own database, server,
and tmux socket.

    MISE_ENV=test mise run docker:up
    MISE_ENV=test mise run restart

Never run `mise run docker:up`, `mise run db:apply`, `mise run restart`, or a bare `komrad`
command from a worktree without it.

The tests take it too, as `MISE_ENV=test mise run check:quick`. From a worktree, `go:test` and
`check:quick` refuse to run without it, since their databases would land on the panel's Postgres.

Each checkout gets a throwaway instance of its own, so agents testing at once do not share one.
Every instance keeps its database in one shared test Postgres on port 5470. `MISE_ENV=test mise
env` prints the instance's database, address and socket, `MISE_ENV=test mise run test:reset`
empties its database, and `MISE_ENV=test mise run test:down` drops it and takes the instance down.

Never stop the shared test Postgres, since every other checkout's instance is in it. Starting and
stopping it are `mise run test:container:up` and `test:container:down`, which are for a person
rather than for day-to-day work.

A change to what the panel draws wants a recording on its pull request, which the throwaway
instance makes from a script of keypresses under `recordings/`:

    MISE_ENV=test mise run record recordings/dev/example-tour.tape

The record-komrad skill under `.claude/skills` says how to write the script and attach the gif.

## Tests

Each behaviour is tested at the lowest layer that can see it, and only there. A higher layer
tests what it adds and nothing it inherits.

| Layer | Where | Tested against | Owns |
|---|---|---|---|
| Store | `internal/store/...` | real Postgres through `storetest` | query semantics, ordering, constraints, tenancy |
| Service | `agents`, `mail`, `reviews`, `checkouts`, `worktrees`, `schedules`, `pullrequests`, `transcripts` | the real store, with only tmux, the harness, git, the forge and the clock faked | business rules |
| Adapter | `internal/rpc/...`, `internal/mcp` | a fake service | validation, error to code mapping, caller scoping, rendering |
| Scenario | `internal/e2e`, through `komradtest` | a real service, tmux and Postgres | a feature as a user or an agent meets it, one file per feature |
| Outside world | `internal/git`, `internal/tmux`, harness frame readers | the real binary or recorded frames | what the other program does |
| Panel | `internal/ui`, in `package ui_test` through `panelHarness` | the frame drawn and the requests sent | keys, screens and forms |

- A regression is a new case in the table or story that owns the behaviour, not a new function.
- A test file sits beside the source file it tests, or is named for the feature or screen it
  describes, never for the change that added it.
- No doc comment that restates a test's name.
- Before adding a test at a higher layer, check the lower layer does not already own it.

A reviewer treats a test at the wrong layer, or one repeating a lower layer, as a finding.

The test-komrad skill under `.claude/skills` says how to use each layer's helpers.
