variable "name" {
  description = "Base name of the queue, without the .fifo suffix."
  type        = string
}

variable "visibility_timeout_seconds" {
  description = "Seconds a received message stays hidden from other consumers."
  type        = number
  default     = 30
}

variable "max_receive_count" {
  description = "Receives before a message is moved to the dead-letter queue."
  type        = number
  default     = 5
}
