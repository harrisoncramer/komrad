resource "aws_subnet" "private" {
  for_each = var.private_subnet_cidrs

  vpc_id            = aws_vpc.komrad.id
  cidr_block        = each.value
  availability_zone = each.key
  tags              = merge(local.tags, { Name = "${var.name}-private-${each.key}" })
}

resource "aws_db_subnet_group" "komrad" {
  name       = var.name
  subnet_ids = [for subnet in aws_subnet.private : subnet.id]
  tags       = merge(local.tags, { Name = "${var.name}-database" })
}

resource "aws_security_group" "database" {
  name_prefix = "${var.name}-database-"
  vpc_id      = aws_vpc.komrad.id
  tags        = merge(local.tags, { Name = "${var.name}-database" })

  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_vpc_security_group_ingress_rule" "database" {
  for_each = {
    control_plane = aws_security_group.control_plane.id
    engine        = aws_security_group.engine.id
  }

  security_group_id            = aws_security_group.database.id
  referenced_security_group_id = each.value
  ip_protocol                  = "tcp"
  from_port                    = 5432
  to_port                      = 5432
  tags                         = local.tags
}

resource "aws_db_instance" "komrad" {
  identifier     = var.name
  engine         = "postgres"
  engine_version = var.database_engine_version
  instance_class = var.database_instance_class

  allocated_storage     = var.database_storage_gib
  max_allocated_storage = var.database_max_storage_gib
  storage_type          = "gp3"
  storage_encrypted     = true

  db_name                     = "komrad"
  username                    = "komrad"
  manage_master_user_password = true

  db_subnet_group_name   = aws_db_subnet_group.komrad.name
  vpc_security_group_ids = [aws_security_group.database.id]
  availability_zone      = var.availability_zone
  multi_az               = false
  publicly_accessible    = false

  backup_retention_period = var.database_backup_retention_days
  copy_tags_to_snapshot   = true
  deletion_protection     = var.database_deletion_protection
  # A timestamp here churns on every plan, and silencing that with ignore_changes leaves the
  # name unset on an instance Terraform imported rather than created, which makes the stack
  # impossible to destroy. The name is therefore fixed: a second teardown asks for a new one
  # rather than quietly skipping the snapshot.
  skip_final_snapshot       = var.database_final_snapshot == ""
  final_snapshot_identifier = var.database_final_snapshot == "" ? null : var.database_final_snapshot

  auto_minor_version_upgrade = true
  apply_immediately          = true

  tags = merge(local.tags, { Name = "${var.name}-database" })
}

