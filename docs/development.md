# Development

This is the contributor page. It covers getting a checkout running, the rebuild loop, the mise task tree, the four generators, and the conventions the repo holds to. If you are only installing weave, read [installation](install.md) instead.

Weave is one Go module and one binary. Everything lives under `internal`, and `main.go` hands off to `internal/cli`. It is not containerised, because it drives tmux on your machine. Only Postgres runs in Docker.

TODO TODO TODO: Write development file.

## Getting a checkout running

Every tool the repo needs is pinned in `mise.toml`: Go, golangci-lint, shellcheck, shfmt, gum, gotestsum, sqlc, atlas, buf, the two protoc plugins, and a specific pg_format build. Install them first.

```sh
cd ~/weave
mise install
```

Then bring up the database. `docker:up` starts Postgres 18, applies the migrations, and creates the read-only role that the MCP query tools connect as. Running it again is free, so run it whenever you have pulled and the schema might have moved.

```sh
mise run docker:up
```

Postgres 18 is a hard floor. The schema uses the built-in `uuidv7()` for primary keys, which does not exist before it, and transcript search needs `pg_trgm`, which the binary creates itself on first run.

Nothing else needs configuring for a plain checkout. The mise task helpers default `WEAVE_DATABASE_URL` to the compose Postgres on port 5460, so an unexported environment works. Setting `WEAVE_DATABASE_URL` points everything, including the tests, at your own instance instead, and the helpers derive the plain `DATABASE_URL` that Atlas and psql read from it. See [configuration](configuration.md) for the rest.

Now build, start the service, and open the panel.

```sh
mise run go:build      # writes bin/weave
bin/weave start        # starts the service if this build is not already serving
mise run panel         # attaches to this checkout's panel
```

`weave start` is a convenience over `weave serve`, which is the long-lived process itself. Start does nothing if a server from this same build is already answering, so it is safe to run twice. The panel starts the service for itself when you open it, so in practice you can skip straight to `mise run panel`.

Use `mise run panel` rather than `prefix + g` in a checkout. The tmux plugin binds that key to one binary, and it is probably not the one you just built.

If something will not come up, `bin/weave doctor` checks the machine: tmux version, the shell tmux hands commands to, Postgres reachability, whether the schema is migrated, and whether an agent CLI is on your `PATH`. `mise run doctor:sandbox` runs that same doctor inside a container that has nothing set up, which is the only way to see the first-run experience from a machine that already has everything.

## The rebuild loop

This is the part that catches people. The service and the panel are long-lived processes. They keep the binary they started with, so writing a new one over `bin/weave` changes nothing about what is running. Your fix is on disk and the old code is still serving.

`weave version` tells you the truth. It prints this binary's build and the build of whatever is answering on the address, so you can see the two disagree.

```sh
bin/weave version
this binary: devel
the server at 127.0.0.1:8787: "devel"
```

Both say `devel` for an ordinary `go build`, so that comparison only distinguishes a release build from a local one. The panel does the same check internally and refuses to adopt a server whose version does not match its own, which is why a stale server is usually visible as the panel starting a second one.

The loop is one task:

```sh
mise run restart
```

It builds first, so a change that does not compile leaves the running instance alone. Then it kills the whole tmux server, which is the panel, its stage, and every agent pane, stops the service matched on its address rather than on the binary name, and starts a fresh one. It reports how many panes it killed.

That is destructive, and deliberately so. A change to the reconciler or to how agents are launched cannot be tested against panes spawned by the old code. But it means `mise run restart` in your development checkout throws away every agent you had running. That is the reason for the second checkout below.

## The mise tasks

Every task lives in `mise/tasks/` as a shell script with a `#MISE description=` line, so `mise tasks` is always the current list and most tasks take `--help`. Grouped by area:

Everything: `check` runs the whole suite. It runs every step and reports which ones failed rather than stopping at the first, so one run tells you everything that is wrong. Nothing in it rewrites the tree; the formatters are called in their reporting mode.

Database: `db:apply` applies pending migrations to whatever `WEAVE_DATABASE_URL` points at. `db:migrate <name>` authors a new migration by diffing `db/schema` against the existing ones. `db:validate` checks `db/migrations` against `atlas.sum`. `db:sqlc` regenerates the query bindings. `db:fmt` formats the queries and the schema. `db:connect` opens psql inside the compose container, so you do not need a local client. `db:readonly-role` creates the least-privilege role behind the MCP SQL tools.

