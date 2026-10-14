# Docs site

This stack serves the website at komrad.dev from a private S3 bucket through CloudFront, with DNS and redirects on Cloudflare.

Every published version of the docs lives under its own prefix in the bucket, `latest/` for main and one per release tag, with `versions.json` at the root for the version picker. `viewer-request.js.tftpl` sends `www` to the bare domain, sends the root to `latest/`, and maps a directory to its `index.html`.

Apply the engine stack first, since this one uses its GitHub trust. Copy `backend.hcl.example` to `backend.hcl`, run `mise run infra:docs:init`, then `mise run infra:docs:apply`. Applying needs a Cloudflare API token in `CLOUDFLARE_API_TOKEN` that can read the four zones and edit their DNS and redirect rules.

`mise run docs:deploy <version>` builds and publishes one version.
