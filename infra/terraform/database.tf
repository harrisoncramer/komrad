resource "aws_subnet" "private" {
  for_each = var.private_subnet_cidrs

  vpc_id            = aws_vpc.weave.id
  cidr_block        = each.value
  availability_zone = each.key
  tags              = merge(local.tags, { Name = "${var.name}-private-${each.key}" })
}

resource "aws_db_subnet_group" "weave" {
  name       = var.name
  subnet_ids = [for subnet in aws_subnet.private : subnet.id]
  tags       = merge(local.tags, { Name = "${var.name}-database" })
}

resource "aws_security_group" "database" {
  name_prefix = "${var.name}-database-"
  vpc_id      = aws_vpc.weave.id

  ingress {
    from_port       = 5432
    to_port         = 5432
    protocol        = "tcp"
    security_groups = [aws_security_group.weave.id]
  }

  tags = merge(local.tags, { Name = "${var.name}-database" })

  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_db_instance" "weave" {
  identifier     = var.name
  engine         = "postgres"
  engine_version = var.database_engine_version
  instance_class = var.database_instance_class

  allocated_storage     = var.database_storage_gib
  max_allocated_storage = var.database_max_storage_gib
  storage_type          = "gp3"
  storage_encrypted     = true

  db_name  = "weave"
  username = "weave"

  manage_master_user_password = true

  db_subnet_group_name   = aws_db_subnet_group.weave.name
  vpc_security_group_ids = [aws_security_group.database.id]
  availability_zone      = var.availability_zone
  multi_az               = false
  publicly_accessible    = false

  backup_retention_period   = var.database_backup_retention_days
  copy_tags_to_snapshot     = true
  deletion_protection       = var.database_deletion_protection
  skip_final_snapshot       = false
  final_snapshot_identifier = "${var.name}-final-${formatdate("YYYYMMDDhhmmss", timestamp())}"

  auto_minor_version_upgrade = true
  apply_immediately          = true

  tags = merge(local.tags, { Name = "${var.name}-database" })

  lifecycle {
    ignore_changes = [final_snapshot_identifier]
  }
}

resource "aws_iam_role_policy" "secrets" {
  name = "${var.name}-secrets"
  role = aws_iam_role.weave.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = ["secretsmanager:GetSecretValue"]
      Resource = aws_db_instance.weave.master_user_secret[0].secret_arn
    }]
  })
}
