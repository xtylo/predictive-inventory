package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// GetStock returns the current stock of sku, or ErrSKUNotFound.
//
// It is a plain read without FOR UPDATE: the value may change right after
// it is returned, which is fine for a report but not for a decision that
// writes, like RecordSale.
func (s *Store) GetStock(ctx context.Context, sku string) (int, error) {
	var stock int
	err := s.pool.QueryRow(ctx,
		`SELECT stock FROM inventory WHERE sku = $1`,
		sku,
	).Scan(&stock)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrSKUNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("read stock: %w", err)
	}
	return stock, nil
}
