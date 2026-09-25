locals {
  tags = merge(var.tags, {
    Environment = "production"
  })
}
