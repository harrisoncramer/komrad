---
title: Administering the engine
description: Build the remote box, deploy to it, and stop it when nobody is using it.
group: operations
order: 11
---

This guide covers everything done to the remote box from weave's repository. Using an engine that already exists is covered in [running weave remotely](../remote/).

Every command here is a mise task in a weave checkout, so you need the repository and `mise install`. AWS credentials come from the environment, as `AWS_PROFILE` or the access key variables.

## Standing one up

Work through these steps in order, because each depends on the one before.

You need an AWS account with a named profile in `~/.aws/config`, and an S3 bucket for Terraform's state, which Terraform does not create. You also need an SSH key used for nothing else.

```sh
ssh-keygen -t ed25519 -f ~/.ssh/weave_engine
```

### 1. Build the machine image

```sh
mise run packer:build
```

This takes about twenty minutes. Note the AMI id it prints at the end.

### 2. Describe the infrastructure

```sh
cp infra/terraform/terraform.tfvars.example infra/terraform/terraform.tfvars
cp infra/terraform/backend.hcl.example infra/terraform/backend.hcl
```

Fill in `terraform.tfvars`.

```
aws_region                   the region to build in
aws_account_id               your account id
aws_profile                  your profile name
aws_config_file              absolute path to your AWS config
aws_shared_credentials_file  absolute path to your AWS credentials
ami_id                       the id from step 1
availability_zone            one inside aws_region
ssh_public_key               contents of ~/.ssh/weave_engine.pub
ssh_allowed_cidrs            ["YOUR.IP.ADDR.HERE/32"]
state_bucket                 your Terraform state bucket
```

Terraform refuses `0.0.0.0/0` for `ssh_allowed_cidrs`.

The file also has three optional `github_*` values. They only let this repository's own GitHub Actions reach AWS without a stored key, so leave them out.

Point `backend.hcl` at your state bucket. Both files are ignored by git.

### 3. Create the infrastructure

```sh
mise run terraform:init
mise run terraform:apply
```

This creates the network, the machine, the data volume, Postgres, and the permissions between them. It prints the address when it finishes.

### 4. Register a GitHub App

The engine has to fetch, push and open pull requests, so it needs its own GitHub identity.

The quick way is to run `gh auth login` on the box once. That leaves a long-lived credential on a machine nobody watches, and it works until you remember to revoke it. The better way is a GitHub App, which the rest of this section sets up. The App holds a private key, and weave uses it to make tokens that expire after an hour and only reach the repositories you installed the App on.

If you would rather log in by hand, skip to step 6 and leave `[github_app]` out of the config. Weave stays out of the way without it. None of this applies to weave on your own laptop, which uses your own git and `gh` logins.

Go to https://github.com/settings/apps/new and fill in these fields.

```
GitHub App name    globally unique, and the bot name on every pull request
Homepage URL       anything, it is unused
Webhook            untick Active
Installed on       Only on this account
```

Give it these repository permissions.

```
Contents        Read and write
Pull requests   Read and write
Issues          Read and write
Workflows       Read and write
Metadata        Read-only, selected for you
```

Create the App. Under Private keys, generate a key. The `.pem` file downloads once and GitHub keeps no copy. Under Install App, install it on your account and choose your repositories.

### 5. Store the App key

```sh
aws secretsmanager create-secret \
  --profile YOUR_PROFILE --region YOUR_REGION \
  --name weave/github-app \
  --secret-string "$(jq -n \
      --arg id "APP_ID" \
      --rawfile key ~/Downloads/YOUR-APP.private-key.pem \
      '{app_id: $id, private_key: $key}')"

rm -P ~/Downloads/YOUR-APP.private-key.pem
```

The machine is already allowed to read secrets under `weave/`, so this needs no Terraform change.

### 6. Configure the engine

Get a shell on the box with `mise run remote:connect`, then write `~/.config/weave/config.toml`.

```toml
default_harness = "claude"

[worktrees]
dir = "/var/lib/weave/worktrees"

[github_app]
app_id           = "APP_ID"
installation_id  = "INSTALLATION_ID"
private_key_path = "/run/weave/github.pem"
```

