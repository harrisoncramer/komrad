# Administering weave's engine

Everything done to the remote box from this repository. Pointing a panel at an engine that already exists is in [remote.md](../remote.md).

Every command here is a task in this checkout, so it needs the repo and `mise install`. AWS credentials come from the environment, as `AWS_PROFILE` or the access key variables.

## Standing one up

Work through these in order. Each depends on the one before it.

You need an AWS account with a named profile in `~/.aws/config`, an S3 bucket for Terraform state, which Terraform does not create for you, and a key pair used for nothing else:

```
ssh-keygen -t ed25519 -f ~/.ssh/weave_engine
```

### 1. Build the machine image

```
mise run packer:build
```

Takes about twenty minutes. Note the AMI id it prints at the end.

### 2. Describe the infrastructure

```
cp infra/terraform/terraform.tfvars.example infra/terraform/terraform.tfvars
cp infra/terraform/backend.hcl.example infra/terraform/backend.hcl
```

Fill in `terraform.tfvars`:

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

> NOTE: This file also has three `github_*` values. They are optional and unrelated to the app, they let this repository's own GitHub Actions get AWS credentials without a stored key. Leave them out.

Point `backend.hcl` at your state bucket. Both files are gitignored.

### 3. Create the infrastructure

```
mise run terraform:init
mise run terraform:apply
```

Creates the network, the box, the data volume, Postgres, and the permissions between them. Prints the address when it finishes.

### 4. Register a GitHub App

The engine has to fetch, push and open pull requests, so it needs its own GitHub login.

You have two ways to give it one. You can run `gh auth login` on the box once and be done in a minute. That leaves a password on a machine nobody watches, and it works until you remember to revoke it. Or you can register a GitHub App, which is the rest of this section. The App holds a private key, and weave uses that key to get a token that stops working after an hour and only reaches the repositories you installed the App on.

The App is more setup and better security. Everything below assumes you picked it. If you would rather log in by hand, skip to step 6 and leave the `[github_app]` section out of the config. Weave stays out of the way when that section is missing, so git and `gh` use whatever the machine was logged in as.

None of this applies to weave on your own laptop. That already uses your own git and `gh` credentials.

Go to https://github.com/settings/apps/new and fill in:

```
GitHub App name    globally unique, becomes the bot name on every PR
Homepage URL       anything, unused
Webhook            untick Active
Installed on       Only on this account
```

Repository permissions you'd like Weave to use. I'd recommend:

```
Contents        Read and write
Pull requests   Read and write
Issues          Read and write
Workflows       Read and write
Metadata        Read-only, selects itself
```

Create it.

Under Private keys, Generate a private key. The `.pem` downloads once and GitHub keeps no copy.

Under Install App, install it on your account, choose your repositories.

### 5. Store the App key

```
aws secretsmanager create-secret \
  --profile YOUR_PROFILE --region YOUR_REGION \
  --name weave/github-app \
  --secret-string "$(jq -n \
      --arg id "APP_ID" \
      --rawfile key ~/Downloads/YOUR-APP.private-key.pem \
      '{app_id: $id, private_key: $key}')"

rm -P ~/Downloads/YOUR-APP.private-key.pem
```

The instance role already reads `weave/*`, so this needs no Terraform change.

### 6. Configure the engine

Get on the box.

```
mise run remote:connect
```

Write `~/.config/weave/config.toml`:

```toml
default_harness = "claude"

[worktrees]
dir = "/var/lib/weave/worktrees"

[github_app]
app_id           = "APP_ID"
installation_id  = "INSTALLATION_ID"
private_key_path = "/run/weave/github.pem"
```

The key is fetched from Secrets Manager into `/run` at boot, which is tmpfs, so it never touches the disk. Check it arrived:

```
weave github token
```

That prints a token starting `ghs_`. If it errors, the secret name, the App id or the installation id is wrong.

Git gets this token by asking weave every time it needs one, so nothing is stored. The `gh` command cannot ask, because it only reads a token from its own environment at startup. So the image puts a small wrapper ahead of `gh` on the path which fetches a fresh token and then runs the real one. An agent does not have to know about any of this.

Then connect the engine's forge through the App, which can be done from the laptop:

```
weave forge connect github.com --method github-app --engine remote
```

No token of yours is stored. The service keeps the App's short-lived token in `github-token.json` under its cache directory, readable only by the weave user and worthless an hour after it was minted, and reuses it until then. `weave doctor --engine remote` shows the connection under forges, and [forges](../forges.md) says the rest.