Docker: `docker:up` starts Postgres, migrates it, and grants the read-only role. `docker:down` stops it; `docker:down -v` also drops the volume, which deletes every agent you have.

Go: `go:build` writes `bin/weave`. `go:test` runs every test through gotestsum. `go:lint` runs the custom linter over both modules. `go:fmt` formats, or reports with `--check`. `go:tidy` tidies `go.mod`. `go:build-linter` rebuilds the custom golangci-lint binary.

Generators: `proto:generate`, `db:sqlc`, `gen:config`, and `gen:check` to catch drift across all of them.

Shell: `bash` runs shellcheck over every script the repo owns, and `bash:format` runs shfmt. Both find scripts by what `file` says they are rather than by extension, since the task scripts have no extension.

Running things: `restart` for the rebuild loop, `panel` to open this checkout's panel, `doctor:sandbox` for the first-run containers.

## The four generators

Four things in this tree are generated, and none of their output is edited by hand. If you find yourself editing a file under `proto/gen`, `db/models`, `db/migrations`, or `config.toml`, you are editing the wrong file.

Protos. The definitions live in `proto/proto/<pkg>/v1` and generate Go plus ConnectRPC code into `proto/gen` with buf.

```sh
mise run proto:generate
```

Queries. The SQL lives in `db/queries` and generates typed bindings into `db/models` with sqlc, checked against the migrations rather than a live database. Note that ids are `uuid` in Postgres and `string` in Go, by an override in `db/sqlc.yaml`, because the application generates them as uuidv7 text and every boundary they cross carries them as strings.

```sh
mise run db:sqlc
```

Schema and migrations. The desired schema is Atlas HCL in `db/schema`. Atlas diffs it against `db/migrations` and writes what is missing.

```sh
mise run db:migrate add_agent_marks
```

Atlas authors migrations and never applies them. Applying is the binary's job, through `weave migrate` and its own embedded runner, so one thing writes the schema and one ledger records what it wrote. If both applied, Atlas's revision table and the binary's would each hold half the history and neither would agree the schema was current. `migrate diff` only needs the throwaway dev container Atlas starts for itself, so authoring a migration needs Docker but not a running development database.

Config. `config.toml` at the repo root is generated from the `Config` struct in `config`, taking each key's documentation from its Go doc comment and each default from the code. The struct is the source of truth, so a config change is a struct change followed by:

```sh
mise run gen:config
```

Drift. `gen:check` reruns sqlc, buf, and the config generator and fails if any committed output moved. It skips the comparison when `config.toml`, `db/models`, or `proto/gen` already differ from HEAD, since then a difference says nothing about whether the generators are in sync. Config drift is caught a second way regardless: the appconfig tests regenerate the file in memory and compare, so a stale `config.toml` fails `go:test` even on a dirty tree.

## Checking your work

```sh
mise run docker:up
mise run check
```

Postgres first, and it matters. The store tests are run against a real database because most of what the store does is SQL: a partial unique index on live names, keyset pagination, trigram search, triggers stamping `updated_at`. A fake would assert that the Go around those works and say nothing about the parts that break. `internal/pgtest` hands each test its own freshly created database and drops it afterwards, so tests are safe to run in parallel and one leaving a row behind cannot change what the next sees. With no Postgres running, those tests fail with a message telling you to run `docker:up`.

Tmux is the case that goes quiet. Tests that need a real tmux server skip themselves when tmux is not installed, so a run with no tmux reads as green while covering less than you think. On a development machine you have tmux, so this mostly matters in CI.

## The custom linter

`go:lint` does not run stock golangci-lint. It builds a custom binary bundling the plugin in `tools/agentslinter`, which is its own Go module, so `go:lint` lints both `.` and `tools/agentslinter`. The build happens on demand if `local/custom-gcl` is missing; after changing an analyzer, force it with `mise run go:build-linter`.

Two analyzers, both configurable under `custom.agentslinter.settings` in `.golangci.yaml` and both skippable with `skip: true`.

