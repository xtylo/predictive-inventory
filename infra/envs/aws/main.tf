# AWS environment: the same reorder-queue module as infra/envs/local, plus
# an RDS PostgreSQL instance. Meant for a short smoke test: create it, test
# it, and run "terraform destroy" the same day.
#
# Cost-conscious choices: the smallest instance class, 20 GB of storage, no
# automated backups, and no NAT gateway or load balancer. RDS is reachable
# from the internet, but its security group only admits allowed_cidr.

terraform {
  required_version = ">= 1.6"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.6"
    }
  }
}

provider "aws" {
  region = var.region

  # Tags every resource, so everything this project created is easy to
  # find in the console and in the bill.
  default_tags {
    tags = {
      Project   = "predictive-inventory"
      ManagedBy = "terraform"
    }
  }
}

module "reorder_queue" {
  source = "../../modules/reorder-queue"
  name   = "inventory-reorder"
}

# The default VPC that every AWS account has in each region. Its subnets are
# public, which is what a publicly accessible RDS instance needs.
data "aws_vpc" "default" {
  default = true
}

resource "aws_security_group" "db" {
  name        = "predictive-inventory-db"
  description = "PostgreSQL access for the predictive-inventory smoke test"
  vpc_id      = data.aws_vpc.default.id
}

# Only the address in allowed_cidr (your public IP) can reach PostgreSQL.
resource "aws_vpc_security_group_ingress_rule" "postgres" {
  security_group_id = aws_security_group.db.id
  description       = "PostgreSQL from the developer machine"
  cidr_ipv4         = var.allowed_cidr
  ip_protocol       = "tcp"
  from_port         = 5432
  to_port           = 5432
}

# Generated password. It lives in the Terraform state, which is local and
# git-ignored; a long-lived environment would use AWS Secrets Manager.
# No special characters, so it can go into a connection URL unescaped.
resource "random_password" "db" {
  length  = 32
  special = false
}

resource "aws_db_instance" "inventory" {
  identifier     = "predictive-inventory"
  engine         = "postgres"
  engine_version = "17"
  instance_class = var.db_instance_class

  allocated_storage = 20
  storage_type      = "gp3"
  storage_encrypted = true

  db_name  = "inventory"
  username = "inventory"
  password = random_password.db.result

  publicly_accessible    = true
  vpc_security_group_ids = [aws_security_group.db.id]

  # Throwaway database: no automated backups, no final snapshot on destroy,
  # and changes applied right away instead of in the maintenance window.
  backup_retention_period = 0
  skip_final_snapshot     = true
  deletion_protection     = false
  apply_immediately       = true
}
