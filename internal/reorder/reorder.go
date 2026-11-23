// Package reorder decides when a SKU needs replenishment and publishes a
// reorder message for the downstream fulfillment system.
package reorder

import (
	"context"
	"fmt"
	"time"

	"github.com/xtylo/predictive-inventory/internal/cache"
	"github.com/xtylo/predictive-inventory/internal/planning"
)

// Message is the contract with consumers of the reorder queue. Fields are
// only ever added, never renamed, so existing consumers keep working.
type Message struct {
	SKU                 string    `json:"sku"`
	Quantity            int       `json:"quantity"`
	Stock               int       `json:"stock"`
	ForecastDemand      int       `json:"forecast_demand"`
	HorizonDays         int       `json:"horizon_days"`
	ForecastGeneratedAt time.Time `json:"forecast_generated_at"`
}

// Publisher sends reorder messages (implemented by *SQSPublisher).
type Publisher interface {
	Publish(ctx context.Context, m Message) error
}

// StockReader reads current stock (implemented by *store.Store).
type StockReader interface {
	GetStock(ctx context.Context, sku string) (int, error)
}

// Evaluator compares a fresh forecast with current stock and publishes a
// reorder when the stock falls short of the target.
type Evaluator struct {
	stock     StockReader
	publisher Publisher
}

// NewEvaluator builds an Evaluator.
func NewEvaluator(stock StockReader, publisher Publisher) *Evaluator {
	return &Evaluator{stock: stock, publisher: publisher}
}

// Evaluate publishes a reorder for f.SKU if one is needed. It returns
// whether a message was published.
func (e *Evaluator) Evaluate(ctx context.Context, f cache.Forecast) (bool, error) {
	stock, err := e.stock.GetStock(ctx, f.SKU)
	if err != nil {
		return false, fmt.Errorf("read stock: %w", err)
	}

	quantity := planning.ReorderQuantity(stock, f.Demand)
	if quantity == 0 {
		return false, nil
	}

	err = e.publisher.Publish(ctx, Message{
		SKU:                 f.SKU,
		Quantity:            quantity,
		Stock:               stock,
		ForecastDemand:      f.Demand,
		HorizonDays:         f.HorizonDays,
		ForecastGeneratedAt: f.GeneratedAt,
	})
	if err != nil {
		return false, fmt.Errorf("publish reorder: %w", err)
	}
	reordersPublished.Inc()
	return true, nil
}
