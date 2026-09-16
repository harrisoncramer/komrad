variable "aws_region" {
  type    = string
  default = "us-east-1"
}

variable "aws_account_id" {
  type = string
}

variable "aws_profile" {
  type    = string
  default = null
}

variable "aws_config_file" {
  type    = string
  default = null
}

variable "aws_shared_credentials_file" {
  type    = string
  default = null
}

variable "name" {
  type    = string
  default = "weave-development"
}

variable "ami_id" {
  type = string
}

variable "availability_zone" {
  type = string

  validation {
    condition     = contains(keys(var.private_subnet_cidrs), var.availability_zone)
    error_message = "availability_zone must be one of the zones private_subnet_cidrs names, since the database sits in that zone and its subnet group has to cover it."
  }
}

variable "vpc_cidr" {
  type    = string
  default = "10.42.0.0/16"
}

variable "public_subnet_cidr" {
  type    = string
  default = "10.42.0.0/24"
}

variable "instance_type" {
  type    = string
  default = "t4g.xlarge"
}

variable "cpu_credits" {
  type    = string
  default = "unlimited"
}

variable "root_volume_size_gib" {
  type    = number
  default = 100
}

variable "data_volume_size_gib" {
  type    = number
  default = 100
}



variable "tags" {
  type = map(string)
  default = {
    ManagedBy = "terraform"
    Project   = "weave"
  }
}

variable "private_subnet_cidrs" {
  type = map(string)
  default = {
    "us-east-1a" = "10.42.1.0/24"
    "us-east-1b" = "10.42.2.0/24"
  }

  validation {
    # The database pins itself to availability_zone and can only sit in a subnet its group
    # covers, so changing one without the other fails deep inside the RDS create rather than here.
    condition     = length(var.private_subnet_cidrs) > 1
    error_message = "A database subnet group needs subnets in at least two availability zones."
  }
}

variable "database_engine_version" {
  type    = string
  default = "18.6"
}

variable "database_instance_class" {
  type    = string
  default = "db.t4g.micro"
}

variable "database_storage_gib" {
  type    = number
  default = 20
}

variable "database_max_storage_gib" {
  type    = number
  default = 100
}

variable "database_backup_retention_days" {
  type    = number
  default = 7
}

variable "database_deletion_protection" {
  type    = bool
  default = true
}

variable "secret_prefix" {
  type    = string
  default = "weave"
}

variable "database_final_snapshot" {
  type    = string
  default = "weave-development-final"
}

variable "ssh_public_key" {
  type    = string
  default = ""
}

variable "ssh_allowed_cidrs" {
  type    = list(string)
  default = []

  validation {
    condition     = !contains(var.ssh_allowed_cidrs, "0.0.0.0/0")
    error_message = "Reaching the engine is gated by the source address, so the whole internet is refused here."
  }
}

variable "github_repository" {
  type    = string
  default = ""

  validation {
    condition     = var.github_repository == "" || can(regex("^[^/]+/[^/]+$", var.github_repository))
    error_message = "A repository is owner/name, since it is matched against the token GitHub signs."
  }
}

variable "state_bucket" {
  type    = string
  default = ""
}

variable "github_owner_id" {
  type    = string
  default = ""
}

variable "github_repository_id" {
  type    = string
  default = ""
}
