output "instance_id" {
  value = aws_instance.weave.id
}

output "instance_public_ip" {
  value = aws_eip.weave.public_ip
}
