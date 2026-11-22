package reorder

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/xtylo/predictive-inventory/internal/cache"
)

type fakeStock struct {
	stock int
	err   error
}

func (f fakeStock) GetStock(context.Context, string) (int, error) { return f.stock, f.err }

// fakePublisher records published messages.
type fakePublisher struct {
	published []Message
	err       error
}

func (f *fakePublisher) Publish(_ context.Context, m Message) error {
	if f.err != nil {
		return f.err
	}
	f.published = append(f.published, m)
	return nil
}

func TestEvaluate(t *testing.T) {
	forecast := cache.Forecast{
		SKU:         "SKU-1",
		Demand:      100, // target = 150
		HorizonDays: 30,
		GeneratedAt: time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC),
	}

	tests := []struct {
		name          string
		stock         fakeStock
		publishErr    error
		wantPublished bool
		wantQuantity  int
		wantErr       bool
	}{
		{name: "below target publishes", stock: fakeStock{stock: 120}, wantPublished: true, wantQuantity: 30},
		{name: "at target publishes nothing", stock: fakeStock{stock: 150}},
		{name: "stock error", stock: fakeStock{err: errors.New("db down")}, wantErr: true},
		{name: "publish error", stock: fakeStock{stock: 0}, publishErr: errors.New("sqs down"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pub := &fakePublisher{err: tt.publishErr}
			published, err := NewEvaluator(tt.stock, pub).Evaluate(t.Context(), forecast)

			if (err != nil) != tt.wantErr {
				t.Fatalf("Evaluate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if published != tt.wantPublished {
				t.Errorf("published = %v, want %v", published, tt.wantPublished)
			}
			if !tt.wantPublished {
				return
			}
			if len(pub.published) != 1 {
				t.Fatalf("messages = %d, want 1", len(pub.published))
			}
			if got := pub.published[0]; got.Quantity != tt.wantQuantity || got.SKU != "SKU-1" {
				t.Errorf("message = %+v, want SKU-1 with quantity %d", got, tt.wantQuantity)
			}
		})
	}
}
