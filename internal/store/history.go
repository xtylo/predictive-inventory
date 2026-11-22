package store

import (
	"context"
	"fmt"
	"time"
)

// DaySales is the total units sold of one SKU on one calendar day.
type DaySales struct {
	Day   time.Time
	Units int
}

// SalesByDay returns one row per day for the last days days, oldest first,
// ending today. Days without sales are included with 0 units: for a
// forecast, "sold nothing" is information, not a missing row.
func (s *Store) SalesByDay(ctx context.Context, sku string, days int) ([]DaySales, error) {
	if days < 1 {
		return nil, fmt.Errorf("days must be at least 1, got %d", days)
	}

	// generate_series builds the calendar; the LEFT JOIN attaches sales to
	// it, and COALESCE turns the days with no match into 0. The interval is
	// computed in SQL from the integer parameter, which avoids the original
	// sketch's bug of putting $2 inside a string literal.
	rows, err := s.pool.Query(ctx, `
		SELECT d::date AS day, COALESCE(SUM(s.quantity), 0) AS units
		FROM generate_series(
		         (current_date - ($2::int - 1))::timestamp,
		         current_date::timestamp,
		         interval '1 day') AS d
		LEFT JOIN sales s
		       ON s.sku = $1
		      AND s.sold_at >= d
		      AND s.sold_at <  d + interval '1 day'
		GROUP BY d
		ORDER BY d`,
		sku, days,
	)
	if err != nil {
		return nil, fmt.Errorf("query sales by day: %w", err)
	}
	// Rows hold a connection from the pool until closed.
	defer rows.Close()

	history := make([]DaySales, 0, days)
	for rows.Next() {
		var row DaySales
		if err := rows.Scan(&row.Day, &row.Units); err != nil {
			return nil, fmt.Errorf("scan sales by day: %w", err)
		}
		history = append(history, row)
	}
	// rows.Err reports a failure that interrupted the iteration.
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sales by day: %w", err)
	}
	return history, nil
}
