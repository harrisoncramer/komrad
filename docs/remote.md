# Running weave on a remote box

Weave can run remotely, where Postgres, tmux and the agents move to a box you reach over SSH. Agents keep running when you close your laptop.

The panel still runs on your machine. What moves is the engine: the service, the database, and every agent's tmux window.

Standing that box up, and everything done to it afterwards, is weave's own administration and lives in [engine-admin.md](engine-admin.md). This page is what you need once one exists.

## Pointing the panel at it

An engine is a file in `~/.config/weave/engines/` on your own machine, layered over your config rather than replacing it. Write `~/.config/weave/engines/remote.toml`:

```toml
[connection]
addr               = "127.0.0.1:8787"
tmux_socket        = "weave-remote"
engine_tmux_socket = "weave"
cache_dir          = "~/.cache/weave-remote"
panel_key          = "G"

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

The engine keeps its host keys in a file of its own. The address outlives the instance, so a rebuilt engine answers on it with a key nothing has seen before, and a shared `known_hosts` would refuse the connection until you edited it by hand.

Add a second line to `~/.tmux.conf` for the remote engine, beside the one you already have for the local panel.

```
run-shell 'weave tmux'                  # local panel, key from your config.toml
run-shell 'weave tmux --engine remote'  # remote panel, key from remote.toml
```

With `panel_key = "G"` above, G now opens the remote panel and your existing key still opens the local one. Give the two engines different keys.

Source the configuration:

```
tmux source-file ~/.tmux.conf
```

Open the panel with that key rather than by running `weave open`. The key opens it as a popup, which keeps its splits and copy-mode inside the popup.

## Signing the harness in

You have to grant permissions inside the remote for the agent harness. Get a shell on the box, go to the checkout agents will be cut from, and start the harness once:

```
cd /var/lib/weave/repos/REPO
claude
```

Accept the login, answer yes to the folder-trust question, then quit.

_Do not skip the trust question. Agents run in worktrees cut from this checkout and inherit its answer, so leaving it unanswered means every agent you launch sits waiting at that prompt with an empty transcript._

Weave's home is on the data volume, so both survive a new image.

## Keeping the two builds in step

The panel refuses to talk to an engine running other code, and it decides that on the commit each binary was built from rather than on the version each prints. So your own weave and the engine's have to come from the same place: install both from the same release, or build yours from the commit the engine was deployed from. `weave version` and `weave doctor` show both builds, the engine as its release tag or `v0.0.0-engine.<commit>` and your own as whatever it was built as.

The engine is deployed when a release is published, so it sits at the last release until somebody cuts another, and it is usually the weave on your own machine that has moved.

The refusal happens as the panel opens, once the forward to the engine is up. Instead of drawing, it names both builds and closes on any key. The engine is untouched by this and every agent on it keeps running, so the fix is on your own machine: rebuild your weave from the commit the engine is serving, which is what `weave doctor` says too.

## The box is not always up

The engine bills for every hour it is running, so it is stopped overnight and started again in the morning. Stopping keeps the instance rather than replacing it: the data volume, the database and the address all survive, so the file above stays correct and your panel needs no change.

Every agent on the box stops with it. Their conversations and worktrees are on the data volume, so resume the ones you want once it is back.

If the panel cannot reach the engine at a time you did not expect, that is the first thing to check.
