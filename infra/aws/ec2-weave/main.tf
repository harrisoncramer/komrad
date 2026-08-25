provider "aws" {
  region = var.region
}

# The default VPC and one of its subnets are enough for a single instance;
# nothing here needs a dedicated network.
data "aws_vpc" "default" {
  default = true
}

data "aws_subnets" "default" {
  filter {
    name   = "vpc-id"
    values = [data.aws_vpc.default.id]
  }
}

data "aws_ami" "ubuntu" {
  most_recent = true
  owners      = ["099720109477"] # Canonical

  filter {
    name   = "name"
    values = ["ubuntu/images/hvm-ssd*/ubuntu-noble-24.04-${var.architecture}-server-*"]
  }

  filter {
    name   = "virtualization-type"
    values = ["hvm"]
  }
}

# The only way in: AWS Session Manager, IAM-authenticated, no inbound port
# and no SSH key anywhere. The Ubuntu AMI ships the SSM Agent by default;
# user_data.sh.tpl confirms it's running rather than assuming so.
resource "aws_iam_role" "ssm" {
  name = "${var.name}-ssm"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "ec2.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}

resource "aws_iam_role_policy_attachment" "ssm" {
  role       = aws_iam_role.ssm.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"
}

resource "aws_iam_instance_profile" "ssm" {
  name = "${var.name}-ssm"
  role = aws_iam_role.ssm.name
}

# Session Manager's "Run As" support, so `aws ssm start-session` lands
# directly as the ubuntu user through the default, best-tested session
# document - not the ssm-user account, and not a special-purpose document
# like AWS-StartInteractiveCommand, which isn't built for a full-screen TUI's
# resize/termios needs the way the default interactive shell document is.
#
# shellProfile.linux is the documented mechanism for a session startup
# command (AWS: "Update Session Manager preferences (command line)") - not
# ubuntu's own /etc/passwd shell field, which a real connection test showed
# Session Manager's Run-As never actually consults (whoami correctly reports
# ubuntu, but $SHELL comes back empty and nothing set as ubuntu's login
# shell runs; the agent execs its own shell directly rather than looking one
# up). weave start backgrounds the service; weave open then attaches.
#
# This document is a single account+region-wide Session Manager preference,
# not scoped to this instance: destroying this module reverts Session
# Manager to its ssm-user default for the whole account, and applying it
# affects every managed node in the account/region, not just this one.
resource "aws_ssm_document" "run_as" {
  name            = "SSM-SessionManagerRunShell"
  document_type   = "Session"
  document_format = "JSON"

  content = jsonencode({
    schemaVersion = "1.0"
    description   = "Document to hold regional settings for Session Manager"
    sessionType   = "Standard_Stream"
    inputs = {
      runAsEnabled     = true
      runAsDefaultUser = "ubuntu"
      shellProfile = {
        linux = "weave start; weave open"
      }
    }
  })
}

resource "aws_security_group" "weave" {
  name        = var.name
  description = "weave EC2 instance: no ingress rule at all. SSM Session Manager reaches it over the egress rule below."
  vpc_id      = data.aws_vpc.default.id

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = {
    Name = var.name
  }
}

resource "aws_instance" "weave" {
  ami                    = data.aws_ami.ubuntu.id
  instance_type          = var.instance_type
  subnet_id              = data.aws_subnets.default.ids[0]
  vpc_security_group_ids = [aws_security_group.weave.id]
  iam_instance_profile   = aws_iam_instance_profile.ssm.name

  # The default VPC's route table sends 0.0.0.0/0 to an Internet Gateway, not
  # a NAT Gateway, so a private-IP-only instance has no outbound path at all -
  # the IGW's NAT depends on the instance owning a public IP. Needed for the
  # SSM Agent to phone home, not for anyone to reach it directly.
  associate_public_ip_address = true

  root_block_device {
    volume_type = "gp3"
    volume_size = var.root_volume_size_gb
  }

  # Plain text in, base64-encoded by the provider on the way to EC2.
  user_data = templatefile("${path.module}/user_data.sh.tpl", {
    architecture = var.architecture
  })

  tags = {
    Name = var.name
  }
}
