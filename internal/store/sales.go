package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Sentinel errors: fixed values callers compare against with errors.Is.
// They are the Go replacement for throwing typed exceptions.
var (
	ErrSKUNotFound       = errors.New("sku not found")
	ErrInsufficientStock = errors.New("insufficient stock")
)

// Sale is a sale event as received from a client.
type Sale struct {
	IdempotencyKey string
	SKU            string
	Quantity       int
}

// SaleResult describes the stored sale and the stock left after it.
type SaleResult struct {
	SaleID   int64
	SKU      string
	Quantity int
	Stock    int
	// Replayed is true when the idempotency key was already used:
	// the original sale is returned and stock is not touched again.
	Replayed bool
}

// RecordSale stores a sale and decrements stock in a single transaction.
//
// Concurrency: SELECT ... FOR UPDATE locks the SKU row, so concurrent sales
// of the same SKU run one after another and can never oversell.
// Idempotency: the UNIQUE idempotency_key turns a retried request into a
// no-op that returns the original sale.
func (s *Store) RecordSale(ctx context.Context, sale Sale) (SaleResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return SaleResult{}, fmt.Errorf("begin transaction: %w", err)
	}
	// Rollback is a no-op once Commit succeeded, so deferring it guarantees
	// cleanup on every early return below. Its error is irrelevant here.
	defer func() { _ = tx.Rollback(ctx) }()

	// 1. Lock the inventory row. Other transactions selecting the same SKU
	//    FOR UPDATE wait here until this one commits or rolls back.
	var stock int
	err = tx.QueryRow(ctx,
		`SELECT stock FROM inventory WHERE sku = $1 FOR UPDATE`,
		sale.SKU,
	).Scan(&stock)
	if errors.Is(err, pgx.ErrNoRows) {
		return SaleResult{}, ErrSKUNotFound
	}
	if err != nil {
		return SaleResult{}, fmt.Errorf("lock inventory row: %w", err)
	}

	// 2. Insert the sale. ON CONFLICT DO NOTHING returns no row when the
	//    idempotency key already exists, which Scan reports as ErrNoRows.
	var saleID int64
	err = tx.QueryRow(ctx,
		`INSERT INTO sales (idempotency_key, sku, quantity)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (idempotency_key) DO NOTHING
		 RETURNING id`,
		sale.IdempotencyKey, sale.SKU, sale.Quantity,
	).Scan(&saleID)
	if errors.Is(err, pgx.ErrNoRows) {
		return replay(ctx, tx, sale.IdempotencyKey, stock)
	}
	if err != nil {
		return SaleResult{}, fmt.Errorf("insert sale: %w", err)
	}

	// 3. Check stock. Returning here rolls back the insert above.
	//    %w keeps ErrInsufficientStock detectable while adding detail.
	if stock < sale.Quantity {
		return SaleResult{}, fmt.Errorf("%w: have %d, want %d",
			ErrInsufficientStock, stock, sale.Quantity)
	}

	// 4. Decrement stock and read the new value in the same statement.
	err = tx.QueryRow(ctx,
		`UPDATE inventory
		 SET stock = stock - $2, updated_at = now()
		 WHERE sku = $1
		 RETURNING stock`,
		sale.SKU, sale.Quantity,
	).Scan(&stock)
	if err != nil {
		return SaleResult{}, fmt.Errorf("decrement stock: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return SaleResult{}, fmt.Errorf("commit sale: %w", err)
	}

	return SaleResult{
		SaleID:   saleID,
		SKU:      sale.SKU,
		Quantity: sale.Quantity,
		Stock:    stock,
	}, nil
}

// replay loads the sale previously stored under key. It only reads, so the
// caller's deferred Rollback simply releases the row lock.
func replay(ctx context.Context, tx pgx.Tx, key string, stock int) (SaleResult, error) {
	result := SaleResult{Stock: stock, Replayed: true}
	err := tx.QueryRow(ctx,
		`SELECT id, sku, quantity FROM sales WHERE idempotency_key = $1`,
		key,
	).Scan(&result.SaleID, &result.SKU, &result.Quantity)
	if err != nil {
		return SaleResult{}, fmt.Errorf("load replayed sale: %w", err)
	}
	return result, nil
}
