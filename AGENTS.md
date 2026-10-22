# Working on komrad

Komrad keeps its database in the embedded Postgres under its state directory, and the panel you are
running inside uses the one under `~/.local/state/komrad`. `komrad migrate` and `mise run restart`
migrate it as its owner, so a checkout carrying a migration of its own rewrites the panel's schema
and leaves the panel refusing to start on its older build. Work against the throwaway instance
instead, which has its own embedded Postgres, server, and tmux socket.

    MISE_ENV=test mise run restart

Never run `mise run db:apply`, `mise run restart`, or a bare `komrad` command from a worktree
without it.

The tests take it too, as `MISE_ENV=test mise run check:quick`, which creates or starts the
instance's Postgres and migrates it first. `go:test` and `check:quick` refuse to run without it,
since their databases would land on the panel's Postgres.

Each checkout gets a throwaway instance of its own, so agents testing at once do not share one.
Each instance keeps its embedded Postgres in its own state directory. `MISE_ENV=test mise env`
prints the instance's state directory, address and socket, `MISE_ENV=test mise run test:reset`
empties its database, and `MISE_ENV=test mise run test:down` drops it and takes the instance down.

A change to what the panel draws wants a recording on its pull request, which the throwaway
instance makes from a script of keypresses under `recordings/`:

    MISE_ENV=test mise run record recordings/dev/example-tour.tape

The record-komrad skill under `.claude/skills` says how to write the script and attach the gif.

A diagram in the docs is a D2 file under `docs/diagrams`, never ASCII art. Each one imports
`_theme.d2` and draws every shape from its classes: `leaf` for a box, `store` for a database,
`group` for a box holding others, `more` for an elided run, `blank` for an empty grid cell and
`dashed` for a loose edge. A box names its part in a short capitalised title and nothing more,
and the prose around the diagram says the rest. Each renders to
`docs/public/diagrams/<name>.png`:

    mise run docs:diagram [name...]

Commit the source and the PNG together.

## Stacked tasks

On a stacked task, never rebase onto the parent, and a reviewer never asks for one; komrad rebases the stack.

## Writing

Load the write-komrad skill under `.claude/skills` before writing anything a person or an agent
reads, from a proto comment to a pull request description. It follows `docs/style-guide.md`.

## Migrations

An older build runs against the newest schema, whether it is the panel you are running inside or
a rollback, so a migration only ever adds. Squawk lints each new migration for that in CI. When it
refuses one, split the change: ship the addition, and drop what is unused in a later pull request
once no build reads it. Never edit `.squawk.toml` to get a migration past it. CI fails any pull
request that changes that file.

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
| Panel | `internal/ui` through `paneltest`, each domain's stories in `package <domain>_test` and the root's in `package ui_test` | the frame drawn and the requests sent | a domain's keys, screens and forms in its own package, and routing, stops, menus, clicks and moves between domains in the root |

- A regression is a new case in the table or story that owns the behaviour, not a new function.
- A test file sits beside the source file it tests, or is named for the feature or screen it
  describes, never for the change that added it.
- No doc comment that restates a test's name.
- Before adding a test at a higher layer, check the lower layer does not already own it.

A reviewer treats a test at the wrong layer, or one repeating a lower layer, as a finding.

The test-komrad skill under `.claude/skills` says how to use each layer's helpers.
