# Engine

This stack runs komrad's own development: a control plane, an engine for each person, and RDS Postgres in private subnets. Each engine keeps a data volume that outlives its instance. You can copy it to run your own, but an engine doesn't need AWS.

Copy `terraform.tfvars.example` and `backend.hcl.example`, drop the `.example`, and fill them in. Then run `mise run infra:engine:init` and `mise run infra:engine:apply`.

The control plane lives on a Tailscale tailnet and opens no TCP ports to the internet. Set `tailscale_hostname` to its name on the tailnet and `tailscale_tailnet` to the tailnet's DNS name. Set `tailscale_auth_key` to a single-use, pre-approved auth key tagged `tag:komrad-engine`, which Terraform stores in Secrets Manager for the machine. The `engine_urls` output is each engine's address.

Each entry in `engines` is a person's engine, named by its path on the control plane, with the email they sign in with and an optional `instance_type`. Set `control_plane_ami_id` and `engine_ami_id` from `mise run packer:build`.

Set `sign_in_issuer`, `sign_in_client_id` and, on Google, `sign_in_client_secret` from the app you registered at your identity provider. Set `github_oauth_client_id` from your GitHub OAuth app.

GitHub Actions assumes the `<name>-ci` role with a token GitHub signs for each run. The role trusts only this repository's branches and its `v` tags. It can read the state, send the instances a command through Systems Manager, stop the engines, and build images. It can't log in.

[Komrad's engine](https://komrad.dev/latest/guides/komrads-engine/) and the guides after it cover the machines, the database, access, deploys and the day-to-day runbooks.
