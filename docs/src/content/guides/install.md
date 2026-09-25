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

Komrade is published on GitHub in two channels. Stable releases have a version number and a changelog; the `tip` release is rebuilt from main after every merge, and always the newest code. Expect breaking changes or bugs if running latest.

Releases carry checksums to verify a download:

```sh
gh release download tip --repo harrisoncramer/komrade --pattern komrade-darwin-arm64 --output ~/.local/bin/komrade
chmod +x ~/.local/bin/komrade
```

## Giving it a database

Komrade needs an empty Postgres database to store agent data. This lets you refer to old conversations, and communicate across agents.

Several komrades, called engines, can share one database. Each engine gets its own login, and Postgres keeps every engine to its own rows via row-level security policies. One engine is one running komrade on one machine, such as your laptop.

The `komrade migrate` command creates komrade's initial schema, and `komrade engine ensure` creates your engine with a connection setting. Put the owner's URL and that printed setting in your config file, under [`owner_database_url`](../../config/#connection.owner_database_url) and [`database_url`](../../config/#connection.database_url).

The service never changes the schema on its own. It refuses to start against a schema its build does not match.

## Hooking it into tmux

Add `run-shell 'komrade tmux'` to your tmux config file. This gives tmux a shortcut for opening and hiding the panel, set by [`panel_key`](../../config/#connection.panel_key).

The first time you open the panel, komrade starts its service in the background.

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
