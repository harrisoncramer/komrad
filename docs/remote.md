# Running weave on a remote box

Weave can run remotely, where Postgres, tmux, and the agents moves to a box you reach over SSH. Agents keep running when you close your laptop.

The panel (and Go binary for launching) are still required on your machine.

Please work through the steps in this guide in order. Each one depends on the one before it.

## Before you start

An AWS account and a named profile in `~/.aws/config` that reaches it.

An S3 bucket for Terraform state. Terraform does not create this for you.

A key pair used for nothing else.

```
ssh-keygen -t ed25519 -f ~/.ssh/weave_engine
```

The toolchain.

```
mise install
```

## 1. Build the machine image

```
mise run packer:build
```

Takes about twenty minutes. Note the AMI id it prints at the end.

## 2. Describe the infrastructure

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

## 3. Create the infrastructure

```
mise run terraform:init
mise run terraform:apply
```

Creates the network, the box, the data volume, Postgres, and the permissions between them. Prints the address when it finishes.

## 4. Register a GitHub App

The engine has to fetch and push, so it needs its own GitHub login. A GitHub App is the way to give it one without leaving a password on the machine. The App holds a private key, and weave uses that key to get a token that stops working after an hour.

This covers git and nothing else. It does not cover `gh`. That tool reads a token from its own environment instead of asking git, so `gh pr create` on the engine will still stop and ask you to log in.

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

## 5. Store the App key

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

## 6. Configure the engine

Get on the box.

```
mise run remote:connect
```

Write `~/.config/weave/config.toml`:

```toml
default_harness = "claude"

[worktrees]
dir = "/var/lib/weave/worktrees"

[github]
app_id           = "APP_ID"
installation_id  = "INSTALLATION_ID"
private_key_path = "/run/weave/github.pem"
```

The key is fetched from Secrets Manager into `/run` at boot, which is tmpfs, so it never touches the disk. Check it arrived:

```
weave github token
```

That prints a token starting `ghs_`. If it errors, the secret name, the App id or the installation id is wrong.

## 7. Clone the repo and create a workflow

Put the checkout on the data volume so it survives an instance replacement.

```
git clone https://github.com/OWNER/REPO /var/lib/weave/repos/REPO
```

Git authenticates through weave's credential helper, so this needs no login.

## 8. Point the panel at it

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
  "-i", "/absolute/path/to/weave_engine",
  "weave@ENGINE_ADDRESS",
  "{{command}}",
]

forward = [
  "ssh", "-N",
  "-o", "ControlMaster=auto",
  "-o", "ControlPersist=10m",
  "-o", "ControlPath=/tmp/cm-weave-%r@%h:%p",
  "-i", "/absolute/path/to/weave_engine",
  "-L", "{{local_port}}:127.0.0.1:{{remote_port}}",
  "weave@ENGINE_ADDRESS",
]
```

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

## 9. Sign the harness in

You have to grant permissions inside the remote for the agent harness.

```
mise run remote:connect
cd /var/lib/weave/repos/REPO
claude
```

Accept the login, answer yes to the folder-trust question, then quit.

_Do not skip the trust question. Agents run in worktrees cut from this checkout and inherit its answer, so leaving it unanswered means every agent you launch sits waiting at that prompt with an empty transcript._

Weave's home is on the data volume, so both survive a new image.

## Updating

The binary changes often. This does not disturb running agents. If you have this repo checked out locally, you can run this Mise helper:

```
mise run remote:deploy
```

The panel refuses to talk to an engine running other code, and it decides that on the commit each binary was built from rather than on the version each prints. The two are stamped differently on purpose, so `weave version` and `weave doctor` show the engine as `v0.0.0-engine.<commit>` and your own binary as a pseudo-version, and they agree as long as both were built from the same commit. Deploy from a clean checkout and build your own weave from it.

Deploying from a tree with uncommitted changes is the exception, because there is no commit that names what was built. The deploy stamps a version instead and leaves a matching binary at `dist/weave-panel`; put that in place of your own weave, or doctor will report the engine as a different build.

The image changes rarely, when the units or provisioning scripts do. This replaces the box and every agent on it stops. The data volume and the address follow the new box; anything mid-conversation is lost.

```
mise run packer:build
# put the new ami_id in terraform.tfvars
mise run terraform:apply
```

## Getting a shell

```
mise run remote:connect
mise run remote:connect -- systemctl status weave
```

The address comes from Terraform. The connection is shared with the panel's, so this is nearly free.

## Tearing it down

```
mise run terraform:destroy
```
