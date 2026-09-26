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
  default = "komrad-dev"
}

variable "zone_name" {
  type    = string
  default = "komrad.dev"
}

variable "domain_name" {
  type    = string
  default = "dev.komrad.dev"
}

variable "bucket_name" {
  type    = string
  default = "komrad-dev-assets"
}

variable "expiring_prefixes" {
  type    = map(number)
  default = { "recordings/dev/" = 30 }
}

variable "writer_role_names" {
  type    = list(string)
  default = []
}

variable "tags" {
  type = map(string)
  default = {
    ManagedBy = "terraform"
    Project   = "komrad"
  }
}
