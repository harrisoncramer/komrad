---
title: Running weave remotely
description: Keep the panel on your laptop and run the agents on a box somewhere else.
group: operations
order: 10
---

Weave can run its agents on a remote machine you reach over SSH, so they keep working when you close your laptop. The panel stays on your machine. The engine moves, which means the service, the database and every agent's tmux window.

Weave's own repository runs one, and remote agents often open pull requests against it. [Administering the engine](../engine-admin/) covers building and maintaining that box. This guide covers using an engine that already exists.

## Pointing the panel at an engine

An engine is a file in `~/.config/weave/engines/` on your machine, layered over your normal config. Write `~/.config/weave/engines/remote.toml`.

```toml
[connection]
addr               = "127.0.0.1:8787"
tmux_socket        = "weave-remote"
engine_tmux_socket = "weave"
cache_dir          = "~/.cache/weave-remote"

exec = [
  "ssh", "-t",
  "-o", "ControlMaster=auto",
  "-o", "ControlPersist=10m",
  "-o", "ControlPath=/tmp/cm-weave-%r@%h:%p",
  "-o", "StrictHostKeyChecking=accept-new",
  "-o", "UserKnownHostsFile=~/.ssh/known_hosts_weave_engine",
  "-i", "/absolute/path/to/weave_engine",
  "weave@ENGINE_ADDRESS",
  "{{command}}",
]

forward = [
  "ssh", "-N",
  "-o", "ControlMaster=auto",
  "-o", "ControlPersist=10m",
  "-o", "ControlPath=/tmp/cm-weave-%r@%h:%p",
  "-o", "StrictHostKeyChecking=accept-new",
  "-o", "UserKnownHostsFile=~/.ssh/known_hosts_weave_engine",
  "-i", "/absolute/path/to/weave_engine",
  "-L", "{{local_port}}:127.0.0.1:{{remote_port}}",
  "weave@ENGINE_ADDRESS",
]
```

The engine's host keys live in their own `known_hosts` file. A rebuilt engine answers on the same address with a new key, and a shared file would refuse it until you edited it by hand.

Your `~/.tmux.conf` does not change. Once an engine file exists, the panel key opens a picker listing this machine and every engine. Move with the arrow keys or `j` and `k`, press enter to open one, and escape to close. Each row shows the build running there. An engine running different code from your weave is greyed out and cannot be opened until they match. An engine file cannot set `panel_key`, because the key belongs to the machine you press it on.

With no engine files, the key opens this machine's panel directly, as before.

## Signing the harness in

The agent CLI on the engine has to be logged in and has to trust the checkout. Get a shell on the box, go to the checkout agents are cut from, and start the CLI once.

```sh
cd /var/lib/weave/repos/REPO
claude
```

Log in, answer yes to the question about trusting the folder, then quit.

Do not skip the trust question. Agents work in worktrees cut from this checkout and inherit its answer. If it is unanswered, every agent you launch sits at that prompt with an empty transcript.

Both answers are kept on the data volume, so they survive a new machine image.

## Checking the engine

`weave doctor --engine remote` checks both machines in one run. Checks about your machine run locally, covering tmux, your config, the terminal, and whether the service answers through the SSH tunnel. Checks about the engine are made by the engine's own service and printed under an `on the engine` heading. If the service cannot be reached or runs a different build, the report says so once and marks the engine's checks as not run.

## Keeping the two builds in step

The panel refuses to talk to an engine running different code. It compares the commit each binary was built from, not the version each prints. So install your weave from the same release as the engine, or build it from the commit the engine was deployed from. `weave version` and `weave doctor` show both builds.

The engine is redeployed when a release is published and stays there until the next one. It is usually your own weave that has moved.

The picker shows a mismatch before anything opens, greying out the engine and naming both builds. Opening it by hand with `weave open --engine remote` shows the same refusal as a screen that closes on any key. The engine and its agents are untouched, so the fix is on your side: rebuild your weave from the commit the engine is running.

## When the engine is down

An engine can be stopped when nobody is using it, because it costs money every hour it runs. Stopping keeps the machine, so the data volume, database and address all survive and your engine file stays correct.

Every agent on the box stops with it. Their conversations and worktrees are kept, so resume the ones you want once it is back.

If the panel cannot reach the engine when you expect it to, ask whoever runs it whether the box is stopped. [Administering the engine](../engine-admin/) covers stopping and starting it.
