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
  default = "komrade-docs"
}

variable "domain_name" {
  type    = string
  default = "komrad.dev"
}

variable "redirect_domain_names" {
  type    = list(string)
  default = ["komrad.ai", "komrade.app", "komrade.dev"]
}

variable "bucket_name" {
  type    = string
  default = "komrade.dev"
}

variable "tags" {
  type = map(string)
  default = {
    ManagedBy = "terraform"
    Project   = "komrade"
  }
}

variable "state_bucket" {
  type    = string
  default = ""

  validation {
    condition     = var.github_repository == "" || var.state_bucket != ""
    error_message = "The CI role reads this stack's state, so the bucket holding it must be named."
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

variable "github_owner_id" {
  type    = string
  default = ""
}

variable "github_repository_id" {
  type    = string
  default = ""
}
