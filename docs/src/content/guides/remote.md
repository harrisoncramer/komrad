---
title: Running komrade remotely
description: Keep the panel on your laptop and run the agents on a machine that never sleeps.
group: operations
order: 10
---

Agents running on your laptop stop when your laptop sleeps. Komrade can instead run them on a remote machine, so they keep working when you close the lid. The panel stays on your laptop and looks exactly the same. What moves is the engine: the service, the database, and every agent's terminal.

Komrade's own repository works this way, and remote agents regularly open pull requests against it. [Administering the engine](../engine-admin/) covers building and looking after that machine. This guide covers using one that already exists.

## Telling your panel about an engine

An engine is described by a small config file on your laptop, one per engine, kept in `~/.config/komrade/engines/` and named after the engine. Komrade layers it over your normal config, so it only holds what is different about that engine.

That file sets the [`[connection]`](../../config/#connection) table. [`exec`](../../config/#connection.exec) says how to run a command on the engine, usually over SSH, and [`forward`](../../config/#connection.forward) says how to carry the engine's service back to your laptop. For a host your SSH config already knows how to reach, a plain [`ssh`](../../config/#connection.ssh) destination is enough instead. The file also gives the engine a [tmux socket](../../config/#connection.tmux_socket) and [cache directory](../../config/#connection.cache_dir) of its own on your side, so its panes never mix with your laptop's.

Keep the engine's SSH host keys in a file of their own. A rebuilt engine answers on the same address with a new key, and a shared known-hosts file would refuse it.

## Choosing where to open the panel

Once an engine file exists, opening komrade first shows a picker listing your laptop and each engine. Each engine shows whether it is ready. If an engine cannot be reached, or runs a different build from yours, it is greyed out and says why. Closing a panel you opened from the picker brings you back to it.

<figure class="placeholder">
  <figcaption>The engine picker, with this machine ready, one engine ready, and one greyed out for a build mismatch.</figcaption>
</figure>

With no engine files, komrade opens your laptop's panel directly, as before.

## Signing the agent CLI in

Agents on the engine run a CLI that has to be logged in and has to trust the checkout it works in. Do that once on the engine. Start the CLI in the checkout agents are cut from, log in, and accept the question about trusting the folder.

Do not skip the trust question. Agents inherit the answer. If it is never given, every agent sits waiting at that question with nothing in its transcript. Both answers live on the engine's data disk, so they survive the machine being rebuilt.

## Keeping the two builds in step

Your komrade and the engine's komrade have to be the same build, and komrade checks this by the commit each was built from. Install yours from the same release the engine runs, or build it from the commit the engine was deployed from.

The engine moves when a release is published and stays put until the next one. So when the two disagree, it is usually your laptop that has moved on. Nothing on the engine is harmed by a mismatch. The fix is always on your side: get your komrade onto the engine's build. `komrade doctor` checks both machines in one run and says what to do.

## When the engine is off

An engine costs money every hour it runs, so it may be switched off when nobody is using it. Switching off keeps everything: the disk, the database and the address. Your engine file stays correct.

Every agent stops while the engine is off. Their conversations and worktrees are kept, so resume the ones you want once it is back. If the panel cannot reach the engine when you expect it to, check with whoever runs it.
