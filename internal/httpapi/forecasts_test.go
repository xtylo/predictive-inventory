package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/xtylo/predictive-inventory/internal/store"
	"github.com/xtylo/predictive-inventory/internal/worker"
)

// fakeQueue records what was enqueued and returns a preset error.
type fakeQueue struct {
	err      error
	enqueued []string
}

// Pointer receiver: Enqueue appends to the slice, and the test must see
// that change. A value receiver would modify a copy.
func (f *fakeQueue) Enqueue(sku string) error {
	if f.err != nil {
		return f.err
	}
	f.enqueued = append(f.enqueued, sku)
	return nil
}

func TestRefreshForecast(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		stock          fakeStock
		queue          *fakeQueue
		wantStatus     int
		wantRetryAfter string
		wantEnqueued   int
	}{
		{
			name:         "queued",
			stock:        fakeStock{stock: 10},
			queue:        &fakeQueue{},
			wantStatus:   http.StatusAccepted,
			wantEnqueued: 1,
		},
		{
			name:           "queue full applies back-pressure",
			stock:          fakeStock{stock: 10},
			queue:          &fakeQueue{err: worker.ErrQueueFull},
			wantStatus:     http.StatusTooManyRequests,
			wantRetryAfter: retryAfterSeconds,
		},
		{
			name:       "shutting down",
			stock:      fakeStock{stock: 10},
			queue:      &fakeQueue{err: worker.ErrClosed},
			wantStatus: http.StatusServiceUnavailable,
		},
		{
			name:       "unknown sku is not enqueued",
			stock:      fakeStock{err: store.ErrSKUNotFound},
			queue:      &fakeQueue{},
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := NewRouter(Dependencies{Stock: tt.stock, Predictions: tt.queue})

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/forecasts/SKU-1/refresh", nil)
			router.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body: %s)", rec.Code, tt.wantStatus, rec.Body)
			}
			if got := rec.Header().Get("Retry-After"); got != tt.wantRetryAfter {
				t.Errorf("Retry-After = %q, want %q", got, tt.wantRetryAfter)
			}
			if got := len(tt.queue.enqueued); got != tt.wantEnqueued {
				t.Errorf("enqueued = %d, want %d", got, tt.wantEnqueued)
			}
		})
	}
}
