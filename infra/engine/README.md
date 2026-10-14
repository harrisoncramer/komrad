# Engine

This stack is the remote engine komrad's own development runs on: one EC2 instance with a data volume that outlives it, and RDS Postgres in private subnets. You can copy it to run your own, but an engine doesn't need AWS.

Copy `terraform.tfvars.example` and `backend.hcl.example`, drop the `.example`, and fill them in. Then run `mise run infra:engine:init` and `mise run infra:engine:apply`.

The engine lives on a Tailscale tailnet and opens no ports to the internet. Set `tailscale_hostname` to the engine's name on the tailnet and `tailscale_tailnet` to the tailnet's DNS name. Before the first apply, store a single-use, pre-approved auth key tagged `tag:komrad-engine` in Secrets Manager as `komrad/tailscale-auth-key`. The machine joins with it on first boot and keeps its tailnet identity in Parameter Store after that. The `engine_url` output is the address to put in your engine file.

Set `sign_in_issuer`, `sign_in_client_id` and, on Google, `sign_in_client_secret` from the app you registered at your identity provider.

GitHub Actions assumes the `<name>-ci` role with a token GitHub signs for each run. The role trusts only this repository's branches and its `v` tags. It can read the state, send the instance a command through Systems Manager, start and stop it, and build an image. It can't log in.

[Komrad's engine](https://komrad.dev/latest/guides/komrads-engine/) and the guides after it cover the machine, the database, access, deploys and the day-to-day runbooks.
