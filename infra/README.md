# Infrastructure

This directory holds the Terraform configuration for Weave's AWS development host.

The first layer creates one Ubuntu EC2 instance, a public subnet, a fixed public IP, a restricted SSH rule, Systems Manager access, and an encrypted data volume. The instance has the public IP because SSH reaches the terminal panel. Weave itself stays on loopback.

The data volume mounts at `/var/lib/weave`. Destroying the stack removes the host and volume after AWS takes a recovery snapshot.

Terraform pins the AWS account, availability zone, and Ubuntu AMI in local configuration. This prevents a default AWS profile or a new Ubuntu release from replacing the host by accident.

## Authentication

Terraform reads `terraform/terraform.tfvars` on the local machine. That file names the AWS account, profile, image, availability zone, and the locations of its config and credentials files.

Copy `terraform/terraform.tfvars.example` to create it on another machine, then update the paths and profile name.

Terraform also reads `terraform/backend.hcl`. Copy `terraform/backend.hcl.example` and set the existing encrypted S3 state bucket. The S3 backend locks state with lock files, so two checkouts cannot apply changes at the same time.

Run `mise run terraform:init` before the first plan. Use `mise run terraform:plan`, `mise run terraform:apply`, `mise run terraform:destroy`, `mise run terraform:fmt`, or `mise run terraform:validate` from the repository root. The AWS CLI profile must already be signed in or have valid credentials.
