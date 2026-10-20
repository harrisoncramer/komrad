data "aws_iam_policy_document" "ec2_assume_role" {
  statement {
    actions = ["sts:AssumeRole"]

    principals {
      type        = "Service"
      identifiers = ["ec2.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "control_plane" {
  name = "${var.name}-control-plane"

  assume_role_policy = data.aws_iam_policy_document.ec2_assume_role.json

  tags = merge(local.tags, { Name = "${var.name}-control-plane-role" })
}

resource "aws_iam_role_policy_attachment" "control_plane_ssm" {
  role       = aws_iam_role.control_plane.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"
}

resource "aws_iam_role_policy" "control_plane" {
  name = "${var.name}-control-plane"
  role = aws_iam_role.control_plane.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect   = "Allow"
        Action   = ["ssm:GetParameter", "ssm:PutParameter"]
        Resource = local.tailscale_state_arn
      },
      {
        Effect = "Allow"
        Action = ["secretsmanager:GetSecretValue"]
        Resource = [
          aws_db_instance.komrad.master_user_secret[0].secret_arn,
          aws_secretsmanager_secret.tailscale_auth_key.arn,
          aws_secretsmanager_secret.forge_proxy_ca_key.arn,
        ]
      },
      {
        Effect = "Allow"
        Action = ["secretsmanager:GetSecretValue", "secretsmanager:PutSecretValue"]
        Resource = [
          "arn:aws:secretsmanager:${var.aws_region}:${var.aws_account_id}:secret:${local.engine_secret_prefix}*",
          "arn:aws:secretsmanager:${var.aws_region}:${var.aws_account_id}:secret:${local.login_secret_prefix}*",
        ]
      },
      {
        Effect   = "Allow"
        Action   = ["ec2:DescribeInstances"]
        Resource = "*"
      },
      {
        Effect   = "Allow"
        Action   = ["ec2:StartInstances", "ec2:StopInstances"]
        Resource = "arn:aws:ec2:${var.aws_region}:${var.aws_account_id}:instance/*"
        Condition = {
          StringEquals = { "aws:ResourceTag/KomradStack" = var.name }
          Null         = { "aws:ResourceTag/KomradEngine" = "false" }
        }
      },
    ]
  })
}

resource "aws_iam_instance_profile" "control_plane" {
  name = "${var.name}-control-plane"
  role = aws_iam_role.control_plane.name
  tags = merge(local.tags, { Name = "${var.name}-control-plane-profile" })
}

resource "aws_secretsmanager_secret" "tailscale_auth_key" {
  name                    = "${var.secret_prefix}/tailscale-auth-key"
  recovery_window_in_days = 0
  tags                    = merge(local.tags, { Name = "${var.name}-tailscale-auth-key" })
}

resource "aws_secretsmanager_secret_version" "tailscale_auth_key" {
  secret_id     = aws_secretsmanager_secret.tailscale_auth_key.id
  secret_string = var.tailscale_auth_key
}

resource "tls_private_key" "forge_proxy_ca" {
  algorithm   = "ECDSA"
  ecdsa_curve = "P256"
}

resource "tls_self_signed_cert" "forge_proxy_ca" {
  private_key_pem       = tls_private_key.forge_proxy_ca.private_key_pem
  is_ca_certificate     = true
  validity_period_hours = 24 * 365 * 10
  allowed_uses          = ["cert_signing", "crl_signing", "digital_signature"]

  subject {
    common_name  = "${var.name} forge proxy"
    organization = "komrad"
  }
}

resource "aws_secretsmanager_secret" "forge_proxy_ca_key" {
  name                    = "${var.secret_prefix}/forge-proxy-ca-key"
  recovery_window_in_days = 0
  tags                    = merge(local.tags, { Name = "${var.name}-forge-proxy-ca-key" })
}

resource "aws_secretsmanager_secret_version" "forge_proxy_ca_key" {
  secret_id     = aws_secretsmanager_secret.forge_proxy_ca_key.id
  secret_string = tls_private_key.forge_proxy_ca.private_key_pem
}

resource "aws_network_interface" "control_plane" {
  subnet_id       = aws_subnet.komrad.id
  security_groups = [aws_security_group.control_plane.id]
  tags            = merge(local.tags, { Name = "${var.name}-control-plane" })
}

resource "aws_eip" "control_plane" {
  domain            = "vpc"
  network_interface = aws_network_interface.control_plane.id
  tags              = merge(local.tags, { Name = "${var.name}-control-plane" })

  depends_on = [aws_internet_gateway.komrad]
}

resource "aws_instance" "control_plane" {
  ami                  = var.control_plane_ami_id
  instance_type        = var.control_plane_instance_type
  iam_instance_profile = aws_iam_instance_profile.control_plane.name
  user_data = templatefile("${path.module}/control-plane.sh.tftpl", {
    data_volume_id            = aws_ebs_volume.control_plane_data.id
    aws_region                = var.aws_region
    stack                     = var.name
    database_secret_arn       = aws_db_instance.komrad.master_user_secret[0].secret_arn
    database_host             = aws_db_instance.komrad.address
    database_name             = aws_db_instance.komrad.db_name
    engine_secret_prefix      = local.engine_secret_prefix
    login_secret_prefix       = local.login_secret_prefix
    ca_key_secret             = aws_secretsmanager_secret_version.forge_proxy_ca_key.secret_id
    ca_certificate            = tls_self_signed_cert.forge_proxy_ca.cert_pem
    github_client_id          = var.github_oauth_client_id
    address                   = aws_network_interface.control_plane.private_ip
    engines                   = { for name, engine in var.engines : name => { email = engine.email, address = aws_network_interface.engine[name].private_ip } }
    sign_in_issuer            = var.sign_in_issuer
    sign_in_client_id         = var.sign_in_client_id
    sign_in_client_secret     = var.sign_in_client_secret
    tailscale_state_arn       = local.tailscale_state_arn
    tailscale_hostname        = var.tailscale_hostname
    tailscale_name            = local.tailscale_name
    tailscale_auth_key_secret = aws_secretsmanager_secret_version.tailscale_auth_key.secret_id
  })

  # Cloud-init runs user data only on an instance's first boot, so a change to it replaces the
  # instance, and the data volume follows.
  user_data_replace_on_change = true

  primary_network_interface {
    network_interface_id = aws_network_interface.control_plane.id
  }

  dynamic "credit_specification" {
    for_each = startswith(var.control_plane_instance_type, "t") ? [1] : []

    content {
      cpu_credits = var.cpu_credits
    }
  }

  metadata_options {
    http_tokens                 = "required"
    http_put_response_hop_limit = 1
  }

  root_block_device {
    delete_on_termination = true
    encrypted             = true
    volume_size           = 20
    volume_type           = "gp3"
    tags                  = merge(local.tags, { Name = "${var.name}-control-plane-root" })
  }

  tags = merge(local.tags, { Name = "${var.name}-control-plane", KomradStack = var.name })
}

resource "aws_ebs_volume" "control_plane_data" {
  availability_zone = aws_subnet.komrad.availability_zone
  encrypted         = true
  size              = 8
  type              = "gp3"
  tags              = merge(local.tags, { Name = "${var.name}-control-plane-data" })
}

resource "aws_volume_attachment" "control_plane_data" {
  device_name = "/dev/sdf"
  volume_id   = aws_ebs_volume.control_plane_data.id
  instance_id = aws_instance.control_plane.id
}
