variable "region" {
  description = "AWS region to provision the instance in."
  type        = string
  default     = "us-east-1"
}

variable "name" {
  description = "Name given to the instance and its security group."
  type        = string
  default     = "weave"
}

variable "instance_type" {
  description = "EC2 instance type. Must match var.architecture: an amd64 type (e.g. t3.small) for \"amd64\", a Graviton type (e.g. t4g.small) for \"arm64\"."
  type        = string
  default     = "t3.small"
}

variable "architecture" {
  description = "CPU architecture of the instance: \"amd64\" or \"arm64\". Selects both the Ubuntu AMI and the weave release binary the instance downloads."
  type        = string
  default     = "amd64"

  validation {
    condition     = contains(["amd64", "arm64"], var.architecture)
    error_message = "architecture must be \"amd64\" or \"arm64\"."
  }
}

variable "root_volume_size_gb" {
  description = "Root EBS volume size, in GiB. Holds the OS, the weave binary, and the local Postgres data directory."
  type        = number
  default     = 30
}

