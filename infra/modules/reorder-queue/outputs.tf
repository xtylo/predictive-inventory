output "queue_url" {
  description = "URL the service publishes to (SQS_REORDER_QUEUE_URL)."
  value       = aws_sqs_queue.this.url
}

output "queue_arn" {
  description = "ARN, for IAM policies."
  value       = aws_sqs_queue.this.arn
}

output "dead_letter_queue_url" {
  description = "URL of the dead-letter queue."
  value       = aws_sqs_queue.dead_letter.url
}
