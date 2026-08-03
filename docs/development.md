# Development

The contributor page. If you are only installing weave, read [installation](install.md) instead.

Weave is one Go module and one binary. Everything lives under `internal`, and `main.go` hands off to `internal/cli`. It is not containerised, because it drives tmux on your machine. Only Postgres runs in Docker.

## Getting a checkout running

Every tool the repo needs is pinned in `mise.toml`: Go, golangci-lint, shellcheck, shfmt, gum, gotestsum, sqlc, atlas, buf, the protoc plugins, and a specific pg_format build.

```bash
cd ~/weave
mise install
mise run docker:up      # Postgres 18 on 5460, migrated, with the read-only role
mise run go:build       # writes bin/weave
mise run panel          # opens this checkout's panel
```

Nothing else needs configuring. The mise helpers default `DATABASE_URL` to the compose Postgres, so an unexported environment works. Setting it yourself points everything, tests included, at your own instance.

Use `mise run panel` rather than `prefix + g` in a checkout, since the tmux plugin binds that key to one binary and it is probably not the one you just built. If something will not come up, `bin/weave doctor` checks the machine, and `mise run doctor:sandbox` runs the same doctor in a container with nothing set up, which is the only way to see the first-run experience.

## The rebuild loop

This is the part that catches people. The service and the panel are long-lived processes that keep the binary they started with, so writing a new one over `bin/weave` changes nothing about what is running.

`weave version` prints this binary's build and the build answering on the address, so you can see the two disagree. Then:

```bash
mise run restart
```

It builds first, so a change that does not compile leaves the running instance alone. Then it kills the whole tmux server, which is the panel, its stage, and every agent pane, stops the service, and starts a fresh one. That is destructive on purpose: a change to the reconciler cannot be tested against panes spawned by the old code. It is also why you want the throwaway checkout below.

## The tasks

Every task lives in `mise/tasks/` as a shell script with a `#MISE description=` line, so `mise tasks` is the current list and most take `--help`.

- `check` runs the whole suite, every step, reporting which failed rather than stopping at the first. Nothing in it rewrites the tree.
- Database: `db:apply`, `db:migrate <name>`, `db:validate`, `db:sqlc`, `db:fmt`, `db:connect` for psql inside the container, `db:readonly-role`.
- Docker: `docker:up`, `docker:down`. `docker:down -v` drops the volume, which deletes every agent you have.
- Go: `go:build`, `go:test`, `go:lint`, `go:fmt`, `go:tidy`, `go:build-linter`.
- Generators: `proto:generate`, `db:sqlc`, `gen:config`, and `gen:check` for drift.
- Shell: `bash` runs shellcheck, `bash:format` runs shfmt. Both find scripts by what `file` says they are, since the task scripts have no extension.
- Running things: `restart`, `panel`, `doctor:sandbox`.

## The four generators

Nothing under `proto/gen`, `db/models`, `db/migrations`, or `config.toml` is edited by hand.

Protos live in `proto/proto/<pkg>/v1` and generate into `proto/gen` with buf: `mise run proto:generate`.

Queries live in `db/queries` and generate into `db/models` with sqlc: `mise run db:sqlc`. Ids are `uuid` in Postgres and `string` in Go by an override in `db/sqlc.yaml`, since the application generates them as uuidv7 text.

The schema is Atlas HCL in `db/schema`, and `mise run db:migrate add_agent_marks` diffs it against `db/migrations` and writes what is missing. Atlas authors migrations and never applies them; applying is the binary's job through `weave migrate`, so one thing writes the schema and one ledger records it.

`config.toml` at the repo root is generated from the `Config` struct in `internal/appconfig`, taking each key's docs from its Go doc comment. The struct is the source of truth, so a config change is a struct change plus `mise run gen:config`.

`gen:check` reruns them all and fails if committed output moved, though it skips the comparison when those paths already differ from HEAD. Config drift is caught a second way: the appconfig tests regenerate the file in memory and compare, so a stale `config.toml` fails `go:test` even on a dirty tree.

## Checking your work

```bash
mise run docker:up
mise run check
```

Postgres first, and it matters. Store tests run against a real database because most of what the store does is SQL: a partial unique index on live names, keyset pagination, trigram search, triggers stamping `updated_at`. `internal/pgtest` hands each test its own database and drops it afterwards, so they run in parallel safely. With no Postgres those tests fail telling you to run `docker:up`.

Tmux is the case that goes quiet. Tests needing a real tmux server skip themselves when tmux is missing, so a run without it reads as green while covering less than you think.

## The custom linter

`go:lint` builds a custom golangci-lint bundling the plugin in `tools/agentslinter`, its own module, and lints both. After changing an analyzer, force a rebuild with `mise run go:build-linter`.

`forbid_getenv` flags `os.Getenv` and `os.LookupEnv`. Configuration flows through the config package, which is the layer that validates, defaults, and documents each setting. `os.Environ` and `os.Setenv` are left alone, since spawning a CLI in a pane needs to build an environment.

`comment_prefix` requires a doc comment on an exported function to begin with the identifier's name, so a rename moves the comment with the code.

Beyond the plugin, `.golangci.yaml` enables bodyclose, unparam, exhaustive, and errcheck with `check-blank`. That last one surprises people: `_ = doThing()` is a finding, since a discarded error is the same bug whether it went nowhere or to `_`. The short exclusion list is the calls where the zero value genuinely is the answer.

## The throwaway checkout

Unit tests do not cover the wiring between config, the service, tmux, and a spawned CLI, and that wiring is where the real bugs are. Exercising it means restarting for real, which kills the agents you are working with. So keep a second checkout that exists to be destroyed.

```bash
git clone git@github.com:harrisoncramer/weave.git ~/weave-test
cd ~/weave-test && MISE_ENV=test mise run docker:up
```

`mise.test.toml` is tracked and holds everything the two instances would otherwise share: Postgres on 5461, the server on `127.0.0.1:8790`, the tmux socket `weave-test`, and its own cache directory. mise does not load it by default, which is the point: every command aimed at the throwaway instance names it.

```bash
cd ~/weave-test && MISE_ENV=test mise run restart
```

Dropping the prefix is the mistake to watch for, because the command then lands on your development instance and kills your real agents. Confirm with `MISE_ENV=test mise env | grep WEAVE_`, which has to print `WEAVE_ADDR=127.0.0.1:8790` and `WEAVE_TMUX_SOCKET=weave-test`.

Drive that instance through its own [MCP endpoint](mcp.md) at `http://127.0.0.1:8790/mcp`, and assert on what it actually did: pane contents through `tmux -L weave-test capture-pane`, rows in Postgres on 5461, or the log under `~/Library/Caches/weave-test`. Clean up the agents and workflows you made.

Do not add an untracked config file to avoid the prefix. Anything not in the repository is invisible in the diff and goes missing without explanation.

## Conventions

Design work goes in `rfcs/`, numbered. An RFC states the problem, points at the code causing it, and proposes something specific. Read the ones touching an area before changing it.

Comments explain why the code is the way it is. They do not narrate the change being made, refer to how something used to work, or restate the line below them. Match the density of the file you are in.

There is no backward compatibility to keep. Weave has no users, so a migration may be edited rather than superseded, a proto field renumbered, a config key renamed outright.

A new agent CLI is a new package under `internal/harness`, not a change to the reconciler. Anything true of one CLI and not another sits behind the `Harness` interface: the command line it is launched with, how activity is read out of a captured frame, and where its conversation log lives.
