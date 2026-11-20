package cache

import (
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

const testTTL = time.Hour

// newTestCache runs an in-memory Redis server inside the test process:
// no Docker, starts in microseconds, closed automatically by RunT.
func newTestCache(t *testing.T) (*Cache, *miniredis.Miniredis) {
	t.Helper()

	server := miniredis.RunT(t)

	c, err := New(t.Context(), server.Addr(), testTTL)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	return c, server
}

func TestSetAndGetForecast(t *testing.T) {
	c, _ := newTestCache(t)
	want := Forecast{
		SKU:         "SKU-1",
		Demand:      120,
		HorizonDays: 30,
		GeneratedAt: time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC),
	}

	if err := c.SetForecast(t.Context(), want); err != nil {
		t.Fatalf("SetForecast() error: %v", err)
	}

	got, err := c.GetForecast(t.Context(), "SKU-1")
	if err != nil {
		t.Fatalf("GetForecast() error: %v", err)
	}

	// time.Time must be compared with Equal, not ==: two values can denote
	// the same instant with different internal representations.
	if got.SKU != want.SKU || got.Demand != want.Demand ||
		got.HorizonDays != want.HorizonDays || !got.GeneratedAt.Equal(want.GeneratedAt) {
		t.Errorf("GetForecast() = %+v, want %+v", got, want)
	}
}

func TestGetForecastMissing(t *testing.T) {
	c, _ := newTestCache(t)

	_, err := c.GetForecast(t.Context(), "SKU-NEVER-SET")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestForecastExpiresAfterTTL(t *testing.T) {
	c, server := newTestCache(t)

	if err := c.SetForecast(t.Context(), Forecast{SKU: "SKU-1", Demand: 10}); err != nil {
		t.Fatalf("SetForecast() error: %v", err)
	}

	// miniredis does not expire keys on its own clock; FastForward moves
	// time ahead so the test runs instantly instead of waiting an hour.
	server.FastForward(testTTL + time.Second)

	_, err := c.GetForecast(t.Context(), "SKU-1")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound after TTL", err)
	}
}
