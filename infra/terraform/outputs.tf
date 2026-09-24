output "instance_id" {
  value = aws_instance.komrade.id
}

output "instance_public_ip" {
  value = aws_eip.komrade.public_ip
}

output "database_endpoint" {
  value = aws_db_instance.komrade.address
}

output "database_secret_arn" {
  value = aws_db_instance.komrade.master_user_secret[0].secret_arn
}

output "instance_ami" {
  value = aws_instance.komrade.ami
}

output "aws_region" {
  value = var.aws_region
}

output "state_bucket" {
  value = var.state_bucket
}

output "ami_parameter" {
  value = "/${var.name}/engine/ami-id"
}
