# Infrastructure

This directory holds the Terraform configuration for Weave's AWS development host.

The first layer creates one Ubuntu EC2 instance, a subnet, a fixed public address, an encrypted data volume, and an instance role that Systems Manager accepts.

Nothing may connect to the instance. Its security group has no inbound rules at all, there is no SSH key on it, and port 22 is closed. Reaching it is a Systems Manager session, which the instance opens outward, so access is an IAM decision rather than a key plus an allowed address range. That matters on a machine whose whole purpose is running coding agents, and it also means a changing home IP never locks you out.

Every agent on this host can read the database. `weave open` puts the connection string in the tmux server's environment, so each agent window inherits it, and the read-only login weave derives has a fixed password. Any process on the machine can also reach the instance metadata service and so the instance role. This is therefore a host you trust the agents on. Making it one you do not means blocking 169.254.169.254 for the weave user, which is the next step after the NAT gateway rather than something this layer does.

The public address is only there so the instance can reach out: to Systems Manager, to package registries, to GitHub, and to the model APIs the agents use. Nothing reaches in through it. Moving the instance to a private subnet behind a NAT gateway would remove even that, at roughly thirty dollars a month, and is the next step rather than a thing this layer does.

Weave itself stays on loopback, and the panel reaches its API by forwarding a port over the same Systems Manager session.

Postgres runs on RDS in private subnets that have no route out, reachable only from the instance's security group. Its password is created and held by AWS, and the instance reads it at boot through its own role, so nobody types a connection string and no password is written into Terraform state.

The data volume mounts at `/var/lib/weave`. The Go build and module caches, weave's worktrees, and its cache directory all live there, so the disks that grow without bound are on the volume sized for growth and survive an instance replacement. A systemd unit performs the mount on every boot rather than only the first, so a slow volume attachment costs a retry rather than leaving the host quietly writing to its root disk. Weave's own unit checks that `/var/lib/weave` is a mount point before it starts, so a host whose volume never arrived runs no weave rather than writing to its root disk.

The mount finds its disk by serial rather than by device name. EBS volumes attached to a Nitro instance appear to the guest as NVMe devices whose serial is the volume id with the dashes removed, and the name they are attached under is not the name the guest sees.

Destroying the stack removes the host and volume after AWS takes a recovery snapshot. The database refuses, because `database_deletion_protection` defaults to true: set it false and apply before the destroy will complete, which is deliberate for the store holding the panel's whole history.

Reach the host with `aws ssm start-session --target "$(terraform output -raw instance_id)"`, which needs the Session Manager plugin; `mise.toml` pins it.

A session arrives as its own account rather than as the one that owns weave, so `sudo -u weave -i` is how you become it.

Terraform pins the AWS account, availability zone, and machine image in local configuration. This prevents a default AWS profile or a new image from replacing the host by accident.

The image itself is built by Packer from `packer/`, which is where the toolchain, the harnesses, weave's binary, and the boot-time units live. Terraform receives a finished AMI id and adds almost nothing at boot: its user data writes the data volume id, starts the mount unit, then starts weave.

So the order of operations for a new host is: run `mise run packer:build`, take the AMI id it prints at the end, put it in `terraform.tfvars` as `ami_id`, then plan and apply.

Changing `ami_id` replaces the instance, and so does anything that moves the database, since the host is told its address and its secret through user data. Replacing RDS or restoring it from a snapshot therefore costs the host as well, and the agents on it. That is the upgrade path rather than an accident: a new image means a new host, the data volume is detached and reattached to it, and everything on the root volume is discarded. The two volumes have different jobs for that reason. The root volume is disposable and holds only what the image put there, while `/var/lib/weave` holds the things that must outlive a host, which is why weave's worktrees, its cache, and the Go build and module caches are all pointed at it.

The image carries no credentials. A unit fetches the database password at boot and writes `/etc/weave/database.env`, and weave's own unit is skipped until that file exists, so a host that cannot reach its secret runs no weave rather than failing in a loop.

The GitHub and harness logins are not delivered by machinery. Sign in to the host once, over a Session Manager session, with `gh auth login` and the first-run flows for Claude and Codex, exactly as on a laptop. The weave user's home is on the data volume, so those logins survive a new image and are a one-time cost rather than something repeated on every upgrade.

## Authentication

Terraform reads `terraform/terraform.tfvars` on the local machine. That file names the AWS account, profile, image, availability zone, and the locations of its config and credentials files.

Copy `terraform/terraform.tfvars.example` to create it on another machine, then update the paths and profile name.

Terraform also reads `terraform/backend.hcl`. Copy `terraform/backend.hcl.example` and set the existing encrypted S3 state bucket. The S3 backend locks state with lock files, so two checkouts cannot apply changes at the same time.

Run `mise run terraform:init` before the first plan. Use `mise run terraform:plan`, `mise run terraform:apply`, `mise run terraform:destroy`, `mise run terraform:fmt`, or `mise run terraform:validate` from the repository root. The AWS CLI profile must already be signed in or have valid credentials.
