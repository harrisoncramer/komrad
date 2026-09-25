# The engine's OIDC provider already exists in this account, so this looks it up rather than
# creating a second one, which AWS refuses. See infra/engine/ci.tf for the fuller explainer
# on why this trust needs no long-lived secret.
data "aws_iam_openid_connect_provider" "github" {
  count = var.github_repository == "" ? 0 : 1

  url = "https://token.actions.githubusercontent.com"
}

locals {
  github_parts      = var.github_repository == "" ? ["", ""] : split("/", var.github_repository)
  github_owner      = local.github_parts[0]
  github_name       = local.github_parts[1]
  github_identified = var.github_owner_id != "" && var.github_repository_id != ""

  # main publishes latest and a version tag publishes its own docs, so only those refs are trusted.
  github_refs = ["refs/heads/main", "refs/tags/v*"]

  github_subjects = flatten([
    for ref in local.github_refs : [
      "repo:${var.github_repository}:ref:${ref}",
      local.github_identified
      ? "repo:${local.github_owner}@${var.github_owner_id}/${local.github_name}@${var.github_repository_id}:ref:${ref}"
      : "repo:${local.github_owner}@*/${local.github_name}@*:ref:${ref}",
    ]
  ])
}

data "aws_iam_policy_document" "github_assume" {
  count = var.github_repository == "" ? 0 : 1

  statement {
    actions = ["sts:AssumeRoleWithWebIdentity"]

    principals {
      type        = "Federated"
      identifiers = [data.aws_iam_openid_connect_provider.github[0].arn]
    }

    condition {
      test     = "StringEquals"
      variable = "token.actions.githubusercontent.com:aud"
      values   = ["sts.amazonaws.com"]
    }

    condition {
      test     = "StringLike"
      variable = "token.actions.githubusercontent.com:sub"
      values   = local.github_subjects
    }
  }
}

resource "aws_iam_role" "github" {
  count = var.github_repository == "" ? 0 : 1

  name               = "${var.name}-ci"
  assume_role_policy = data.aws_iam_policy_document.github_assume[0].json
  tags               = local.tags
}

data "aws_iam_policy_document" "github" {
  count = var.github_repository == "" ? 0 : 1

  statement {
    actions   = ["s3:GetObject"]
    resources = ["arn:aws:s3:::${var.state_bucket}/*"]
  }

  statement {
    actions   = ["s3:ListBucket"]
    resources = ["arn:aws:s3:::${var.state_bucket}"]
  }

  # Publishing a version: writing its files and folding it into versions.json.
  statement {
    actions   = ["s3:GetObject", "s3:PutObject", "s3:DeleteObject"]
    resources = ["${aws_s3_bucket.site.arn}/*"]
  }

  statement {
    actions   = ["s3:ListBucket"]
    resources = [aws_s3_bucket.site.arn]
  }

  statement {
    actions   = ["cloudfront:CreateInvalidation"]
    resources = [aws_cloudfront_distribution.site.arn]
  }
}

resource "aws_iam_role_policy" "github" {
  count = var.github_repository == "" ? 0 : 1

  name   = "${var.name}-ci"
  role   = aws_iam_role.github[0].id
  policy = data.aws_iam_policy_document.github[0].json
}

output "ci_role_arn" {
  value       = var.github_repository == "" ? "" : aws_iam_role.github[0].arn
  description = "The role CI assumes, set as the AWS_DOCS_CI_ROLE variable in the repository."
}
