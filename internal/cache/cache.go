// Package cache stores demand forecasts in Redis for low-latency reads.
package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// ErrNotFound means no forecast is cached for the SKU: it was never
// computed or its TTL expired.
var ErrNotFound = errors.New("forecast not found")

// Forecast is the expected demand for a SKU over a time horizon.
//
// It is stored as JSON instead of a bare integer so readers can tell how
// fresh it is (GeneratedAt) and what period it covers (HorizonDays).
type Forecast struct {
	SKU         string    `json:"sku"`
	Demand      int       `json:"demand"`
	HorizonDays int       `json:"horizon_days"`
	GeneratedAt time.Time `json:"generated_at"`
}

// Cache wraps a Redis client. Like pgxpool, a redis.Client manages its own
// connection pool and is safe for concurrent use.
type Cache struct {
	rdb *redis.Client
	ttl time.Duration
}

// New connects to Redis and pings it so startup fails fast.
// Every forecast written through this Cache expires after ttl.
func New(ctx context.Context, addr string, ttl time.Duration) (*Cache, error) {
	rdb := redis.NewClient(&redis.Options{Addr: addr})

	if err := rdb.Ping(ctx).Err(); err != nil {
		_ = rdb.Close()
		return nil, fmt.Errorf("ping redis: %w", err)
	}

	return &Cache{rdb: rdb, ttl: ttl}, nil
}

// Close releases the client's connections.
func (c *Cache) Close() error {
	return c.rdb.Close()
}

// forecastKey namespaces keys so other data can share the same Redis.
func forecastKey(sku string) string {
	return "forecast:" + sku
}

// SetForecast writes (or overwrites) the forecast of f.SKU with the TTL.
func (c *Cache) SetForecast(ctx context.Context, f Forecast) error {
	payload, err := json.Marshal(f)
	if err != nil {
		return fmt.Errorf("encode forecast: %w", err)
	}

	if err := c.rdb.Set(ctx, forecastKey(f.SKU), payload, c.ttl).Err(); err != nil {
		return fmt.Errorf("write forecast: %w", err)
	}
	return nil
}

// GetForecast returns the cached forecast of sku, or ErrNotFound.
func (c *Cache) GetForecast(ctx context.Context, sku string) (Forecast, error) {
	payload, err := c.rdb.Get(ctx, forecastKey(sku)).Bytes()
	// go-redis signals a missing key with the sentinel redis.Nil.
	// Translating it keeps Redis details out of the callers.
	if errors.Is(err, redis.Nil) {
		return Forecast{}, ErrNotFound
	}
	if err != nil {
		return Forecast{}, fmt.Errorf("read forecast: %w", err)
	}

	var f Forecast
	if err := json.Unmarshal(payload, &f); err != nil {
		return Forecast{}, fmt.Errorf("decode forecast: %w", err)
	}
	return f, nil
}
