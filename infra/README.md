# Infrastructure

Two Terraform stacks live here.

The `engine/` directory is the machine komrad runs on when it is not running on a laptop.

The `docs/` stack is specific to this repo, and holds website at komrad.dev.

They share an AWS account, one state bucket, and one GitHub trust. That means the engine stack has to be applied first, with `github_repository` set.

Both stacks keep their state in the same bucket under different keys.

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

## Running Terraform

Nothing here is run with `terraform` directly. Instead, each has a mise task group that points Terraform at the right directory.

The engine is set up end to end in `docs/src/content/guides/engine-admin.md` for folks who want to replicate this setup.
