---
title: Installing komrade
description: What komrade needs, how to get it, and how to keep it up to date.
group: start
order: 1
---

Komrade is a single binary. It runs your agents inside tmux and keeps its records in Postgres, so installing it means getting the binary, giving it a database, and letting tmux know about it.

## What you need

Komrade runs on Mac and Linux. It needs a recent tmux, PostgreSQL 18 or newer, and at least one agent CLI, Claude or Codex, installed and already logged in. `komrade doctor` checks all of this for you and says what is missing.

## Getting the binary

Komrade is published on GitHub in two channels. A stable release has a version number and a changelog, and is cut on purpose when main is in good shape. The `tip` release is rebuilt from main after every merge. It is always the newest code and makes no promises, so expect the odd breaking change or bug. Until the first stable release is cut, `tip` is the only channel.

There is a build for Apple Silicon and Intel Macs, and for Linux on both x86 and ARM. Each release also carries checksums to verify a download against.

```sh
gh release download tip --repo harrisoncramer/komrade --pattern komrade-darwin-arm64 --output ~/.local/bin/komrade
chmod +x ~/.local/bin/komrade
```

The repository is private for now, which is why the download goes through the GitHub CLI.

## Giving it a database

Komrade needs an empty Postgres database and a login that owns it.

The owner login sets things up, and the running service uses a login of its own. That split exists because several komrades, called engines, can share one database. Each engine gets its own login, and Postgres keeps every engine to its own rows. An engine is one running komrade on one machine, such as your laptop.

Set up the database as the owner. `komrade migrate` creates the schema, and `komrade engine ensure` creates your engine and prints the connection setting it should use. Put the owner's URL and that printed setting in your config file, under [`owner_database_url`](../../config/#connection.owner_database_url) and [`database_url`](../../config/#connection.database_url). Put them in the file rather than your shell, because the panel opens in a tmux popup that never sees your shell's variables.

The service never changes the schema on its own. It refuses to start against a schema its build does not match, which is why upgrades migrate first.

## Hooking it into tmux

Add `run-shell 'komrade tmux'` to your tmux config and reload it. That gives tmux a shortcut for opening and hiding the panel, set by [`panel_key`](../../config/#connection.panel_key). The first time you open the panel, komrade starts its service in the background.

<figure class="placeholder">
  <figcaption>The panel open in a tmux popup over a normal terminal session, showing an empty workflow list on first run.</figcaption>
</figure>

Run `komrade doctor` whenever something looks wrong. It checks every piece komrade depends on and says which one is broken.

## Upgrading

Download the new binary over the old one. Your config, database and tmux setup are left alone.

The running service keeps the build it started with, so stop it, migrate the database, and open the panel again, which starts the new build.

```sh
komrade stop
komrade migrate
```

A panel that is open when the binary changes notices and asks you to reopen it. Your agents keep running through all of this, because they live in tmux rather than inside the service.

## Building from source

Contributors build from a checkout of the repository. It needs Go 1.25.8 or newer and mise, which installs the rest of the tools. `mise run go:build` builds the binary and `mise run docker:up` starts a local development database.
