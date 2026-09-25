output "bucket_name" {
  value = aws_s3_bucket.site.id
}

output "cloudfront_distribution_id" {
  value = aws_cloudfront_distribution.site.id
}

output "domain_name" {
  value = var.domain_name
}

output "aws_region" {
  value = var.aws_region
}

output "state_bucket" {
  value = var.state_bucket
}
