# Engine

This stack is the remote engine komrad's own development runs on: one EC2 instance with a data volume that outlives it, and RDS Postgres in private subnets. You can copy it to run your own, but an engine doesn't need AWS.

Copy `terraform.tfvars.example` and `backend.hcl.example`, drop the `.example`, and fill them in. Then run `mise run infra:engine:init` and `mise run infra:engine:apply`.

Set `domain_name` to the engine's domain, and add an A record for it at your DNS host, pointing at the `instance_public_ip` output. Terraform leaves the record to you, so the engine doesn't depend on any one DNS provider.

GitHub Actions assumes the `<name>-ci` role with a token GitHub signs for each run. The role trusts only this repository's branches and its `v` tags. It can read the state, send the instance a command through Systems Manager, start and stop it, and build an image. It can't log in.

[Komrad's engine](https://komrad.dev/latest/guides/komrads-engine/) and the guides after it cover the machine, the database, access, deploys and the day-to-day runbooks.
