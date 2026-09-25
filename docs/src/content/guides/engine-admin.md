---
title: Administering the engine
description: Build the remote machine, keep it up to date, and switch it off when nobody is using it.
group: operations
order: 11
---

This guide is for whoever runs the remote engine: building it, keeping it up to date, and keeping its bill down. Using an engine that already exists is covered in [running komrade remotely](../remote/).

Everything here runs from a komrade checkout as a mise task, and reaches AWS with the credentials in your environment.

## What gets built

The engine is one machine on AWS with a separate data disk and a managed Postgres database. The machine is disposable. Everything that matters lives on the data disk or in the database: agents' conversations and worktrees, komrade's config, and the logins the agent CLIs use. So the machine can be switched off, or thrown away and rebuilt from a new image, without losing work.

The engine pushes code and opens pull requests as a GitHub App rather than as you. The App's key is kept in AWS Secrets Manager and copied into memory on the machine when it boots, so it never touches the disk. From that key, komrade makes GitHub tokens that expire after an hour and only reach the repositories you chose.

## Standing one up

Do these in order, because each step depends on the one before.

First, get the prerequisites ready. You need an AWS account with a named profile, an S3 bucket for Terraform's state (Terraform does not create it), and an SSH key used for nothing else.

Build the machine image with `mise run packer:build`. It takes a while and prints an image id at the end, which the infrastructure needs. It builds in us-east-1 unless you pass another region.

Register a GitHub App on your account, with no webhook, installed only on the repositories the engine should work in. It needs to read and write repository contents, pull requests, issues and workflows. Generate a private key for it, then store that key in Secrets Manager as `komrade/github-app` before creating the infrastructure. The machine only copies the key in when it boots, so storing it first means the first boot finds it.

Describe the infrastructure by copying the two example files in [`infra/engine`](https://github.com/harrisoncramer/komrade/tree/main/infra/engine) and filling them in. They cover your account, region, the image id, your SSH key, and the addresses allowed to connect over SSH. Terraform refuses to let the whole internet in. Fill in the three `github_*` values too. They let this repository's GitHub Actions reach AWS, and deploys, new images and the nightly schedule all depend on them. The defaults assume us-east-1, so another region needs its subnets overridden as well.

Create the infrastructure with `mise run infra:engine:init` and then `mise run infra:engine:apply`. It builds the network, the machine, the data disk, the database and the permissions between them, and prints the engine's address.

Configure the engine. Get a shell on it with `mise run remote:connect`. Komrade's config file already exists there, set up by the image, so add the [`[github_app]`](../../config/#github_app) table to it with your App's id, its installation id and the key's path in memory. Check it works with `komrade github token`, which should print a token. Then connect the engine's forge through the App, which you can do from your laptop. [Forges](../forges/) describes what that connection is for.

Finally, clone the repositories agents will work in onto the data disk, so they survive the machine being rebuilt. Git authenticates through komrade, so this needs no password. Then point your panel at the engine and sign the agent CLI in, as [running komrade remotely](../remote/) describes.

## Keeping it up to date

Publishing a release deploys it to the engine automatically. A deploy restarts the service but leaves the agents running. You can also deploy by hand with `mise run remote:deploy`, or deploy any branch from GitHub's Actions tab. That is the way in when your own machine is the problem.

A deploy from a clean checkout carries its commit, which is what your panel compares against. A deploy from a checkout with uncommitted changes has no commit to name, so it leaves a matching panel binary for you to use.

The machine image changes rarely, only when the machine's setup does. Building one from the Actions tab records it without applying it, because applying it replaces the machine and stops every agent. `mise run remote:reimage` applies it. It names the agents about to stop and asks first. Their conversations and worktrees survive on the data disk, but whatever they were in the middle of is lost.

`mise run remote:restart` restarts the service and leaves agents running. A hard restart also restarts the terminal server, which stops every agent.

## Keeping the bill down

The machine costs money every hour it runs, and it runs for far more hours than it is used. `mise run remote:stop` switches it off and `mise run remote:start` switches it back on. Stopping asks first, because every agent stops with it. Nothing is lost except the turn each agent was in the middle of. The disk, the database and the address all stay, so everyone's engine files stay correct.

A GitHub Actions schedule does this every night, stopping the engine in the late evening and starting it again in the morning, New York time. Either can also be run by hand from the Actions tab.

Stopping the machine does not stop every cost. The disks, database, snapshots and address are still billed. The machine itself is most of the bill.

## Tearing it down

`mise run infra:engine:destroy` removes everything. The database is protected from deletion by default, so turn that protection off first. Tearing down keeps a final snapshot of the database.
