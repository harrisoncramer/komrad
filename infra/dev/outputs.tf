output "bucket_name" {
  value = aws_s3_bucket.assets.id
}

output "cloudfront_distribution_id" {
  value = aws_cloudfront_distribution.assets.id
}

output "domain_name" {
  value = var.domain_name
}

output "writer_policy_arn" {
  value = aws_iam_policy.writer.arn
}
