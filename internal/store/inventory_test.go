package store

import (
	"errors"
	"testing"
)

// Reuses newTestStore and seedSKU from sales_test.go: every _test.go file
// of a package is compiled together.
func TestGetStock(t *testing.T) {
	s := newTestStore(t)
	seedSKU(t, s, "SKU-STOCK", 42)

	got, err := s.GetStock(t.Context(), "SKU-STOCK")
	if err != nil {
		t.Fatalf("GetStock() error: %v", err)
	}
	if got != 42 {
		t.Errorf("GetStock() = %d, want 42", got)
	}

	if _, err := s.GetStock(t.Context(), "SKU-MISSING"); !errors.Is(err, ErrSKUNotFound) {
		t.Errorf("err = %v, want ErrSKUNotFound", err)
	}
}
