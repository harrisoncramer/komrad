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

variable "secret_recovery_days" {
  type    = number
  default = 7
}
