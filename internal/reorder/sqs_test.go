package reorder

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

// Integration test against the queue Terraform created in LocalStack.
// It runs only when SQS_REORDER_QUEUE_URL is set, for example:
//
//	export SQS_REORDER_QUEUE_URL=$(terraform -chdir=infra/envs/local output -raw reorder_queue_url)
func TestSQSPublisherRoundTrip(t *testing.T) {
	queueURL := os.Getenv("SQS_REORDER_QUEUE_URL")
	if testing.Short() || queueURL == "" {
		t.Skip("integration test: set SQS_REORDER_QUEUE_URL to run it")
	}

	pub, err := NewSQSPublisher(t.Context(), queueURL)
	if err != nil {
		t.Fatalf("NewSQSPublisher() error: %v", err)
	}

	// A unique SKU keeps this test independent of other messages in the queue.
	want := Message{
		SKU:                 fmt.Sprintf("SKU-TEST-%d", time.Now().UnixNano()),
		Quantity:            42,
		ForecastGeneratedAt: time.Now().UTC(),
	}

	// Publishing twice with the same deduplication ID must deliver once.
	for range 2 {
		if err := pub.Publish(t.Context(), want); err != nil {
			t.Fatalf("Publish() error: %v", err)
		}
	}

	var received []Message
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		out, err := pub.client.ReceiveMessage(t.Context(), &sqs.ReceiveMessageInput{
			QueueUrl:            aws.String(queueURL),
			MaxNumberOfMessages: 10,
			WaitTimeSeconds:     1,
		})
		if err != nil {
			t.Fatalf("ReceiveMessage() error: %v", err)
		}
		if len(out.Messages) == 0 {
			break
		}
		for _, msg := range out.Messages {
			var m Message
			if err := json.Unmarshal([]byte(aws.ToString(msg.Body)), &m); err == nil && m.SKU == want.SKU {
				received = append(received, m)
			}
			// Delete everything read, so the queue is left clean.
			_, _ = pub.client.DeleteMessage(t.Context(), &sqs.DeleteMessageInput{
				QueueUrl:      aws.String(queueURL),
				ReceiptHandle: msg.ReceiptHandle,
			})
		}
	}

	if len(received) != 1 {
		t.Fatalf("received %d messages for %s, want exactly 1", len(received), want.SKU)
	}
	if received[0].Quantity != want.Quantity {
		t.Errorf("quantity = %d, want %d", received[0].Quantity, want.Quantity)
	}
}
