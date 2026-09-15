# Infrastructure

This directory holds the Terraform configuration for a deployment of Weave to AWS.

It runs the complete Weave installation on one private EC2 instance. The instance holds the service, panel, tmux server, agents, repositories, worktrees, and build tools. RDS holds Weave's durable Postgres data.

Terraform also manages the private network, security groups, encrypted storage, instance role, secret delivery, logs, and other resources.

You can reach the panel over SSH, so Weave's agent controls are never exposed as a public web service.

## Authentication

Terraform reads `terraform/terraform.tfvars` on the local machine. That file names the AWS profile and the locations of its config and credentials files.

Copy `terraform/terraform.tfvars.example` to create it on another machine, then update the paths and profile name.

Run `mise run terraform:init` before the first plan. Use `mise run terraform:plan`, `mise run terraform:apply`, `mise run terraform:fmt`, or `mise run terraform:validate` from the repository root. The AWS CLI profile must already be signed in or have valid credentials.
