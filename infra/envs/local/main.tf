# Local environment: the same module that will run on AWS, applied against
# LocalStack. Only the provider configuration differs between environments.

terraform {
  required_version = ">= 1.6"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
  }
}

variable "localstack_endpoint" {
  description = "LocalStack edge endpoint, as seen from the dev container."
  type        = string
  default     = "http://localstack:4566"
}

provider "aws" {
  region = "us-east-1"

  # LocalStack accepts any credentials; these keep the provider from looking
  # for a real AWS profile.
  access_key                  = "test"
  secret_key                  = "test"
  skip_credentials_validation = true
  skip_requesting_account_id  = true
  skip_metadata_api_check     = true

  endpoints {
    sqs = var.localstack_endpoint
    sts = var.localstack_endpoint
  }
}

module "reorder_queue" {
  source = "../../modules/reorder-queue"
  name   = "inventory-reorder"
}

output "reorder_queue_url" {
  value = module.reorder_queue.queue_url
}

output "dead_letter_queue_url" {
  value = module.reorder_queue.dead_letter_queue_url
}
