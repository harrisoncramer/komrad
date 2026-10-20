terraform {
  required_version = "~> 1.16.0"

  backend "s3" {}

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "6.64.0"
    }

    tls = {
      source  = "hashicorp/tls"
      version = "4.4.1"
    }
  }
}
