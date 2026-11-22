# Reorder queue: the service publishes a message here whenever a SKU needs
# replenishment, and a downstream fulfillment system consumes it.
#
# FIFO guarantees two things a plain queue does not: messages of the same
# group (one SKU) are delivered in order, and a message sent twice with the
# same deduplication ID within 5 minutes is delivered once.

# Messages that fail processing maxReceiveCount times are moved here instead
# of blocking the queue or being lost. A FIFO queue needs a FIFO dead-letter
# queue.
resource "aws_sqs_queue" "dead_letter" {
  name                      = "${var.name}-dlq.fifo"
  fifo_queue                = true
  message_retention_seconds = 1209600 # 14 days, the maximum, to investigate failures
}

resource "aws_sqs_queue" "this" {
  name       = "${var.name}.fifo"
  fifo_queue = true

  # The service sets MessageDeduplicationId itself, so deduplication does
  # not depend on the message body being byte-for-byte identical.
  content_based_deduplication = false

  # How long a consumer has to process a message before it becomes visible
  # to other consumers again.
  visibility_timeout_seconds = var.visibility_timeout_seconds
  message_retention_seconds  = 345600 # 4 days

  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.dead_letter.arn
    maxReceiveCount     = var.max_receive_count
  })
}
