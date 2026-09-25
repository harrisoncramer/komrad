---
name: test-komrad
description: Write a test in komrad at the right layer with the right helper - storetest for a store, the real store plus shared fakes for a service, a fake service for a handler, komradtest for a scenario, and panelHarness for the panel. Use when adding or changing a test anywhere in komrad.
---

# Testing komrad

AGENTS.md has the layer table and the four rules. This says how to write a test at each layer.
Pick the lowest layer that can see the behaviour first, then find the file that already owns it
and add a case there.

## Store

A store test opens a database of its own:

    stores := storetest.Open(t)
    require.NoError(t, stores.Agents.Create(t.Context(), agent))

`storetest.Open` migrates a fresh database on the test Postgres and closes it when the test
ends. Every store is a field on what it returns. One test file per store package, with the
query's cases as rows.

## Service

A service runs against `storetest.Open` too. Never write a fake of a store: the `fake_store`
rule flags any test method that matches a store's own method. Fake only the process boundaries,
and use the fake a helper package already has rather than writing one:

- `tmuxtest.Fake` for tmux, and `tmuxtest.Server` for a real tmux on its own socket.
- `harnesstest.Fake`, `Registry`, `Channel` and `Observer` for the harness.
- `gittest.Checkout`, `Heads`, `Trees`, `Rebaser`, `Snapshots`, `Repo` and `NoRepo` for git.

Each is configured by its fields and records what it was asked. Never write a second fake of one
of these boundaries. If a shared fake cannot answer what your test needs, add a field to it.

## Handler

A handler under `internal/rpc` takes its service as an interface, so its test hands in a fake
service and checks only what the handler adds: validation, which domain error becomes which
connect code, caller scoping and rendering. Write it as one table per handler file. depguard
refuses `storetest` and `pgtest` there. A business scenario never runs through a handler test.

## Scenario

A scenario in `internal/e2e` starts the whole service:

    instance := komradtest.Start(t, komradtest.Params{})

It hands back a connect client per service (`instance.Agents`, `instance.Workflows` and so on),
a real tmux server in `instance.Tmux`, the scripted harness in `instance.Harness`, and the stores
in `instance.Stores`. Scenarios carry the `integration` build tag. There is one file per feature
in `docs/`. Each file tells a few long stories as `t.Run` steps, the way
`TestIntegrationAgentsMailEachOther` does. Add a step to the story that owns the feature before
starting a new one.

## Panel

A screen test is in `package ui_test`, in the file named for its screen. It builds the panel
with `newPanelHarness`, or the `open<Screen>` helper beside the screen's tests, and then does
three things:

- feeds it watch events with `harness.watch`;
- presses keys with `harness.press`;
- asserts on the frame, with `harness.readFrame` or the readers beside it, and on the requests
  sent, with `assertSent`, `assertSentTo` and `readSent`.

What tmux was asked to do is read through the `Read*`, `Count*` and `Is*` methods on
`ui.Panel`. `harness.answer` and `harness.refuse` set what an RPC replies. `harness.hold` and
`harness.release` hold a reply back so a test can look at the frame while it is in flight. The
name is an RPC procedure constant, or `git.<Method>` for the review screen's git.
`TestFormsInFlight` in `forms_test.go` is the worked example.

A test never names the model or anything unexported. `export_test.go` is the one file that sees
inside the package. When the harness cannot express something, add the smallest method there
that can. The `black_box` rule flags any other test in `package ui` that drives the model.

A small pure helper with real logic, such as a sort or a matcher, keeps a table test in
`package ui`, in the file named for the helper's source file.

Goldens change only through `-update`, and you read every golden diff.

## Running them

The tests need the test Postgres on 5461. Point every URL at it, never at 5460:

    KOMRAD_OWNER_DATABASE_URL=postgres://komrad:komrad@localhost:5461/komrad?sslmode=disable
    KOMRAD_DATABASE_URL=postgres://komrad_engine_default:komrad@localhost:5461/komrad?sslmode=disable

Run the packages you touched with `GOFLAGS=-count=1`. A cached pass ran nothing. Integration
tests skip without Postgres rather than fail, so check the count.
