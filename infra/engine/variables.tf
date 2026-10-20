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
  default = "komrad-development"
}

variable "control_plane_ami_id" {
  type = string
}

variable "engine_ami_id" {
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

variable "control_plane_instance_type" {
  type    = string
  default = "t4g.small"
}

variable "engines" {
  type = map(object({
    email         = string
    instance_type = optional(string, "t4g.xlarge")
  }))
  default = {}

  validation {
    condition     = alltrue([for name in keys(var.engines) : can(regex("^[a-z][a-z0-9_]{0,29}$", name))])
    error_message = "An engine's name starts with a lowercase letter and has at most 30 lowercase letters, digits and underscores."
  }

  validation {
    condition     = alltrue([for engine in values(var.engines) : can(regex("^[^@\\s]+@[^@\\s]+$", engine.email))])
    error_message = "Each engine's email is the bare address its person signs in with."
  }
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
    Project   = "komrad"
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
  default = "komrad"
}

variable "database_final_snapshot" {
  type    = string
  default = "komrad-development-final"
}

variable "tailscale_hostname" {
  type    = string
  default = "komrad"

  validation {
    condition     = can(regex("^[a-z0-9]([a-z0-9-]*[a-z0-9])?$", var.tailscale_hostname))
    error_message = "The engine's name on the tailnet is one lowercase label, like komrad."
  }
}

variable "tailscale_tailnet" {
  type = string

  validation {
    condition     = can(regex("^[a-z0-9-]+\\.ts\\.net$", var.tailscale_tailnet))
    error_message = "The tailnet is its DNS name from the admin console, like tail1234.ts.net."
  }
}

variable "tailscale_auth_key" {
  type      = string
  sensitive = true

  validation {
    condition     = startswith(var.tailscale_auth_key, "tskey-auth-")
    error_message = "The auth key is the tskey-auth- value from the admin console's keys page."
  }
}

variable "tailscale_direct_connections" {
  type    = bool
  default = true
}

variable "sign_in_issuer" {
  type = string

  validation {
    condition     = startswith(var.sign_in_issuer, "https://")
    error_message = "The issuer is the provider's https address, like https://accounts.google.com."
  }
}

variable "sign_in_client_id" {
  type = string
}

variable "sign_in_client_secret" {
  type      = string
  default   = ""
  sensitive = true
}

variable "github_oauth_client_id" {
  type = string
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
