// Package predictor computes demand forecasts. The interface lets the
// service run with a fake today and with Claude in session 5, without the
// worker noticing the difference.
package predictor

import (
	"context"
	"time"
)

// Forecast is a predicted demand over a horizon.
type Forecast struct {
	Demand      int
	HorizonDays int
	// Rationale explains the forecast in a sentence; empty for the fake.
	Rationale string
}

// Predictor forecasts the demand of one SKU.
type Predictor interface {
	Predict(ctx context.Context, sku string) (Forecast, error)
}

// Fake returns a fixed forecast after Delay, imitating a slow remote model.
// It is used in development, in tests and for load testing without cost.
type Fake struct {
	Demand      int
	HorizonDays int
	Delay       time.Duration
}

// Predict waits for Delay or for ctx to be cancelled, whichever comes first.
func (f Fake) Predict(ctx context.Context, sku string) (Forecast, error) {
	// select blocks until one of its channels is ready. time.After delivers
	// a value once the delay passes; ctx.Done() is closed on cancellation.
	// This is how Go code stays responsive to timeouts and shutdowns.
	select {
	case <-time.After(f.Delay):
		return Forecast{Demand: f.Demand, HorizonDays: f.HorizonDays}, nil
	case <-ctx.Done():
		return Forecast{}, ctx.Err()
	}
}
