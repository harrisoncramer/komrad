output "instance_ids" {
  value = merge(
    { control-plane = aws_instance.control_plane.id },
    { for name, engine in aws_instance.engine : name => engine.id },
  )
}

output "control_plane_public_ip" {
  value = aws_eip.control_plane.public_ip
}

output "engine_urls" {
  value = { for name in keys(var.engines) : name => "https://${local.tailscale_name}/${name}" }
}

output "database_endpoint" {
  value = aws_db_instance.komrad.address
}

output "database_secret_arn" {
  value = aws_db_instance.komrad.master_user_secret[0].secret_arn
}

output "control_plane_ami" {
  value = aws_instance.control_plane.ami
}

output "engine_ami" {
  value = var.engine_ami_id
}

output "aws_region" {
  value = var.aws_region
}

output "state_bucket" {
  value = var.state_bucket
}

output "control_plane_image_parameter" {
  value = "/${var.name}/images/control-plane"
}

output "engine_image_parameter" {
  value = "/${var.name}/images/engine"
}

output "tailscale_state_parameter" {
  value = local.tailscale_state_parameter
}
