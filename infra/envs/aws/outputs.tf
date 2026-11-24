output "reorder_queue_url" {
  description = "SQS_REORDER_QUEUE_URL for the service."
  value       = module.reorder_queue.queue_url
}

output "db_address" {
  description = "RDS endpoint hostname."
  value       = aws_db_instance.inventory.address
}

# sensitive hides the value in plan and apply output. Read it on purpose
# with: terraform output -raw postgres_dsn
output "postgres_dsn" {
  description = "POSTGRES_DSN for the service. RDS for PostgreSQL requires TLS."
  sensitive   = true
  value = format(
    "postgres://%s:%s@%s:%d/%s?sslmode=require",
    aws_db_instance.inventory.username,
    random_password.db.result,
    aws_db_instance.inventory.address,
    aws_db_instance.inventory.port,
    aws_db_instance.inventory.db_name,
  )
}
