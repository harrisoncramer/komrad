---
title: Installing komrade
description: Get the binary, give it a database, bind the panel key.
group: start
order: 1
---

Komrade is one binary. It runs your agents inside tmux and keeps its records in Postgres. You need Mac or Linux, a recent tmux, PostgreSQL 18 or newer, and Claude or Codex installed and logged in. `komrade doctor` checks all of it and says what is missing.

## 1. Get the binary

```sh
gh release download tip --repo harrisoncramer/komrade --pattern komrade-darwin-arm64 --output ~/.local/bin/komrade
chmod +x ~/.local/bin/komrade
```

Swap `darwin-arm64` for your platform. `tip` is rebuilt from main on every merge and makes no promises. Stable releases have a version number and a changelog. Every release carries a `checksums.txt`.

## 2. Create the database

Create a Postgres user that owns an empty database. Here the user is `komrade_owner` with the password `hunter2` and the database is `komrade`.

```sh
createuser --pwprompt komrade_owner
createdb --owner komrade_owner komrade
```

Point komrade at it as that owner, migrate, and create your engine. The engine is named `laptop` here.

```sh
export KOMRADE_OWNER_DATABASE_URL=postgres://komrade_owner:hunter2@localhost:5432/komrade
komrade migrate
komrade engine ensure laptop
```

An engine is one running komrade on one machine. Several can share one database, each with its own login and its own rows. `komrade migrate` is the only thing that changes the schema, and the service refuses to start against a schema its build does not match.

## 3. Write the config

`engine ensure` prints a `[connection]` block with a login it generated for the engine. Put that block in `~/.config/komrade/config.toml`, and add the owner URL from the previous step beside it.

```toml
[connection]
owner_database_url = "postgres://komrade_owner:hunter2@localhost:5432/komrade"
database_url = "postgres://komrade_engine_laptop:8f3a1c9e2b7d4e6f@localhost:5432/komrade"
```

`owner_database_url` is only used by `komrade migrate` and `komrade engine ensure`. The service itself connects with `database_url`.

## 4. Bind the panel key

Add this to your tmux config and reload tmux.

```
run-shell 'komrade tmux'
```

Prefix then `g` opens and hides the panel. Change the key with [`panel_key`](../../config/#connection.panel_key). The first open starts the service in the background.

<figure class="placeholder">
  <figcaption>The panel open in a tmux popup over a normal terminal session, showing an empty workflow list on first run.</figcaption>
</figure>

## Upgrading

Download the new binary over the old one, then stop the service and migrate.

```sh
komrade stop
komrade migrate
```

Open the panel again and it starts the new build. Agents keep running throughout, because they live in tmux, not in the service.

The panel is open and empty. The first thing it needs is a [workflow](../workflows/).
