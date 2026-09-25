resource "aws_vpc" "komrade" {
  cidr_block           = var.vpc_cidr
  enable_dns_hostnames = true
  enable_dns_support   = true
  tags                 = merge(local.tags, { Name = "${var.name}-vpc" })
}

resource "aws_internet_gateway" "komrade" {
  vpc_id = aws_vpc.komrade.id
  tags   = merge(local.tags, { Name = "${var.name}-internet-gateway" })
}

resource "aws_subnet" "komrade" {
  vpc_id                  = aws_vpc.komrade.id
  cidr_block              = var.public_subnet_cidr
  availability_zone       = var.availability_zone
  map_public_ip_on_launch = true
  tags                    = merge(local.tags, { Name = "${var.name}-public-subnet" })
}

resource "aws_route_table" "komrade" {
  vpc_id = aws_vpc.komrade.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.komrade.id
  }

  tags = merge(local.tags, { Name = "${var.name}-public-routes" })
}

resource "aws_route_table_association" "komrade" {
  subnet_id      = aws_subnet.komrade.id
  route_table_id = aws_route_table.komrade.id
}

resource "aws_security_group" "komrade" {
  name_prefix = "${var.name}-instance-"
  vpc_id      = aws_vpc.komrade.id

  dynamic "ingress" {
    for_each = length(var.ssh_allowed_cidrs) > 0 ? [1] : []

    content {
      cidr_blocks = var.ssh_allowed_cidrs
      from_port   = 22
      protocol    = "tcp"
      to_port     = 22
    }
  }

  egress {
    cidr_blocks = ["0.0.0.0/0"]
    from_port   = 0
    protocol    = "-1"
    to_port     = 0
  }

  tags = merge(local.tags, { Name = "${var.name}-instance" })

  lifecycle {
    create_before_destroy = true
  }
}
