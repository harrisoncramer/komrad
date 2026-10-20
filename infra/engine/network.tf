resource "aws_vpc" "komrad" {
  cidr_block           = var.vpc_cidr
  enable_dns_hostnames = true
  enable_dns_support   = true
  tags                 = merge(local.tags, { Name = "${var.name}-vpc" })
}

resource "aws_internet_gateway" "komrad" {
  vpc_id = aws_vpc.komrad.id
  tags   = merge(local.tags, { Name = "${var.name}-internet-gateway" })
}

resource "aws_subnet" "komrad" {
  vpc_id                  = aws_vpc.komrad.id
  cidr_block              = var.public_subnet_cidr
  availability_zone       = var.availability_zone
  map_public_ip_on_launch = true
  tags                    = merge(local.tags, { Name = "${var.name}-public-subnet" })
}

resource "aws_route_table" "komrad" {
  vpc_id = aws_vpc.komrad.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.komrad.id
  }

  tags = merge(local.tags, { Name = "${var.name}-public-routes" })
}

resource "aws_route_table_association" "komrad" {
  subnet_id      = aws_subnet.komrad.id
  route_table_id = aws_route_table.komrad.id
}

resource "aws_security_group" "control_plane" {
  name_prefix = "${var.name}-control-plane-"
  vpc_id      = aws_vpc.komrad.id
  tags        = merge(local.tags, { Name = "${var.name}-control-plane" })

  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_vpc_security_group_ingress_rule" "control_plane_tailscale" {
  count = var.tailscale_direct_connections ? 1 : 0

  security_group_id = aws_security_group.control_plane.id
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "udp"
  from_port         = 41641
  to_port           = 41641
  tags              = local.tags
}

resource "aws_vpc_security_group_ingress_rule" "control_plane_forge_proxy" {
  security_group_id            = aws_security_group.control_plane.id
  referenced_security_group_id = aws_security_group.engine.id
  ip_protocol                  = "tcp"
  from_port                    = 8789
  to_port                      = 8789
  tags                         = local.tags
}

resource "aws_vpc_security_group_egress_rule" "control_plane" {
  security_group_id = aws_security_group.control_plane.id
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
  tags              = local.tags
}

resource "aws_security_group" "engine" {
  name_prefix = "${var.name}-engine-"
  vpc_id      = aws_vpc.komrad.id
  tags        = merge(local.tags, { Name = "${var.name}-engine" })

  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_vpc_security_group_ingress_rule" "engine_server" {
  security_group_id            = aws_security_group.engine.id
  referenced_security_group_id = aws_security_group.control_plane.id
  ip_protocol                  = "tcp"
  from_port                    = 8788
  to_port                      = 8788
  tags                         = local.tags
}

resource "aws_vpc_security_group_egress_rule" "engine" {
  security_group_id = aws_security_group.engine.id
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
  tags              = local.tags
}