The key is copied from Secrets Manager into `/run` at boot. That directory lives in memory, so the key never touches the disk. Check it arrived with `weave github token`, which should print a token starting `ghs_`. If it errors, the secret name, App id or installation id is wrong.

Git asks weave for a token each time it needs one, so nothing is stored. Agents get `gh` access through the stand-in described in [forges](../forges/#how-agents-push-and-open-pull-requests), without needing to know about it.

Then connect the engine's forge through the App. You can do this from your laptop.

```sh
weave forge connect github.com --method github-app --engine remote
```

No token of yours is stored. `weave doctor --engine remote` shows the connection under forges.

### 7. Clone the repository

Put the checkout on the data volume, so it survives the machine being replaced.

```sh
git clone https://github.com/OWNER/REPO /var/lib/weave/repos/REPO
```

Git logs in through weave's credential helper, so this needs no password. Then point your panel at the engine and sign the harness in, as [running weave remotely](../remote/) describes.

## Getting a shell

```sh
mise run remote:connect
mise run remote:connect -- systemctl status weave
```

The address comes from Terraform. The SSH connection is shared with the panel's, so opening one is nearly free.

## Deploying a new binary

Publishing a release deploys the engine, without disturbing running agents. To deploy by hand from a clean checkout, run `mise run remote:deploy`.

A clean deploy carries the checkout's commit, which the panel compares against. Deploying with uncommitted changes has no commit to name, so the deploy stamps a version instead. It also leaves a matching binary at `dist/weave-panel` for you to use as your own weave.

You can deploy any ref without local AWS credentials by running the `engine` workflow from GitHub's Actions tab. That is the way in when your own machine is the problem.

A stopped engine is skipped rather than failed, because it cannot receive a deploy. That build reaches it with the next deploy after it starts.

## Taking a new machine image

The image changes rarely, only when the system units or setup scripts do. Run the `image` workflow to build one. It records the new image without applying it, because applying stops every agent on the box.

```sh
mise run remote:reimage
```

This replaces the machine with the latest image CI built. It names the agents that are about to stop and asks first. Every agent on the box stops and does not come back. The data volume, database and address all carry over, so what is lost is whatever those agents were in the middle of. The old host key is dropped from `~/.ssh/known_hosts_weave_engine` for you.

Pass an image id to use a different one, or build your own with `mise run packer:build`.

## Restarting the service

```sh
mise run remote:restart
mise run remote:restart --hard
```

A plain restart leaves tmux alone, so agents keep running. `--hard` restarts tmux too, which is needed to pick up changes to the manager session, and every agent stops.

## Stopping and starting the box

The machine costs money every hour it runs, and it runs far more hours than it is used.

```sh
mise run remote:stop
mise run remote:start
```

Stopping asks first, because every agent stops with the box. Each loses the turn it was in the middle of. Their conversations, worktrees and the database are all kept, so you can resume agents from the panel afterwards.

Stopping keeps the machine rather than replacing it. The data volume stays attached, the host key survives, and the address stays the same, so your engine file stays correct.

Starting waits for the machine's health checks, which take a few minutes. Running either command against an engine already in that state does nothing and reports what it found.

Terraform does not track whether the machine is running. `terraform plan` shows no changes against a stopped engine, and `apply` will not start it. Do not stop and reimage at the same time, because the reimage replaces the machine the stop is waiting on.

Stopping does not end every cost. The volumes, database, snapshots and address are still billed. The machine itself, which is most of the bill, stops costing anything.

### The nightly schedule

Two GitHub Actions workflows, `engine stop` and `engine start`, run those commands on a timer. They reach AWS the same way the deploy does, but talk to the EC2 API rather than the box, so they need nothing on the machine.

They are live once merged. Either can be run by hand from the Actions tab, or with `gh workflow run "engine stop"`.

GitHub's timers use UTC and ignore daylight saving. The stop lands at 10pm in New York for half the year and 9pm for the other half, and the start moves with it. The scheduled stop answers its own confirmation, because nobody is there at night and the work survives.

## Tearing it down

```sh
mise run terraform:destroy
```

`database_deletion_protection` is on by default, so this refuses until you set it to false and apply.
