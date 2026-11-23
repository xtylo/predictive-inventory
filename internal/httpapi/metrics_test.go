package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestMetricsEndpointExposesRouteTemplates(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewRouter(Dependencies{Stock: fakeStock{stock: 1}, Forecasts: fakeForecasts{}})

	// One request to a parameterized route...
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/inventory/SKU-42", nil))

	// ...then read /metrics like Prometheus would.
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /metrics status = %d, want 200", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, `route="/inventory/:sku"`) {
		t.Error("metrics do not contain the route template /inventory/:sku")
	}
	// The raw path must never become a label value: one series per SKU
	// would grow without bound.
	if strings.Contains(body, "SKU-42") {
		t.Error("metrics contain the raw path SKU-42")
	}
}
