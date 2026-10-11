# Docs site

The website at komrad.dev, an Astro site served via a private S3 bucket via CloudFront.

```
   browser
      |  https://komrad.dev/latest/guides/agents
      v
   Cloudflare DNS -----> CloudFront distribution
                         |
                         |  viewer-request function
                         |    www.komrad.dev   ->  301 to komrad.dev
                         |    /                ->  302 to /latest/
                         |    /latest/guides/agents  rewritten to /latest/guides/agents/index.html
                         |
                         |  signed read, allowed only from this distribution
                         v
   S3 bucket komrade.dev (private)
      versions.json  robots.txt
      latest/     index.html  404.html  sitemap-index.xml  guides/agents/index.html  _astro/  pagefind/
      v0.3.0/     ...
```

## The Buckets + Cloudfront

The bucket holds every published version of the docs, under its own prefix.

The  `latest/` directory holds the rolling build of main, and `v0.3.0/` and so on, for releases.

Each is a complete static site built with that prefix as its base path. The `versions.json` file lists what exists, and the version picker on the site reads it.

Cloudfront reads the private bucket through a small function via the `viewer-request.js.tftpl` function. This function sends `www` to the bare domain, sends the root to `latest/`, and turns a request for a directory into a request for the `index.html` inside it.

## Publishing

Run `mise run docs:deploy <version>` to build the site as that version, upload it under its prefix, fold it into `versions.json`, and invalidate the cache.

## The Initial Deploy

Copy the `backend.hcl.example` to `backend.hcl`, then `mise run infra:docs:init` to initialize the infrastructure.

Then run the `mise run infra:docs:apply` command. Applying needs a Cloudflare API token in `CLOUDFLARE_API_TOKEN` that can read the four zones and edit their DNS and redirect rules.

Setting `engine_ip` to the engine's `instance_public_ip` output points `engine.komrad.dev` at it. Set `domain_name` to `engine.komrad.dev` in the engine's variables so its certificate is for that name.
