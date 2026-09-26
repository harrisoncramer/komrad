# Infrastructure

Two Terraform stacks live here, one per directory. `engine/` is the machine komrad runs on
when it is not running on a laptop. `docs/` is the website at komrad.dev. They share an AWS
account, one state bucket, and one GitHub trust, and nothing else.

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

Both stacks keep their state in the same bucket under different keys. The bucket itself is not
managed here. It is named in a `backend.hcl` you copy from each directory's example, and in CI it
is written from the `TF_STATE_BUCKET` repository variable.

## Running Terraform

Nothing here is run with `terraform` directly. Each stack has a mise task group that points
Terraform at the right directory: `mise run infra:engine:init`, `plan`, `apply`, `destroy`,
`fmt`, `validate`, and the same under `infra:docs:`. CI checks formatting on every pull request
that touches this directory.

The engine is set up end to end in `docs/src/content/guides/engine-admin.md`. Each directory's
own README says what it builds and what to be careful of.
