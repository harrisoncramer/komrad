# Explainer: this is how CI reaches the account without a key anybody could steal.
#
# GitHub signs a short-lived token for each workflow run saying which repository and which branch
# it is running for. AWS is told to trust that signature, and to hand back credentials only when
# the token says this repository. Nothing long-lived is stored in GitHub, so there is no secret to
# rotate and nothing useful to leak out of a fork.

# GitHub now writes immutable numeric ids into the subject it signs, so that a repository name
# freed up and taken by somebody else does not inherit this trust. Both spellings are accepted,
# and naming the ids in tfvars pins the newer one to this repository rather than to its name.
locals {
  github_parts = var.github_repository == "" ? ["", ""] : split("/", var.github_repository)
  github_owner = local.github_parts[0]
  github_name  = local.github_parts[1]

  github_identified = var.github_owner_id != "" && var.github_repository_id != ""

  # A release runs for a tag rather than a branch, so both refs are named. Without the tag arm the
  # deploy a release triggers cannot assume this role at all. Only v tags cut releases, so only
  # those are trusted.
  github_refs = ["refs/heads/*", "refs/tags/v*"]

  github_subjects = flatten([
    for ref in local.github_refs : [
      "repo:${var.github_repository}:ref:${ref}",
      local.github_identified
      ? "repo:${local.github_owner}@${var.github_owner_id}/${local.github_name}@${var.github_repository_id}:ref:${ref}"
      : "repo:${local.github_owner}@*/${local.github_name}@*:ref:${ref}",
    ]
  ])
}

resource "aws_iam_openid_connect_provider" "github" {
  count = var.github_repository == "" ? 0 : 1

  url             = "https://token.actions.githubusercontent.com"
  client_id_list  = ["sts.amazonaws.com"]
  thumbprint_list = ["6938fd4d98bab03faadb97b34396831e3780aea1", "1c58a3a8518e8759bf075b76b750d4f2df264fcd"]

  tags = local.tags
}

data "aws_iam_policy_document" "github_assume" {
  count = var.github_repository == "" ? 0 : 1

  statement {
    actions = ["sts:AssumeRoleWithWebIdentity"]

    principals {
      type        = "Federated"
      identifiers = [aws_iam_openid_connect_provider.github[0].arn]
    }

    condition {
      test     = "StringEquals"
      variable = "token.actions.githubusercontent.com:aud"
      values   = ["sts.amazonaws.com"]
    }

    # Only this repository's own refs, so a fork's pull request cannot assume the role.
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

  # Reading Terraform's state is how CI finds the instance to deploy to, and the same bucket is
  # where the binary is staged for the instance to fetch.
  statement {
    actions   = ["s3:GetObject", "s3:PutObject", "s3:DeleteObject"]
    resources = ["arn:aws:s3:::${var.state_bucket}/*"]
  }

  statement {
    actions   = ["s3:ListBucket"]
    resources = ["arn:aws:s3:::${var.state_bucket}"]
  }

  # Deploying is one command sent to the one instance, rather than a login.
  statement {
    actions   = ["ssm:SendCommand"]
    resources = ["arn:aws:ssm:${var.aws_region}::document/AWS-RunShellScript"]
  }

  statement {
    actions   = ["ssm:SendCommand"]
    resources = ["arn:aws:ec2:${var.aws_region}:${var.aws_account_id}:instance/*"]

    condition {
      test     = "StringEquals"
      variable = "ssm:resourceTag/Project"
      values   = ["komrade"]
    }
  }

  statement {
    actions   = ["ssm:GetCommandInvocation", "ssm:ListCommandInvocations"]
    resources = ["*"]
  }

  # Where the image built by CI is recorded, since the variable naming it is not in the repository.
  statement {
    actions = ["ssm:PutParameter", "ssm:GetParameter"]
    resources = [
      "arn:aws:ssm:${var.aws_region}:${var.aws_account_id}:parameter/${var.name}/engine/*",
    ]
  }

  statement {
    actions   = ["ec2:DescribeInstances", "ec2:DescribeInstanceStatus", "ec2:DescribeTags"]
    resources = ["*"]
  }

  # Stopping the engine overnight is a schedule rather than a person, so CI is allowed to stop and
  # start this one instance and nothing else in the account.
  statement {
    actions   = ["ec2:StartInstances", "ec2:StopInstances"]
    resources = ["arn:aws:ec2:${var.aws_region}:${var.aws_account_id}:instance/*"]

    condition {
      test     = "StringEquals"
      variable = "ec2:ResourceTag/Project"
      values   = ["komrade"]
    }
  }
}

resource "aws_iam_role_policy" "github" {
  count = var.github_repository == "" ? 0 : 1

  name   = "${var.name}-ci"
  role   = aws_iam_role.github[0].id
  policy = data.aws_iam_policy_document.github[0].json
}

# Explainer: building an image is a different shape of permission from deploying a binary.
#
# Packer starts an instance of its own, waits for it, photographs its disk and throws the instance
# away, so it needs to create and destroy things rather than talk to something that already exists.
# It is a separate policy so that the deploy path, which runs on every merge, does not carry the
# right to launch instances.

data "aws_iam_policy_document" "github_image" {
  count = var.github_repository == "" ? 0 : 1

  statement {
    actions = [
      "ec2:AttachVolume",
      "ec2:AuthorizeSecurityGroupIngress",
      "ec2:CopyImage",
      "ec2:CreateImage",
      "ec2:CreateKeypair",
      "ec2:CreateSecurityGroup",
      "ec2:CreateSnapshot",
      "ec2:CreateTags",
      "ec2:CreateVolume",
      "ec2:DeleteKeyPair",
      "ec2:DeleteSecurityGroup",
      "ec2:DeleteSnapshot",
      "ec2:DeleteVolume",
      "ec2:DeregisterImage",
      "ec2:DescribeImageAttribute",
      "ec2:DescribeImages",
      "ec2:DescribeInstances",
      "ec2:DescribeInstanceStatus",
      "ec2:DescribeRegions",
      "ec2:DescribeSecurityGroups",
      "ec2:DescribeSnapshots",
      "ec2:DescribeSubnets",
      "ec2:DescribeTags",
      "ec2:DescribeVolumes",
      "ec2:DescribeVpcs",
      "ec2:DetachVolume",
      "ec2:GetPasswordData",
      "ec2:ModifyImageAttribute",
      "ec2:ModifyInstanceAttribute",
      "ec2:ModifySnapshotAttribute",
      "ec2:RegisterImage",
      "ec2:RunInstances",
      "ec2:StopInstances",
      "ec2:TerminateInstances",
    ]
    resources = ["*"]
  }
}

resource "aws_iam_role_policy" "github_image" {
  count = var.github_repository == "" ? 0 : 1

  name   = "${var.name}-ci-image"
  role   = aws_iam_role.github[0].id
  policy = data.aws_iam_policy_document.github_image[0].json
}

output "ci_role_arn" {
  value       = var.github_repository == "" ? "" : aws_iam_role.github[0].arn
  description = "The role CI assumes, set as the AWS_CI_ROLE variable in the repository."
}
