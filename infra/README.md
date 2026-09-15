# Infrastructure

This directory holds the Terraform configuration for Weave's AWS development host.

The first layer creates one Ubuntu EC2 instance, a public subnet, a fixed public IP, a restricted SSH rule, Systems Manager access, and an encrypted data volume. The instance has the public IP because SSH reaches the terminal panel. Weave itself stays on loopback.

The data volume mounts at `/var/lib/weave`. The Go build and module caches, weave's worktrees, and its cache directory all live there, so the disks that grow without bound are on the volume sized for growth and survive an instance replacement. A systemd unit performs the mount on every boot rather than only the first, so a slow volume attachment costs a retry rather than leaving the host quietly writing to its root disk. Weave's own unit checks that `/var/lib/weave` is a mount point before it starts, so a host whose volume never arrived runs no weave rather than writing to its root disk.

The mount finds its disk by serial rather than by device name. EBS volumes attached to a Nitro instance appear to the guest as NVMe devices whose serial is the volume id with the dashes removed, and the name they are attached under is not the name the guest sees.

Destroying the stack removes the host and volume after AWS takes a recovery snapshot.

Reach the host with `aws ssm start-session --target "$(terraform output -raw instance_id)"`.

Terraform pins the AWS account, availability zone, and machine image in local configuration. This prevents a default AWS profile or a new image from replacing the host by accident.

The image itself is built by Packer from `packer/`, which is where the toolchain, the harnesses, weave's binary, and the boot-time units live. Terraform receives a finished AMI id and adds almost nothing at boot: its user data writes the data volume id, starts the mount unit, then starts weave.

So the order of operations for a new host is: run `mise run packer:build`, take the AMI id it prints at the end, put it in `terraform.tfvars` as `ami_id`, then plan and apply.

Changing `ami_id` replaces the instance. That is the upgrade path rather than an accident: a new image means a new host, the data volume is detached and reattached to it, and everything on the root volume is discarded. The two volumes have different jobs for that reason. The root volume is disposable and holds only what the image put there, while `/var/lib/weave` holds the things that must outlive a host, which is why weave's worktrees, its cache, and the Go build and module caches are all pointed at it.

The image carries no credentials. Weave's systemd unit requires `/etc/weave/database.env`, which the image does not contain, so the unit is skipped rather than failed and a freshly booted host runs every dependency and no weave until secret delivery puts that file in place.

## Authentication

Terraform reads `terraform/terraform.tfvars` on the local machine. That file names the AWS account, profile, image, availability zone, and the locations of its config and credentials files.

Copy `terraform/terraform.tfvars.example` to create it on another machine, then update the paths and profile name.

Terraform also reads `terraform/backend.hcl`. Copy `terraform/backend.hcl.example` and set the existing encrypted S3 state bucket. The S3 backend locks state with lock files, so two checkouts cannot apply changes at the same time.

Run `mise run terraform:init` before the first plan. Use `mise run terraform:plan`, `mise run terraform:apply`, `mise run terraform:destroy`, `mise run terraform:fmt`, or `mise run terraform:validate` from the repository root. The AWS CLI profile must already be signed in or have valid credentials.
