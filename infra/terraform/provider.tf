provider "aws" {
  region                   = var.aws_region
  profile                  = var.aws_profile
  shared_config_files      = var.aws_config_file == null ? null : [var.aws_config_file]
  shared_credentials_files = var.aws_shared_credentials_file == null ? null : [var.aws_shared_credentials_file]
}
