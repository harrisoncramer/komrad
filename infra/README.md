# infra

Infrastructure for running weave outside a local checkout.

- [aws/ec2-weave](aws/ec2-weave): Terraform module that provisions an EC2 instance with tmux, Postgres, and the weave binary installed and passing `weave doctor`.

Run everything through mise (`mise run tf:init`, `tf:plan`, `tf:apply`, `tf:destroy`, `tf:connect`, `tf:fmt`, `tf:validate`) rather than calling `terraform`/`aws` directly - besides pinning the tool versions in `mise.toml`, each task forces the isolated `weave` AWS profile itself, so it can't silently pick up whatever credentials happen to be in your shell.
