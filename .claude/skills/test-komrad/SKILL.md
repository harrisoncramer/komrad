---
name: test-komrad
description: Write a test in komrad at the right layer with the right helper - storetest for a store, the real store plus shared fakes for a service, a fake service for a handler, komradtest for a scenario, and paneltest for the panel. Use when adding or changing a test anywhere in komrad.
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

A domain's stories live in `package <domain>_test` through `paneltest`, such as
`package logs_test`, in the file named for the screen. The root keeps the stories for routing,
stops, menus, clicks and moves between domains in `package ui_test`. A story builds the panel with
`paneltest.Start`, or the `open<Screen>` helper beside the screen's tests, and then does three
things:

- feeds it watch events with `harness.Watch`, built from `paneltest.Snapshot`, `paneltest.Puts`
  and `paneltest.Deleted`;
- presses keys with `harness.Press`, and sends mouse and other messages with `harness.Send`;
- asserts on the frame, with `harness.Frame` and the `paneltest` readers such as `ListRows`,
  `Screen` and `Legend`, and on the requests sent, with `AssertSent`, `AssertSentTo` and `Sent`.

`paneltest` builds the panel through `ui.New` and sees only what a person or a server sees: the
frames drawn, the requests sent, and the calls tmux records. `harness.Tmux()` reads and sets what
tmux reports. `harness.Answer` and `harness.Refuse` set what an RPC replies. `harness.Hold`,
`harness.Held` and `harness.Release` hold a reply back so a test can look at the frame while it is
in flight. The name is an RPC procedure constant, or `git.<Method>` for the review screen's git.
`TestFormsInFlight` in `forms_test.go` is the worked example.

Time moves only through `harness.Advance`. Each wait the panel arms, such as a loader frame, a
flash or a worktree reading, fires once the clock passes it. `harness.WriteInEditor` sets the text
the next editor the panel opens writes back, and `harness.Edited` lists the files it opened.

A test never names the model or anything unexported. When `paneltest` cannot express something a
person could see, add it to `paneltest` as a reader of the frame or a fake's record, never as a
way into the model. The `black_box` rule flags any test that drives the model or a domain's screen.

A small pure helper with real logic, such as a sort or a matcher, keeps a table test in
`package ui`, in the file named for the helper's source file.

Goldens change only through `-update`, and you read every golden diff.

## Running them

The tests need this checkout's throwaway Postgres, never the panel's. It is the embedded Postgres
under the state directory of the checkout's slot, and `MISE_ENV=test mise run check:quick` creates
or starts it. Read the slot's state directory rather than typing it:

    MISE_ENV=test mise env | grep KOMRAD_STATE_DIR

Run the packages you touched with `GOFLAGS=-count=1`. A cached pass ran nothing. Integration
tests skip without Postgres rather than fail, so check the count.
