resource "aws_vpc" "weave" {
  cidr_block           = var.vpc_cidr
  enable_dns_hostnames = true
  enable_dns_support   = true
  tags                 = merge(local.tags, { Name = "${var.name}-vpc" })
}

resource "aws_internet_gateway" "weave" {
  vpc_id = aws_vpc.weave.id
  tags   = merge(local.tags, { Name = "${var.name}-internet-gateway" })
}

resource "aws_subnet" "weave" {
  vpc_id                  = aws_vpc.weave.id
  cidr_block              = var.public_subnet_cidr
  availability_zone       = var.availability_zone
  map_public_ip_on_launch = true
  tags                    = merge(local.tags, { Name = "${var.name}-public-subnet" })
}

resource "aws_route_table" "weave" {
  vpc_id = aws_vpc.weave.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.weave.id
  }

  tags = merge(local.tags, { Name = "${var.name}-public-routes" })
}

resource "aws_route_table_association" "weave" {
  subnet_id      = aws_subnet.weave.id
  route_table_id = aws_route_table.weave.id
}

resource "aws_security_group" "weave" {
  name_prefix = "${var.name}-instance-"
  vpc_id      = aws_vpc.weave.id

  dynamic "ingress" {
    for_each = var.ssh_allowed_cidr == null ? [] : [var.ssh_allowed_cidr]

    content {
      cidr_blocks = [ingress.value]
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
