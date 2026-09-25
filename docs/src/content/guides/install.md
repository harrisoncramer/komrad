---
title: Installing komrad
description: Get the binary, give it a database, bind the panel key.
group: start
order: 1
---

Komrad is one binary. It runs your agents inside tmux and keeps its records in Postgres. You need Mac or Linux, a recent tmux, PostgreSQL 18 or newer, and Claude or Codex installed and logged in. `komrad doctor` checks all of it and says what is missing.

## 1. Get the binary

```sh
gh release download tip --repo harrisoncramer/komrad --pattern komrad-darwin-arm64 --output ~/.local/bin/komrad
chmod +x ~/.local/bin/komrad
```

Swap `darwin-arm64` for your platform. `tip` is rebuilt from main on every merge and makes no promises. Stable releases have a version number and a changelog. Every release carries a `checksums.txt`.

## 2. Create the database

Create a Postgres user that owns an empty database. Here the user is `komrad_owner` with the password `hunter2` and the database is `komrad`.

```sh
createuser --pwprompt komrad_owner
createdb --owner komrad_owner komrad
```

Point komrad at it as that owner, migrate, and create your engine. The engine is named `laptop` here.

```sh
export KOMRAD_OWNER_DATABASE_URL=postgres://komrad_owner:hunter2@localhost:5432/komrad
komrad migrate
komrad engine ensure laptop
```

An engine is one running komrad on one machine. Several can share one database, each with its own login and its own rows. `komrad migrate` is the only thing that changes the schema, and the service refuses to start against a schema its build does not match.

## 3. Write the config

`engine ensure` prints a `[connection]` block with a login it generated for the engine. Put that block in `~/.config/komrad/config.toml`, and add the owner URL from the previous step beside it.

```toml
[connection]
owner_database_url = "postgres://komrad_owner:hunter2@localhost:5432/komrad"
database_url = "postgres://komrad_engine_laptop:8f3a1c9e2b7d4e6f@localhost:5432/komrad"
```

`owner_database_url` is only used by `komrad migrate` and `komrad engine ensure`. The service itself connects with `database_url`.

## 4. Bind the panel key

Add this to your tmux config and reload tmux.

```
run-shell 'komrad tmux'
```

Prefix then `g` opens and hides the panel. Change the key with [`panel_key`](../../config/#connection.panel_key). The first open starts the service in the background.

<figure class="placeholder">
  <figcaption>The panel open in a tmux popup over a normal terminal session, showing an empty workflow list on first run.</figcaption>
</figure>

## Upgrading

Download the new binary over the old one, then stop the service and migrate.

```sh
komrad stop
komrad migrate
```

Open the panel again and it starts the new build. Agents keep running throughout, because they live in tmux, not in the service.

The panel is open and empty. The first thing it needs is a [workflow](../workflows/).