`forbid_getenv` flags direct `os.Getenv` and `os.LookupEnv`. Configuration has to flow through the config package, because that is the layer that validates, defaults, and documents each setting, and a bare read in the middle of the code is a setting nobody knows exists. It leaves `os.Environ`, `os.Setenv`, and `os.Unsetenv` alone, since spawning a CLI in a pane legitimately needs to build an environment.

`comment_prefix` requires a doc comment on an exported function or method to begin with the identifier's name. It does not require the comment. The point is that an LSP rename then moves the comment with the code instead of leaving it describing a function that no longer has that name.

Analyzers are tested with `analysistest`, with fixtures under `testdata/src/<pkg>` and expectations as `// want` comments.

Beyond the plugin, `.golangci.yaml` enables bodyclose, unparam, exhaustive on switches, and errcheck with `check-blank`. That last one is worth knowing before it surprises you: a discarded error is the same bug whether it went nowhere or to `_`, so `_ = doThing()` is a finding. The short exclusion list in the config is the set of calls where the zero value genuinely is the answer, mostly closes in a defer where the caller has already returned and rollbacks with nothing left to report to. Tests and the shared test-fixture packages are excluded, since their teardown discards errors that mean the throwaway thing is already gone.

## The throwaway checkout

Unit tests do not cover the wiring between config, the service, tmux, and a spawned CLI, and that wiring is where the real bugs are. Exercising it means restarting for real, and restarting your development checkout kills the agents you are working with. So keep a second checkout that exists to be destroyed.

```sh
git clone git@github.com:harrisoncramer/weave.git ~/weave-test
cd ~/weave-test && MISE_ENV=test mise run docker:up
```

`mise.test.toml` is tracked, and holds everything the two instances would otherwise share: its own Postgres on port 5461, its own server on `127.0.0.1:8790`, its own tmux socket `weave-test`, its own cache directory, and a `WEAVE_CONFIG` pointing at `weave.test.toml` in the checkout. That last one matters because a config file can name a database, so without it the throwaway instance would read the panel settings, and possibly the connection string, of the checkout you develop in. mise does not load that file by default, which is on purpose. It means the checkout you develop in ignores it, and every command aimed at the throwaway one names it:

```sh
cd ~/weave-test
MISE_ENV=test mise run restart
```

Dropping the prefix is the mistake to watch for, because the command then lands on your development instance and kills your real agents. Confirm the environment resolved before relying on it:

```sh
cd ~/weave-test && MISE_ENV=test mise env | grep WEAVE_
```

That has to print `WEAVE_ADDR=127.0.0.1:8790`, `WEAVE_TMUX_SOCKET=weave-test`, and a `WEAVE_CONFIG` inside the throwaway checkout. If it prints nothing, the prefix was dropped.

Drive that instance through its own [MCP endpoint](mcp.md) at `http://127.0.0.1:8790/mcp`, and assert on what the system actually did: the pane contents through `tmux -L weave-test capture-pane`, the rows in Postgres on 5461, or the log under `~/Library/Caches/weave-test`. Delete the agents and workflows you created when you are done.

Do not add an untracked config file to avoid the prefix. Anything not in the repository is invisible in the diff and goes missing without explanation. A setting the throwaway instance needs goes in `mise.test.toml` and is committed like any other change.

## Conventions

Design work goes in `rfcs/`, numbered. An RFC here is a document that states the problem, points at the code that causes it, and proposes something specific, not a summary written after the fact. Read the ones touching an area before proposing a change to it.

Comments explain why the code is the way it is, in plain prose. They do not narrate the change being made, do not refer to how something used to work, and do not restate the line below them. The density varies by file, so match what is around you rather than adding a comment per block. Most of the sharpest things to know about this repo are already comments; the reason Atlas does not apply migrations is a comment in `db/atlas.hcl`, and the reason `restart` kills tmux before stopping the server is a comment in the task.

There is no backward compatibility to keep. Weave has no users, so a migration may be edited rather than superseded, a proto field may be renumbered, and a config key may be renamed outright. Do the clean thing.

New CLIs are a new package under `internal/harness`, not a change to the reconciler. Anything true of one agent CLI and not another sits behind the `Harness` interface, such as the command line it is launched with, how activity is read out of a captured frame, and where its conversation log lives. [How it works](concepts.md) covers that seam in more detail.
