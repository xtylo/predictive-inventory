// Package httpapi exposes the service over HTTP using Gin.
package httpapi

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/xtylo/predictive-inventory/internal/cache"
	"github.com/xtylo/predictive-inventory/internal/store"
)

// Go interfaces are satisfied implicitly: *store.Store never says
// "implements SaleRecorder", it just has a matching method. The convention
// is the opposite of C#: the consumer declares the small interfaces it
// needs, so tests can pass fakes without touching the other packages.

// SaleRecorder records sales (implemented by *store.Store).
type SaleRecorder interface {
	RecordSale(ctx context.Context, sale store.Sale) (store.SaleResult, error)
}

// StockReader reads current stock (implemented by *store.Store).
type StockReader interface {
	GetStock(ctx context.Context, sku string) (int, error)
}

// ForecastReader reads cached forecasts (implemented by *cache.Cache).
type ForecastReader interface {
	GetForecast(ctx context.Context, sku string) (cache.Forecast, error)
}

// PredictionQueue schedules background forecasts (implemented by *worker.Pool).
type PredictionQueue interface {
	Enqueue(sku string) error
}

// Dependencies groups everything the handlers need. A struct keeps
// NewRouter's signature stable as the service grows, instead of adding
// one more parameter per feature.
type Dependencies struct {
	Sales       SaleRecorder
	Stock       StockReader
	Forecasts   ForecastReader
	Predictions PredictionQueue
}

// NewRouter wires every route to its handler.
func NewRouter(deps Dependencies) *gin.Engine {
	// gin.Default() attaches request logging and panic recovery middlewares.
	router := gin.Default()
	router.Use(metricsMiddleware())

	// Prometheus scrapes this endpoint. promhttp serves the default registry,
	// which also includes Go runtime metrics (goroutines, GC, memory).
	// gin.WrapH adapts a standard net/http handler to Gin.
	router.GET("/metrics", gin.WrapH(promhttp.Handler()))

	// Liveness endpoint used by Docker, load balancers and smoke tests.
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	sales := salesHandler{sales: deps.Sales, predictions: deps.Predictions}
	router.POST("/sales", sales.create)

	inventory := inventoryHandler{stock: deps.Stock, forecasts: deps.Forecasts}
	router.GET("/inventory/:sku", inventory.get)

	forecasts := forecastHandler{stock: deps.Stock, predictions: deps.Predictions}
	router.POST("/forecasts/:sku/refresh", forecasts.refresh)

	return router
}
