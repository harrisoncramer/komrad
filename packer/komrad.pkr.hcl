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
  stamp = formatdate("YYYYMMDD-hhmmss", timestamp())

  tags = {
    Project        = "komrad"
    ManagedBy      = "packer"
    KomradRevision = var.komrad_revision
    KomradVersion  = var.komrad_version
    BaseAmi        = "{{ .SourceAMI }}"
    BaseAmiName    = "{{ .SourceAMIName }}"
  }

  snapshot_tags = {
    Project   = "komrad"
    ManagedBy = "packer"
  }
}

source "amazon-ebs" "komrad" {
  region        = var.aws_region
  profile       = var.aws_profile
  instance_type = var.build_instance_type
  ssh_username  = "ubuntu"

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
}

build {
  name = "komrad"

  source "amazon-ebs.komrad" {
    name          = "control-plane"
    ami_name      = "komrad-control-plane-${local.stamp}"
    tags          = merge(local.tags, { Name = "komrad-control-plane-${local.stamp}", KomradImage = "control-plane" })
    snapshot_tags = merge(local.snapshot_tags, { Name = "komrad-control-plane-${local.stamp}" })
  }

  source "amazon-ebs.komrad" {
    name          = "engine"
    ami_name      = "komrad-engine-${local.stamp}"
    tags          = merge(local.tags, { Name = "komrad-engine-${local.stamp}", KomradImage = "engine" })
    snapshot_tags = merge(local.snapshot_tags, { Name = "komrad-engine-${local.stamp}" })
  }

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
    only            = ["amazon-ebs.control-plane"]
    execute_command = "sudo -E bash '{{ .Path }}'"
    scripts = [
      "scripts/00-base.sh",
      "scripts/40-komrad.sh",
      "scripts/50-caddy.sh",
      "scripts/55-tailscale.sh",
      "scripts/60-control-plane.sh",
      "scripts/99-clean.sh",
    ]
  }

  provisioner "shell" {
    only            = ["amazon-ebs.engine"]
    execute_command = "sudo -E bash '{{ .Path }}'"
    scripts = [
      "scripts/00-base.sh",
      "scripts/10-docker.sh",
      "scripts/20-toolchain.sh",
      "scripts/30-harnesses.sh",
      "scripts/40-komrad.sh",
      "scripts/60-engine.sh",
      "scripts/99-clean.sh",
    ]
  }

  post-processor "manifest" {
    output      = "../dist/packer-manifest.json"
    strip_path  = true
    custom_data = { image = source.name }
  }
}