### 7. Clone the repo

Put the checkout on the data volume so it survives an instance replacement.

```
git clone https://github.com/OWNER/REPO /var/lib/weave/repos/REPO
```

Git authenticates through weave's credential helper, so this needs no login.

Then point your panel at it and sign the harness in, both of which are in [remote.md](../remote.md).

## Getting a shell

```
mise run remote:connect
mise run remote:connect -- systemctl status weave
```

The address comes from Terraform. The connection is shared with the panel's, so this is nearly free.

## Deploying a new binary

A release deploys the engine when it is published, and it does not disturb running agents. By hand, from a clean checkout:

```
mise run remote:deploy
```

A clean deploy carries that checkout's commit, which is what the panel compares against. Deploying from a tree with uncommitted changes is the exception, because there is no commit that names what was built: the deploy stamps a version instead and leaves a matching binary at `dist/weave-panel`, which you put in place of your own weave.

You can also deploy any ref without local AWS credentials, by dispatching the `engine` workflow on it from the Actions tab. That is the way in when the machine in front of you is the problem.

A stopped engine is skipped rather than failed, since Systems Manager refuses an instance that is not running. That build reaches the box whenever something is next deployed after it starts.

## Taking a new image

The image changes rarely, when the units or provisioning scripts do. Dispatch the `image` workflow to build one; it records the id without applying it, because applying stops every agent on the box.

```
mise run remote:reimage
```

That moves the engine to the image CI last built and replaces the instance. It names the agents that are about to stop and asks first. Every agent on the box goes and does not come back. The data volume, the database and the address all follow the new instance, so what is lost is whatever those agents were part way through.

The old host key goes with the old instance, and the reimage drops it from `~/.ssh/known_hosts_weave_engine` for you.

Pass an image id to use one other than the latest, or build your own with `mise run packer:build` and pass that.

## Restarting the service

```
mise run remote:restart
mise run remote:restart --hard
```

The plain restart leaves the tmux server alone, so the agents keep running. `--hard` takes the tmux server too, which is what picks up a change to the manager session itself, and every agent on the box stops.

This is the one way to stop and start the service. `weave stop` on the box refuses, since the unit only restarts the service after a failure and a clean stop would stay down until root ran `systemctl start weave`, which the `weave` user cannot. `weave doctor` from that shell cannot read the database url either, which is in a root-only file only the unit hands the service, so its database line says so instead of pinging the built-in default. `systemctl status weave` shows the service from the box, and `weave doctor --engine remote` from your laptop asks it for its own checks.

## Stopping and starting the box

The instance bills for every hour it is running, and it is running for far more hours than it is used.

```
mise run remote:stop
mise run remote:start
```

The stop asks before it does anything, because every agent on the box stops with it. What is lost is the turn each one was part way through. The conversations, the worktrees and the database are all on the data volume or in RDS, so an agent can be resumed from the panel afterwards.

This is not the reimage path. The instance is kept rather than replaced, so the data volume stays attached, the root disk and its host key survive, and the Elastic IP stays associated throughout. The panel's engine file names the address literally, and it stays correct.

The start waits for the instance checks to pass, which take minutes, by which time weave has long been serving. Running either command against an engine already in the state it asks for costs nothing and reports what it found.

Terraform is not involved. It does not track whether an instance is running, so `terraform plan` reports no changes against a stopped engine and a later `apply` will not start it. Nothing stops you running a stop and a reimage at once, so do not: the reimage is replacing the instance the stop is waiting on.

Stopping does not stop everything on the bill. The two 100 GiB volumes, the database, the snapshots and the address all keep costing what they cost. What goes away is the instance itself, which is the large part.

### The nightly schedule

Two GitHub Actions workflows, `engine stop` and `engine start`, run those same two commands, reaching AWS through the same OIDC role the deploy uses. They talk to the EC2 API rather than to the box, so unlike the deploy they need nothing on the instance and no way in past the security group.

They are live once merged, with nothing to turn on. Either one runs by hand from the Actions tab or with `gh workflow run "engine stop"`.

GitHub's cron is UTC and does not follow daylight saving, so the stop is 10pm in New York for half the year and 9pm for the other half, and the start moves with it. That is close enough.

The scheduled stop passes --yes, since nobody is there to answer at 10pm and the work itself survives.

## Tearing it down

```
mise run terraform:destroy
```

`database_deletion_protection` defaults to true, so this refuses until it is set false and applied.
