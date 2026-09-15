locals {
  tags = merge(var.tags, {
    Environment = "development"
  })
}
