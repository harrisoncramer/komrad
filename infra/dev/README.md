# Dev assets

A bucket for things made while working on komrad that should be reachable from any machine but
never appear on komrad.dev. It is served at https://dev.komrad.dev through CloudFront, the same
way the docs site is: the bucket is private and only the distribution can read it.

```
   https://dev.komrad.dev/recordings/prod/first-workflow.mp4
      |
      v
   Cloudflare DNS -----> CloudFront distribution
                         |  signed read, allowed only from this distribution
                         v
   S3 bucket komrad-dev-assets (private)
      recordings/prod/   mirrors recordings/prod, rendered docs videos before they are published
      recordings/dev/    mirrors recordings/dev, scratch recordings, expired after thirty days
```

Add a top-level prefix for a new kind of asset rather than a new bucket. A prefix whose objects
should not live forever goes in `expiring_prefixes` with its age in days.

Anyone who can apply this stack can already write to the bucket. `writer_role_names` attaches
write access to other roles, such as the engine's instance role, so an agent running there can
upload what it records.

## Running it

Copy `backend.hcl.example` to `backend.hcl` and `terraform.tfvars.example` to
`terraform.tfvars`, then `mise run infra:dev:init` and `mise run infra:dev:apply`. Applying needs
a Cloudflare API token in `CLOUDFLARE_API_TOKEN` that can edit DNS in the komrad.dev zone. The
engine stack has to exist first when its role is named in `writer_role_names`.

Upload with `mise run infra:dev:recordings upload <prod|dev>`.
