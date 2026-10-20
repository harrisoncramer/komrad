resource "aws_iam_role" "engine" {
  for_each = var.engines

  name = "${var.name}-engine-${each.key}"

  assume_role_policy = data.aws_iam_policy_document.ec2_assume_role.json

  tags = merge(local.tags, { Name = "${var.name}-engine-${each.key}-role" })
}

resource "aws_iam_role_policy_attachment" "engine_ssm" {
  for_each = var.engines

  role       = aws_iam_role.engine[each.key].name
  policy_arn = "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"
}

resource "aws_iam_role_policy" "engine" {
  for_each = var.engines

  name = "${var.name}-engine-${each.key}"
  role = aws_iam_role.engine[each.key].id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = ["secretsmanager:GetSecretValue"]
      Resource = aws_secretsmanager_secret.engine[each.key].arn
    }]
  })
}

resource "aws_iam_instance_profile" "engine" {
  for_each = var.engines

  name = "${var.name}-engine-${each.key}"
  role = aws_iam_role.engine[each.key].name
  tags = merge(local.tags, { Name = "${var.name}-engine-${each.key}-profile" })
}

# The control plane writes each engine's database password and forge proxy credential here.
resource "aws_secretsmanager_secret" "engine" {
  for_each = var.engines

  name                    = "${local.engine_secret_prefix}${each.key}"
  recovery_window_in_days = 0
  tags                    = merge(local.tags, { Name = "${var.name}-engine-${each.key}" })
}

# The control plane writes each person's GitHub login here once they authorize it.
resource "aws_secretsmanager_secret" "login" {
  for_each = var.engines

  name                    = "${local.login_secret_prefix}${each.key}"
  recovery_window_in_days = 0
  tags                    = merge(local.tags, { Name = "${var.name}-login-${each.key}" })
}

resource "aws_network_interface" "engine" {
  for_each = var.engines

  subnet_id       = aws_subnet.komrad.id
  security_groups = [aws_security_group.engine.id]
  tags            = merge(local.tags, { Name = "${var.name}-engine-${each.key}" })
}

resource "aws_eip" "engine" {
  for_each = var.engines

  domain            = "vpc"
  network_interface = aws_network_interface.engine[each.key].id
  tags              = merge(local.tags, { Name = "${var.name}-engine-${each.key}" })

  depends_on = [aws_internet_gateway.komrad]
}

resource "aws_instance" "engine" {
  for_each = var.engines

  ami                  = var.engine_ami_id
  instance_type        = each.value.instance_type
  iam_instance_profile = aws_iam_instance_profile.engine[each.key].name
  user_data = templatefile("${path.module}/engine.sh.tftpl", {
    engine                = each.key
    data_volume_id        = aws_ebs_volume.engine_data[each.key].id
    aws_region            = var.aws_region
    engine_secret         = aws_secretsmanager_secret.engine[each.key].name
    database_host         = aws_db_instance.komrad.address
    database_name         = aws_db_instance.komrad.db_name
    address               = aws_network_interface.engine[each.key].private_ip
    control_plane_address = aws_network_interface.control_plane.private_ip
    ca_certificate        = tls_self_signed_cert.forge_proxy_ca.cert_pem
    sign_in_issuer        = var.sign_in_issuer
    sign_in_client_id     = var.sign_in_client_id
    sign_in_client_secret = var.sign_in_client_secret
  })

  # Cloud-init runs user data only on an instance's first boot, so a change to it replaces the
  # instance, and the data volume follows.
  user_data_replace_on_change = true

  primary_network_interface {
    network_interface_id = aws_network_interface.engine[each.key].id
  }

  dynamic "credit_specification" {
    for_each = startswith(each.value.instance_type, "t") ? [1] : []

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
    volume_size           = var.root_volume_size_gib
    volume_type           = "gp3"
    tags                  = merge(local.tags, { Name = "${var.name}-engine-${each.key}-root" })
  }

  tags = merge(local.tags, {
    Name         = "${var.name}-engine-${each.key}"
    KomradStack  = var.name
    KomradEngine = each.key
  })
}

resource "aws_ebs_volume" "engine_data" {
  for_each = var.engines

  availability_zone = aws_subnet.komrad.availability_zone
  encrypted         = true
  final_snapshot    = true
  size              = var.data_volume_size_gib
  type              = "gp3"
  tags              = merge(local.tags, { Name = "${var.name}-engine-${each.key}-data" })
}

resource "aws_volume_attachment" "engine_data" {
  for_each = var.engines

  device_name = "/dev/sdf"
  volume_id   = aws_ebs_volume.engine_data[each.key].id
  instance_id = aws_instance.engine[each.key].id
}
