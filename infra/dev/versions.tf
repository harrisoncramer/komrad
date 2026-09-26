terraform {
  required_version = "~> 1.16.0"

  backend "s3" {}

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "6.64.0"
    }

    cloudflare = {
      source  = "cloudflare/cloudflare"
      version = "5.25.0"
    }
  }
}
