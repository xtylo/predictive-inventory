package worker

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"github.com/xtylo/predictive-inventory/internal/cache"
	"github.com/xtylo/predictive-inventory/internal/predictor"
)

// countingPredictor records how many times it is called. It is shared by
// several worker goroutines, so the counter must be atomic.
type countingPredictor struct {
	calls atomic.Int32
	err   error
}

func (c *countingPredictor) Predict(context.Context, string) (predictor.Forecast, error) {
	c.calls.Add(1)
	if c.err != nil {
		return predictor.Forecast{}, c.err
	}
	return predictor.Forecast{Demand: 10, HorizonDays: 30}, nil
}

// newTestCache gives the pool a real lock and forecast store backed by an
// in-memory Redis.
func newTestCache(t *testing.T) *cache.Cache {
	t.Helper()
	server := miniredis.RunT(t)
	c, err := cache.New(t.Context(), server.Addr(), time.Hour)
	if err != nil {
		t.Fatalf("cache.New() error: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func testConfig(workers, queueSize int) Config {
	return Config{
		Workers:     workers,
		QueueSize:   queueSize,
		DedupWindow: time.Minute,
		JobTimeout:  time.Second,
	}
}

func shutdown(t *testing.T, p *Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error: %v", err)
	}
}

// A burst of 20 triggers for one SKU, handled by 4 workers in parallel,
// must reach the model exactly once.
func TestOnePredictionPerSKUWithinWindow(t *testing.T) {
	c := newTestCache(t)
	pred := &countingPredictor{}
	pool := New(testConfig(4, 100), pred, c, c)
	pool.Start()

	for range 20 {
		if err := pool.Enqueue("SKU-1"); err != nil {
			t.Fatalf("Enqueue() error: %v", err)
		}
	}
	shutdown(t, pool) // drains the queue before returning

	if got := pred.calls.Load(); got != 1 {
		t.Errorf("predictor calls = %d, want 1", got)
	}

	forecast, err := c.GetForecast(t.Context(), "SKU-1")
	if err != nil {
		t.Fatalf("GetForecast() error: %v", err)
	}
	if forecast.Demand != 10 {
		t.Errorf("stored demand = %d, want 10", forecast.Demand)
	}
}

// waitFor polls cond until it is true or a second passes. Polling with a
// deadline beats a fixed sleep: fast when the condition holds early, and
// still bounded when it never does.
func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// When a prediction fails the lock is released, so the next trigger
// retries instead of waiting for the whole dedup window.
func TestFailedPredictionReleasesLock(t *testing.T) {
	c := newTestCache(t)
	pred := &countingPredictor{err: errors.New("model unavailable")}
	pool := New(testConfig(1, 10), pred, c, c)
	pool.Start()

	// One trigger at a time: each must reach the model, which proves the
	// failed attempt before it did not leave the lock behind.
	for want := int32(1); want <= 3; want++ {
		if err := pool.Enqueue("SKU-1"); err != nil {
			t.Fatalf("Enqueue() error: %v", err)
		}
		waitFor(t, func() bool { return pred.calls.Load() == want }, "a prediction attempt")
	}
	shutdown(t, pool)
}

// A SKU already waiting in the queue is not queued again.
func TestEnqueueDeduplicatesPendingSKU(t *testing.T) {
	// No workers: nothing leaves the queue, so its length is observable.
	pool := New(testConfig(0, 10), &countingPredictor{}, nil, nil)

	for range 5 {
		if err := pool.Enqueue("SKU-1"); err != nil {
			t.Fatalf("Enqueue(SKU-1) error: %v", err)
		}
	}
	if err := pool.Enqueue("SKU-2"); err != nil {
		t.Fatalf("Enqueue(SKU-2) error: %v", err)
	}

	if got := len(pool.queue); got != 2 {
		t.Errorf("queued jobs = %d, want 2 (one per distinct SKU)", got)
	}
}

// With no workers running nothing is consumed, so the queue fills up.
func TestEnqueueFailsWhenQueueIsFull(t *testing.T) {
	pool := New(testConfig(0, 2), &countingPredictor{}, nil, nil)

	for _, sku := range []string{"SKU-1", "SKU-2"} {
		if err := pool.Enqueue(sku); err != nil {
			t.Fatalf("Enqueue(%s) error: %v", sku, err)
		}
	}
	if err := pool.Enqueue("SKU-3"); !errors.Is(err, ErrQueueFull) {
		t.Errorf("third Enqueue() = %v, want ErrQueueFull", err)
	}
}

func TestEnqueueFailsAfterShutdown(t *testing.T) {
	pool := New(testConfig(1, 10), &countingPredictor{}, nil, nil)
	pool.Start()
	shutdown(t, pool)

	if err := pool.Enqueue("SKU-1"); !errors.Is(err, ErrClosed) {
		t.Errorf("Enqueue() after Shutdown = %v, want ErrClosed", err)
	}
}

// A prediction that would take an hour must not hold shutdown hostage:
// once the deadline passes, in-flight work is cancelled.
func TestShutdownCancelsSlowWorkAtDeadline(t *testing.T) {
	c := newTestCache(t)
	slow := predictor.Fake{Demand: 1, HorizonDays: 30, Delay: time.Hour}
	cfg := testConfig(1, 10)
	cfg.JobTimeout = 2 * time.Hour
	pool := New(cfg, slow, c, c)
	pool.Start()

	if err := pool.Enqueue("SKU-1"); err != nil {
		t.Fatalf("Enqueue() error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := pool.Shutdown(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Shutdown() = %v, want context.DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("Shutdown() took %v, want about 100ms", elapsed)
	}
}

func TestOnForecastRunsAfterStoring(t *testing.T) {
	c := newTestCache(t)
	cfg := testConfig(1, 10)

	var got []cache.Forecast
	cfg.OnForecast = func(_ context.Context, f cache.Forecast) error {
		// Safe without a mutex: the pool has a single worker.
		got = append(got, f)
		return nil
	}
	pool := New(cfg, &countingPredictor{}, c, c)
	pool.Start()

	if err := pool.Enqueue("SKU-1"); err != nil {
		t.Fatalf("Enqueue() error: %v", err)
	}
	shutdown(t, pool)

	if len(got) != 1 || got[0].SKU != "SKU-1" || got[0].Demand != 10 {
		t.Errorf("OnForecast calls = %+v, want one call for SKU-1 with demand 10", got)
	}
}
