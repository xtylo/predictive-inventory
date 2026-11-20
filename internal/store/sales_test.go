package store

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

// newTestStore starts a throwaway PostgreSQL container, applies the
// migrations and returns a Store connected to it. Everything is removed
// when the test finishes.
func newTestStore(t *testing.T) *Store {
	// t.Helper makes failures point at the caller's line, not this one.
	t.Helper()

	// "go test -short" skips anything that needs Docker.
	if testing.Short() {
		t.Skip("integration test: requires Docker")
	}

	// t.Context is cancelled automatically when the test ends.
	ctx := t.Context()

	container, err := postgres.Run(ctx, "postgres:17-alpine",
		postgres.WithDatabase("inventory"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		postgres.BasicWaitStrategies(),
	)
	// Registered before the error check so a half-started container is
	// still removed.
	testcontainers.CleanupContainer(t, container)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("get connection string: %v", err)
	}

	s, err := New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(s.Close)

	if _, err := s.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return s
}

// seedSKU inserts one inventory row. Tests in the same package can reach
// the unexported pool field directly.
func seedSKU(t *testing.T, s *Store, sku string, stock int) {
	t.Helper()
	_, err := s.pool.Exec(t.Context(),
		`INSERT INTO inventory (sku, stock) VALUES ($1, $2)`, sku, stock)
	if err != nil {
		t.Fatalf("seed %s: %v", sku, err)
	}
}

// stockOf reads the current stock of a SKU.
func stockOf(t *testing.T, s *Store, sku string) int {
	t.Helper()
	var stock int
	err := s.pool.QueryRow(t.Context(),
		`SELECT stock FROM inventory WHERE sku = $1`, sku).Scan(&stock)
	if err != nil {
		t.Fatalf("read stock of %s: %v", sku, err)
	}
	return stock
}

// 50 buyers compete for 30 units at the same instant. Without the row lock,
// several transactions would read the same stock and oversell.
func TestRecordSaleConcurrentSalesNeverOversell(t *testing.T) {
	s := newTestStore(t)
	const (
		sku    = "SKU-RACE"
		stock  = 30
		buyers = 50
	)
	seedSKU(t, s, sku, stock)

	// Atomic counters: plain ints incremented from many goroutines would
	// be a data race themselves.
	var succeeded, rejected atomic.Int32
	unexpected := make(chan error, buyers)

	var wg sync.WaitGroup
	for i := range buyers {
		// wg.Go starts a goroutine and tracks it; wg.Wait blocks until all
		// of them return. Like Task.Run plus Task.WhenAll.
		wg.Go(func() {
			_, err := s.RecordSale(t.Context(), Sale{
				IdempotencyKey: fmt.Sprintf("buyer-%d", i),
				SKU:            sku,
				Quantity:       1,
			})
			switch {
			case err == nil:
				succeeded.Add(1)
			case errors.Is(err, ErrInsufficientStock):
				rejected.Add(1)
			default:
				// t.Fatal must only be called from the test goroutine, so
				// errors are collected and reported after Wait.
				unexpected <- err
			}
		})
	}
	wg.Wait()
	close(unexpected)

	for err := range unexpected {
		t.Errorf("unexpected error: %v", err)
	}
	if got := succeeded.Load(); got != stock {
		t.Errorf("succeeded = %d, want %d", got, stock)
	}
	if got := rejected.Load(); got != buyers-stock {
		t.Errorf("rejected = %d, want %d", got, buyers-stock)
	}
	if got := stockOf(t, s, sku); got != 0 {
		t.Errorf("final stock = %d, want 0", got)
	}
}

// The same request retried 10 times at once must count as one sale.
func TestRecordSaleConcurrentRetriesAreIdempotent(t *testing.T) {
	s := newTestStore(t)
	const (
		sku     = "SKU-RETRY"
		stock   = 100
		retries = 10
	)
	seedSKU(t, s, sku, stock)

	var firstWrites, replays atomic.Int32
	unexpected := make(chan error, retries)

	var wg sync.WaitGroup
	for range retries {
		wg.Go(func() {
			result, err := s.RecordSale(t.Context(), Sale{
				IdempotencyKey: "same-request",
				SKU:            sku,
				Quantity:       5,
			})
			switch {
			case err != nil:
				unexpected <- err
			case result.Replayed:
				replays.Add(1)
			default:
				firstWrites.Add(1)
			}
		})
	}
	wg.Wait()
	close(unexpected)

	for err := range unexpected {
		t.Errorf("unexpected error: %v", err)
	}
	if got := firstWrites.Load(); got != 1 {
		t.Errorf("first writes = %d, want 1", got)
	}
	if got := replays.Load(); got != retries-1 {
		t.Errorf("replays = %d, want %d", got, retries-1)
	}
	if got := stockOf(t, s, sku); got != stock-5 {
		t.Errorf("final stock = %d, want %d", got, stock-5)
	}
}

func TestRecordSaleUnknownSKU(t *testing.T) {
	s := newTestStore(t)

	_, err := s.RecordSale(t.Context(), Sale{
		IdempotencyKey: "unknown",
		SKU:            "SKU-DOES-NOT-EXIST",
		Quantity:       1,
	})
	if !errors.Is(err, ErrSKUNotFound) {
		t.Errorf("err = %v, want ErrSKUNotFound", err)
	}
}
