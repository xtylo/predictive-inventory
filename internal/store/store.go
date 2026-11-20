// Package store is the PostgreSQL data access layer.
package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/xtylo/predictive-inventory/migrations"
)

// Store wraps a connection pool. A pool is safe for concurrent use by many
// goroutines; a single pgx.Conn is not, which is why the original sketch broke.
type Store struct {
	pool *pgxpool.Pool
}

// New opens the pool and pings the database so startup fails fast when
// PostgreSQL is unreachable, instead of failing on the first request.
//
// ctx plays the role of a CancellationToken in .NET: it carries deadlines
// and cancellation into every call that does I/O.
func New(ctx context.Context, dsn string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		// %w wraps the original error, so callers can still inspect it
		// with errors.Is / errors.As while the message gains context.
		return nil, fmt.Errorf("create pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return &Store{pool: pool}, nil
}

// Close releases every connection in the pool.
func (s *Store) Close() {
	s.pool.Close()
}

// Migrate applies every pending migration and returns how many ran.
func (s *Store) Migrate(ctx context.Context) (int, error) {
	// goose works with database/sql, Go's standard database interface.
	// OpenDBFromPool adapts our pool to it; closing this *sql.DB does not
	// close the underlying pool.
	db := stdlib.OpenDBFromPool(s.pool)
	defer db.Close()

	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations.FS)
	if err != nil {
		return 0, fmt.Errorf("create migration provider: %w", err)
	}

	results, err := provider.Up(ctx)
	if err != nil {
		return 0, fmt.Errorf("apply migrations: %w", err)
	}

	return len(results), nil
}
