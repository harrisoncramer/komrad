# ec2-weave

Terraform module that provisions a single EC2 instance for running weave remotely: an Ubuntu box with tmux, a local Postgres 18, and the weave binary installed, migrated, and passing `weave doctor`.

This is the first phase. It does not yet configure git, `gh`, or Claude Code auth, start the weave service, or bind the tmux prefix key for a login shell. That lands once this phase is confirmed working. Once it does, you connect, follow the rest of the [quickstart](../../../docs/install.md), and the box behaves like any other weave install.

## What it creates

- One EC2 instance on the account's default VPC, running the latest Ubuntu 24.04 LTS AMI for the chosen architecture
- A security group with no ingress rule at all - nothing listens for inbound connections
- An IAM role and instance profile granting only `AmazonSSMManagedInstanceCore`, the only way onto the box
- A gp3 root volume sized for the OS, the binary, and Postgres's data directory

Postgres listens on localhost only, with the same `weave`/`weave` credentials as the checkout's own `docker-compose.yml`. That is fine for a single-user box with no inbound port 5432; it becomes a real secret once this instance is reachable by anyone else.

There's no `terraform.tfvars` to fill in - every variable has a default, so `mise run tf:apply` works as-is. Override `region`, `instance_type`, `architecture`, or `root_volume_size_gb` only if you want something other than the defaults.

## Running it

All commands go through mise, matching the rest of the repo:

```sh
mise run tf:init
mise run tf:plan
mise run tf:apply
```

## Connecting

No SSH, no key, no IP allowlist - everything goes through AWS Session Manager, which authenticates by IAM identity rather than network location or a key. That means it works the same from home, office, or anywhere else your AWS CLI is logged in, dynamic IP or not.

```sh
mise run tf:connect
```

Looks up the instance id itself and connects, using the same isolated credentials as every other `tf:*` task - never `aws configure`'s ambient default profile. It lands you directly as the `ubuntu` user, not Session Manager's default `ssm-user` - a different account, with a different home directory and none of the `/etc/environment` values a plain `sudo` login picks up, which everything this module set up (the Postgres role, `WEAVE_DATABASE_URL`, `.tmux.conf`) actually depends on. From there, `tmux -L weave attach` gets you to the same place SSH would. It requires the AWS CLI's Session Manager plugin locally, and your own IAM identity needs `ssm:StartSession` on the instance - the module only grants the instance side of that permission (via the IAM role above), not yours.

To tear it down:

```sh
mise run tf:destroy
```

`mise run tf:fmt` formats every `.tf` file under `infra/`, and `mise run tf:validate` checks the module without touching AWS.

## After apply

`/var/log/weave-bootstrap.log` on the instance has the full first-boot log, including the `weave doctor` output the bootstrap script runs at the end.
