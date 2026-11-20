package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/xtylo/predictive-inventory/internal/cache"
	"github.com/xtylo/predictive-inventory/internal/store"
)

// Fakes: tiny structs that satisfy the handler's interfaces implicitly.
// No mocking library is needed.

type fakeStock struct {
	stock int
	err   error
}

func (f fakeStock) GetStock(context.Context, string) (int, error) {
	return f.stock, f.err
}

type fakeForecasts struct {
	forecast cache.Forecast
	err      error
}

func (f fakeForecasts) GetForecast(context.Context, string) (cache.Forecast, error) {
	return f.forecast, f.err
}

func intPtr(n int) *int { return &n }

func TestGetInventory(t *testing.T) {
	// Silences Gin's debug output during tests.
	gin.SetMode(gin.TestMode)

	forecast := cache.Forecast{
		SKU:         "SKU-1",
		Demand:      100,
		HorizonDays: 30,
		GeneratedAt: time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC),
	}

	tests := []struct {
		name        string
		stock       fakeStock
		forecasts   fakeForecasts
		wantStatus  int
		wantState   string
		wantReorder *int
	}{
		{
			name:        "below target needs reorder",
			stock:       fakeStock{stock: 120},
			forecasts:   fakeForecasts{forecast: forecast},
			wantStatus:  http.StatusOK,
			wantState:   statusOK,
			wantReorder: intPtr(30),
		},
		{
			name:        "overstocked needs nothing",
			stock:       fakeStock{stock: 500},
			forecasts:   fakeForecasts{forecast: forecast},
			wantStatus:  http.StatusOK,
			wantState:   statusOK,
			wantReorder: intPtr(0),
		},
		{
			name:       "forecast not computed yet",
			stock:      fakeStock{stock: 120},
			forecasts:  fakeForecasts{err: cache.ErrNotFound},
			wantStatus: http.StatusOK,
			wantState:  statusForecastPending,
		},
		{
			name:       "unknown sku",
			stock:      fakeStock{err: store.ErrSKUNotFound},
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "database failure",
			stock:      fakeStock{err: errors.New("connection reset")},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:       "redis failure",
			stock:      fakeStock{stock: 120},
			forecasts:  fakeForecasts{err: errors.New("connection reset")},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := NewRouter(Dependencies{Stock: tt.stock, Forecasts: tt.forecasts})

			// httptest runs the full router in memory: routing, middleware
			// and JSON encoding, with no network port involved.
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/inventory/SKU-1", nil)
			router.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body: %s)", rec.Code, tt.wantStatus, rec.Body)
			}
			if tt.wantStatus != http.StatusOK {
				return
			}

			var got inventoryResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if got.Status != tt.wantState {
				t.Errorf("status field = %q, want %q", got.Status, tt.wantState)
			}

			switch {
			case tt.wantReorder == nil && got.ReorderQuantity != nil:
				t.Errorf("reorder_quantity = %d, want null", *got.ReorderQuantity)
			case tt.wantReorder != nil && got.ReorderQuantity == nil:
				t.Errorf("reorder_quantity = null, want %d", *tt.wantReorder)
			case tt.wantReorder != nil && *got.ReorderQuantity != *tt.wantReorder:
				t.Errorf("reorder_quantity = %d, want %d", *got.ReorderQuantity, *tt.wantReorder)
			}
		})
	}
}
