# Engine

This directory holds the infrastructure behind one remote komrad engine.

This engine is used to help maintain komrad itself. However, you can refer to it as an example of how you could deploy your own remote instance.

```
                     External Internet
                              |
                              v
   +---------------------- VPC ----------------------------+
   |                                                        |
   |   public subnet                                        |
   |   +------------------------------+                     |
   |   |  EC2 instance  (fixed IP)    |                     |
   |   |  komrad serve               |                     |
   |   |  /var/lib/komrad  <---------+---- EBS data volume |
   |   +--------------+---------------+       (encrypted,   |
   |                  |                        follows the  |
   |                  | 5432, only from         instance)   |
   |                  | the instance                        |
   |                  v                                     |
   |   private subnets                                      |
   |   +------------------------------+                     |
   |   |  RDS Postgres                |                     |
   |   |  password in Secrets Manager |                     |
   |   +------------------------------+                     |
   +--------------------------------------------------------+
```

## Deployments

First, copy the `terraform.tfvars.example` file and the `backend.hcl.example` file, fill them in, and run `mise run infra:engine:init` command.

This initializes the terraform provider.

Next, run the `mise run infra:engine:apply` command, to deploy the infrastructure.

For more information on deploying and managing the engine, read: `docs/src/content/guides/engine-admin.md`.

## This Engine's Relationship to CI/CD

Komrad's GitHub Actions assume the `<name>-ci` role, in this case `komrad-development-ci` by default, using a token GitHub signs for each run.

The role trusts only this repository's branches and its `v` tags, so a fork cannot use it.

This can be used to read Terraform state to find the instance, send it an SSM command, start and stop it, and build an image with Packer. It cannot log in.
