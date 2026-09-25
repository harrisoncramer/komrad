---
title: The remote engine
description: How we run komrad on a machine of its own, and what survives what.
group: architecture
order: 14
---

An engine is komrad running on a machine that is not your laptop. The service, the database and every agent's terminal live there. The panel stays with you.

Komrad needs little from that machine: a Postgres it can reach, tmux, the agent CLIs, and SSH in. This guide describes the engine we run for komrad's own development. It is on AWS, deployed from GitHub Actions, and wired to GitHub through a GitHub App. The Terraform and Packer in the repository build exactly that. None of it is required to run an engine. It is how we chose to.

## The pieces

```
  LAPTOP                         AWS
  +----------------+             +-----------------------------+
  |  panel         |  ssh -L     |  EC2, fixed IP              |
  |                |------------>|   komrad serve             |
  |  local tmux    |  ssh -t     |   tmux, every agent         |
  |  view          |------------>|   data volume               |
  +----------------+             +--------------+--------------+
                                                v
  GITHUB ACTIONS                 +-----------------------------+
  +----------------+   ssm       |  RDS Postgres               |
  |  release.yml   |------------>|                             |
  +----------------+             +-----------------------------+
```

One EC2 instance runs the service and every agent. It has a fixed address, and SSH is open only to the addresses we list. The database is RDS, reachable only from the instance. Its password lives in Secrets Manager, never on disk.

The agents' worktrees and the komrad user's home sit on a separate data volume. The instance can be thrown away and the volume attached to the next one.

## Boot

The machine boots from an image Packer built, with tmux, Docker, Go, Node, Claude Code, Codex and komrad already installed. A few systemd units do the rest. One mounts the data volume. One reads the database password, runs migrations, and writes the service's connection string. One starts `komrad serve`. One opens a headless panel, so the tmux session exists before the first agent needs it.

## Deploys

The engine follows release tags, not merges. Pushing a `v` tag runs the release workflow, which publishes the binary and then deploys it. The workflow assumes an AWS role with a token GitHub signs, so no key is stored anywhere. It puts the binary on the box with an SSM command, never SSH.

Agents survive a deploy. The service unit is configured so that stopping it stops only the `komrad serve` process. The tmux server it started keeps running, and the new binary picks the agents back up.

## Reaching it

An engine is a TOML file in `~/.config/komrad/engines/`. It says how to run a command on the box and how to forward a port, both over SSH. The API itself has no authentication. It listens on loopback and SSH is the only way in. The panel refuses an engine built from a different commit than itself.

On our engine, GitHub access comes from a GitHub App, so no person's token sits on a shared box. `komrad github credential` mints short-lived tokens from the App's key, and git picks them up through the credential helper. An engine without the App still runs on whatever credentials the box has.

## Stop and replace

We stop the instance overnight. The data volume, the database and the address survive. The agents do not, but their worktrees and conversations do, and you resume them in the morning.

Changing the image replaces the instance. The root disk is discarded and the data volume follows the new host. The database is untouched. `database_deletion_protection` is on, so a Terraform destroy refuses until you turn it off on purpose.
