resource "aws_iam_role" "weave" {
  name = "${var.name}-instance"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect = "Allow"
      Action = "sts:AssumeRole"
      Principal = {
        Service = "ec2.amazonaws.com"
      }
    }]
  })

  tags = merge(local.tags, { Name = "${var.name}-instance-role" })
}

resource "aws_iam_role_policy_attachment" "ssm" {
  role       = aws_iam_role.weave.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"
}

resource "aws_iam_instance_profile" "weave" {
  name = "${var.name}-instance"
  role = aws_iam_role.weave.name
  tags = merge(local.tags, { Name = "${var.name}-instance-profile" })
}

resource "aws_instance" "weave" {
  ami                         = var.ami_id
  instance_type               = var.instance_type
  subnet_id                   = aws_subnet.weave.id
  vpc_security_group_ids      = [aws_security_group.weave.id]
  associate_public_ip_address = true
  iam_instance_profile        = aws_iam_instance_profile.weave.name
  user_data = templatefile("${path.module}/weave-data-volume.sh.tftpl", {
    data_volume_id           = aws_ebs_volume.weave_data.id
    database_secret          = aws_secretsmanager_secret.database_url.name
    database_fallback_secret = aws_db_instance.weave.master_user_secret[0].secret_arn
    database_host            = aws_db_instance.weave.address
    database_name            = aws_db_instance.weave.db_name
    aws_region               = var.aws_region
  })

  dynamic "credit_specification" {
    for_each = startswith(var.instance_type, "t") ? [1] : []

    content {
      cpu_credits = var.cpu_credits
    }
  }

  metadata_options {
    http_tokens = "required"
  }

  root_block_device {
    delete_on_termination = true
    encrypted             = true
    volume_size           = var.root_volume_size_gib
    volume_type           = "gp3"
    tags                  = merge(local.tags, { Name = "${var.name}-root" })
  }

  tags = merge(local.tags, { Name = var.name })
}

resource "aws_eip" "weave" {
  domain   = "vpc"
  instance = aws_instance.weave.id
  tags     = merge(local.tags, { Name = "${var.name}-public-ip" })
}

resource "aws_ebs_volume" "weave_data" {
  availability_zone = aws_subnet.weave.availability_zone
  encrypted         = true
  final_snapshot    = true
  size              = var.data_volume_size_gib
  type              = "gp3"
  tags              = merge(local.tags, { Name = "${var.name}-data" })
}

resource "aws_volume_attachment" "weave_data" {
  device_name = "/dev/sdf"
  volume_id   = aws_ebs_volume.weave_data.id
  instance_id = aws_instance.weave.id
}
