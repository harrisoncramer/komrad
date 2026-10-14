# Infrastructure

Two Terraform stacks live here. `engine/` is the machine komrad's own development runs on. `docs/` is the website at komrad.dev.

They share an AWS account, one state bucket and one GitHub trust, so apply the engine stack first, with `github_repository` set. Each stack keeps its state under its own key in that bucket.

Nothing here is run with `terraform` directly. Each stack has a mise task group, such as `mise run infra:engine:plan`, that points Terraform at the right directory and backend.

The remote engine guides describe the engine stack:

- [Running komrad](https://komrad.dev/latest/guides/running-komrad/) names what any engine needs.
- [Komrad's engine](https://komrad.dev/latest/guides/komrads-engine/) maps those pieces onto this stack.
- [Standing up an engine](https://komrad.dev/latest/guides/standing-up-an-engine/) builds it from nothing.
