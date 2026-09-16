# Infrastructure

Terraform for weave's own remote engine: one EC2 instance, a subnet, a fixed public address, an encrypted data volume mounted at `/var/lib/weave`, and Postgres on RDS in private subnets reachable only from the instance.

Setting one up is documented in `docs/remote.md`, which covers the whole path from a built image to a working panel. Nothing here is run directly; the tasks are `mise run terraform:init`, `plan`, `apply`, `destroy`, `fmt` and `validate` from the repository root.

Two things worth knowing before you apply. Changing `ami_id`, moving the database, or editing the boot script all replace the instance and stop every agent on it, which is the upgrade path rather than an accident: the data volume detaches and follows the new host, and the root volume is discarded. And `database_deletion_protection` defaults to true, so a destroy refuses until it is set false and applied.

The image itself is built by Packer from `packer/`, which holds the toolchain, the harnesses, weave's binary and the boot-time units.
