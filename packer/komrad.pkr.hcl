packer {
  required_plugins {
    amazon = {
      source  = "github.com/hashicorp/amazon"
      version = "~> 1.3"
    }
  }
}

variable "aws_region" {
  type    = string
  default = "us-east-1"
}

variable "aws_profile" {
  type    = string
  default = null
}

variable "build_instance_type" {
  type    = string
  default = "t4g.large"
}

variable "komrad_binary" {
  type    = string
  default = "../dist/komrad-linux-arm64"
}

variable "komrad_revision" {
  type    = string
  default = "unknown"
}

variable "komrad_version" {
  type    = string
  default = "unknown"
}

locals {
  name = "komrad-${formatdate("YYYYMMDD-hhmmss", timestamp())}"
}

source "amazon-ebs" "komrad" {
  region        = var.aws_region
  profile       = var.aws_profile
  instance_type = var.build_instance_type
  ssh_username  = "ubuntu"
  ami_name      = local.name

  source_ami_filter {
    filters = {
      name                = "ubuntu/images/hvm-ssd-gp3/ubuntu-noble-24.04-arm64-server-*"
      root-device-type    = "ebs"
      virtualization-type = "hvm"
    }
    owners      = ["099720109477"]
    most_recent = true
  }

  launch_block_device_mappings {
    device_name           = "/dev/sda1"
    volume_size           = 30
    volume_type           = "gp3"
    encrypted             = true
    delete_on_termination = true
  }

  imds_support = "v2.0"

  tags = {
    Name           = local.name
    Project        = "komrad"
    ManagedBy      = "packer"
    KomradRevision = var.komrad_revision
    KomradVersion  = var.komrad_version
    BaseAmi        = "{{ .SourceAMI }}"
    BaseAmiName    = "{{ .SourceAMIName }}"
  }

  snapshot_tags = {
    Name      = local.name
    Project   = "komrad"
    ManagedBy = "packer"
  }
}

build {
  name    = "komrad"
  sources = ["source.amazon-ebs.komrad"]

  provisioner "shell" {
    inline = ["mkdir -p /tmp/files"]
  }

  provisioner "file" {
    source      = "files/"
    destination = "/tmp/files/"
  }

  provisioner "file" {
    source      = var.komrad_binary
    destination = "/tmp/komrad"
  }

  provisioner "shell" {
    execute_command = "sudo -E bash '{{ .Path }}'"
    scripts = [
      "scripts/00-base.sh",
      "scripts/10-docker.sh",
      "scripts/20-toolchain.sh",
      "scripts/30-harnesses.sh",
      "scripts/40-komrad.sh",
      "scripts/99-clean.sh",
    ]
  }
}
