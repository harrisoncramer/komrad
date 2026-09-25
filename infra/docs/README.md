# Docs site

The website at komrad.dev. A private S3 bucket holds the built pages, CloudFront serves them
under an ACM certificate, and Cloudflare holds the DNS for every komrade domain. Terraform builds
those and nothing else. The pages are put there by a script.

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

komrad.ai, komrade.app and komrade.dev never reach CloudFront. A redirect rule in each Cloudflare
zone sends them to komrad.dev, keeping the path and query.

The bucket keeps the name komrade.dev, since a bucket cannot be renamed and every published
version lives in it.

## What is in the bucket

Every published version of the docs sits under its own prefix, `latest/` for the rolling build of
main and `v0.3.0/` and so on for releases. Each is a complete static site built with that prefix
as its base path, so nothing in one version links into another. `versions.json` at the root lists
what exists, and the version picker on the site reads it.

Every page carries a canonical link to its twin under `latest/`, and each version builds its own
sitemap, but `robots.txt` at the root names only latest's. So search engines crawl every version
and index one.

## Why there is a function

CloudFront reads the bucket through an Origin Access Control, which keeps the bucket private but
loses the directory-index behaviour an S3 website endpoint would give. So a small function runs
on every request before the cache.

It sends www to the bare domain, sends the root to `latest/`, and turns a request for a directory into a request for the `index.html` inside it.

Paths under `_astro/` and `pagefind/` and paths with a known file extension are left alone.

The function is `viewer-request.js.tftpl`. The only templated part is the domain name.

## Publishing

Runs `mise run docs:deploy <version>` to build the site as that version, upload it under its prefix,
fold it into `versions.json`, and invalidate the cache.

## Running it

Copy `backend.hcl.example` to `backend.hcl`, then `mise run infra:docs:init` and
`mise run infra:docs:apply` command. Applying needs a Cloudflare API token in
`CLOUDFLARE_API_TOKEN` that can read the four zones and edit their DNS and redirect rules.
