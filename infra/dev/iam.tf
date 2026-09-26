data "aws_iam_policy_document" "writer" {
  statement {
    actions   = ["s3:GetObject", "s3:PutObject", "s3:DeleteObject"]
    resources = ["${aws_s3_bucket.assets.arn}/*"]
  }

  statement {
    actions   = ["s3:ListBucket"]
    resources = [aws_s3_bucket.assets.arn]
  }
}

resource "aws_iam_policy" "writer" {
  name   = "${var.name}-writer"
  policy = data.aws_iam_policy_document.writer.json
  tags   = local.tags
}

resource "aws_iam_role_policy_attachment" "writer" {
  for_each = toset(var.writer_role_names)

  role       = each.value
  policy_arn = aws_iam_policy.writer.arn
}
