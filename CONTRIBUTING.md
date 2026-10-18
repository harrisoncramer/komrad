# Contributing

## Building

Komrad's tools come from [mise](https://mise.jdx.dev). Install it, then from the repository:

```sh
mise install
mise run go:build
```

The binary lands in `bin/komrad`.

## Testing

The tests run against a throwaway instance with its own embedded Postgres, so they never touch the komrad you use day to day. Check your change with:

```sh
MISE_ENV=test mise run check:quick
```

`check:quick` builds, checks formatting and tests the packages your branch touched. CI runs the full suite on every pull request.

[AGENTS.md](AGENTS.md) covers the throwaway instance, migrations, and which layer a test belongs in.

## Pull requests

Keep a pull request to one change. Title it as a [conventional commit](https://www.conventionalcommits.org), such as `fix(panel): keep the cursor on a deleted agent`. The description says why the change exists in a short paragraph or two. Pull requests are squash merged, so the title becomes the commit on main.

A change to what the panel draws wants a recording. The record-komrad skill under `.claude/skills` says how to make one.

## Bugs

File bugs with the [bug report form](https://github.com/harrisoncramer/komrad/issues/new?template=bug_report.yml). The [bugs guide](https://komrad.dev/latest/guides/bugs/) says what to check first. Ideas go in [Discussions](https://github.com/harrisoncramer/komrad/discussions).

## Releases

A stable release is a `v` tag on main. Cut one from an up-to-date main whose CI passed:

```sh
mise run release:cut v0.3.0
```

The tag starts the release workflow. It runs the suite, publishes the four binaries and `checksums.txt` with a changelog, publishes the docs for that version beside latest, and deploys the engine.

A tag starts the workflow only once, so a release that failed part way is retried on the same tag rather than by tagging again:

```sh
gh workflow run release.yml --ref v0.3.0
```

A retry reuses the release if it was already created and replaces its files, so it is safe to run more than once.

Separately, every merge to main rebuilds the `tip` prerelease.
