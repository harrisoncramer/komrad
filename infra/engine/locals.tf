locals {
  tags = merge(var.tags, {
    Environment = "development"
  })

  tailscale_name            = "${var.tailscale_hostname}.${var.tailscale_tailnet}"
  tailscale_state_parameter = "/${var.name}/tailscale/state"
  tailscale_state_arn       = "arn:aws:ssm:${var.aws_region}:${var.aws_account_id}:parameter${local.tailscale_state_parameter}"
}
