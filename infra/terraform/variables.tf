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
  default = "standard"
}

variable "root_volume_size_gib" {
  type    = number
  default = 50
}

variable "data_volume_size_gib" {
  type    = number
  default = 100
}

variable "ssh_allowed_cidr" {
  type    = string
  default = null
}

variable "ssh_public_key_path" {
  type    = string
  default = null
}

variable "tags" {
  type = map(string)
  default = {
    ManagedBy = "terraform"
    Project   = "weave"
  }
}
