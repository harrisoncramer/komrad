output "instance_id" {
  value = aws_instance.weave.id
}

output "instance_public_ip" {
  value = aws_eip.weave.public_ip
}

output "database_endpoint" {
  value = aws_db_instance.weave.address
}

output "database_secret_arn" {
  value = aws_db_instance.weave.master_user_secret[0].secret_arn
}
