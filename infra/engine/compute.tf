resource "aws_iam_role" "komrade" {
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
  role       = aws_iam_role.komrade.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"
}

resource "aws_iam_instance_profile" "komrade" {
  name = "${var.name}-instance"
  role = aws_iam_role.komrade.name
  tags = merge(local.tags, { Name = "${var.name}-instance-profile" })
}

resource "aws_instance" "komrade" {
  ami                         = var.ami_id
  instance_type               = var.instance_type
  subnet_id                   = aws_subnet.komrade.id
  vpc_security_group_ids      = [aws_security_group.komrade.id]
  associate_public_ip_address = true
  iam_instance_profile        = aws_iam_instance_profile.komrade.name
  user_data = templatefile("${path.module}/komrade-data-volume.sh.tftpl", {
    data_volume_id      = aws_ebs_volume.komrade_data.id
    database_secret_arn = aws_db_instance.komrade.master_user_secret[0].secret_arn
    database_host       = aws_db_instance.komrade.address
    database_name       = aws_db_instance.komrade.db_name
    aws_region          = var.aws_region
    ssh_public_key      = var.ssh_public_key
  })

  # User data now carries the database's address and secret, which move if RDS is ever replaced
  # or restored. Writing them to a running instance would stop and start it to change a file
  # cloud-init never re-reads, so the host would lose its agents and keep the old address. A
  # replacement runs the script, and the data volume follows it, which is the same path an image
  # change already takes.
  user_data_replace_on_change = true

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

resource "aws_eip" "komrade" {
  domain   = "vpc"
  instance = aws_instance.komrade.id
  tags     = merge(local.tags, { Name = "${var.name}-public-ip" })
}

resource "aws_ebs_volume" "komrade_data" {
  availability_zone = aws_subnet.komrade.availability_zone
  encrypted         = true
  final_snapshot    = true
  size              = var.data_volume_size_gib
  type              = "gp3"
  tags              = merge(local.tags, { Name = "${var.name}-data" })
}

resource "aws_volume_attachment" "komrade_data" {
  device_name = "/dev/sdf"
  volume_id   = aws_ebs_volume.komrade_data.id
  instance_id = aws_instance.komrade.id
}
