package reorder

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

// SQSPublisher publishes reorder messages to an SQS FIFO queue.
type SQSPublisher struct {
	client   *sqs.Client
	queueURL string
}

// NewSQSPublisher loads the AWS configuration the standard way: credentials,
// region and endpoint come from the environment (AWS_ACCESS_KEY_ID,
// AWS_REGION, AWS_ENDPOINT_URL...), a profile, or an IAM role. The code does
// not change between LocalStack and real AWS, only the environment does.
func NewSQSPublisher(ctx context.Context, queueURL string) (*SQSPublisher, error) {
	// awsconfig is an import alias: the package is also called "config",
	// which would clash with this project's own config package.
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	return &SQSPublisher{client: sqs.NewFromConfig(cfg), queueURL: queueURL}, nil
}

// Publish sends m to the queue.
func (p *SQSPublisher) Publish(ctx context.Context, m Message) error {
	body, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("encode reorder: %w", err)
	}

	_, err = p.client.SendMessage(ctx, &sqs.SendMessageInput{
		// The SDK takes pointers for optional fields; aws.String converts.
		QueueUrl:    aws.String(p.queueURL),
		MessageBody: aws.String(string(body)),
		// Messages of one group are delivered in order: one group per SKU
		// keeps each SKU's reorders ordered while SKUs flow in parallel.
		MessageGroupId: aws.String(m.SKU),
		// One reorder per forecast: if publishing is retried within SQS's
		// 5-minute deduplication window, the queue delivers it once.
		MessageDeduplicationId: aws.String(DeduplicationID(m)),
	})
	if err != nil {
		return fmt.Errorf("send message: %w", err)
	}
	return nil
}

// DeduplicationID identifies the forecast a reorder comes from.
func DeduplicationID(m Message) string {
	return fmt.Sprintf("%s-%d", m.SKU, m.ForecastGeneratedAt.UnixNano())
}
