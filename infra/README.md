# Infrastructure

Three Terraform stacks live here, one per directory. `engine/` is the machine komrad runs on
when it is not running on a laptop. `docs/` is the website at komrad.dev. `dev/` is a bucket at
dev.komrad.dev for recordings and other assets that are shared between machines but never
published. They share an AWS account and one state bucket, and nothing else; engine and docs
also share one GitHub trust.

```
                      GitHub Actions
                    (OIDC, no stored keys)
                     /                \
          engine role                 docs role
               |                          |
               v                          v
   +--------------------+     +---------------------------+
   |  engine/           |     |  docs/                    |
   |                    |     |                           |
   |  EC2 + data volume |     |  S3 bucket (private)      |
   |  RDS Postgres      |     |  CloudFront + certificate |
   |  VPC, SSH, SSM     |     |  Cloudflare DNS           |
   +--------------------+     +---------------------------+
               \                          /
                \                        /
              one S3 bucket holding both states
              engine/terraform.tfstate, docs/terraform.tfstate
```

## How the two relate

The engine stack creates the GitHub OIDC provider for the account. AWS allows only one per
issuer, so the docs stack looks it up instead of making its own. That means the engine stack has
to be applied first, with `github_repository` set, before the docs stack is applied with it set.
Each stack then has its own IAM role with only the permissions its workflows need.

Every stack keeps its state in the same bucket under its own key. The bucket itself is not
managed here. It is named in a `backend.hcl` you copy from each directory's example, and in CI it
is written from the `TF_STATE_BUCKET` repository variable.

## Running Terraform

Nothing here is run with `terraform` directly. Each stack has a mise task group that points
Terraform at the right directory: `mise run infra:engine:init`, `plan`, `apply`, `destroy`,
`fmt`, `validate`, and the same under `infra:docs:` and `infra:dev:`. CI checks formatting on
every pull request that touches this directory.

The engine is set up end to end in `docs/src/content/guides/engine-admin.md`. Each directory's
own README says what it builds and what to be careful of.
