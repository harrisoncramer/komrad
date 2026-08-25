output "instance_id" {
  value = aws_instance.weave.id
}

output "connect_command" {
  value = "aws ssm start-session --target ${aws_instance.weave.id}"
}
