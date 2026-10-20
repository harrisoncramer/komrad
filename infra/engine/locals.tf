locals {
  tags = merge(var.tags, {
    Environment = "development"
  })

  engine_secret_prefix = "${var.secret_prefix}/engines/"
  login_secret_prefix  = "${var.secret_prefix}/logins/"

  tailscale_name            = "${var.tailscale_hostname}.${var.tailscale_tailnet}"
  tailscale_state_parameter = "/${var.name}/tailscale/state"
  tailscale_state_arn       = "arn:aws:ssm:${var.aws_region}:${var.aws_account_id}:parameter${local.tailscale_state_parameter}"
}
