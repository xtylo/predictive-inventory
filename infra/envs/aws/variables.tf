variable "region" {
  description = "AWS region for every resource."
  type        = string
  default     = "us-east-1"
}

variable "allowed_cidr" {
  description = "The only address allowed to reach PostgreSQL, e.g. \"203.0.113.7/32\"."
  type        = string

  validation {
    condition     = can(cidrhost(var.allowed_cidr, 0)) && var.allowed_cidr != "0.0.0.0/0"
    error_message = "Use your own public IP as a /32 CIDR, never 0.0.0.0/0."
  }
}

variable "db_instance_class" {
  description = "RDS instance class. db.t4g.micro is the smallest general-purpose one."
  type        = string
  default     = "db.t4g.micro"
}
