// Package httpapi exposes the service over HTTP using Gin.
package httpapi

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/xtylo/predictive-inventory/internal/store"
)

// SaleRecorder is everything the HTTP layer needs from storage.
//
// Go interfaces are satisfied implicitly: *store.Store never says
// "implements SaleRecorder", it just has a matching method. The convention
// is the opposite of C#: the consumer declares the small interface it needs,
// so tests can pass a fake without touching the store package.
type SaleRecorder interface {
	RecordSale(ctx context.Context, sale store.Sale) (store.SaleResult, error)
}

// NewRouter wires every route to its handler.
func NewRouter(sales SaleRecorder) *gin.Engine {
	// gin.Default() attaches request logging and panic recovery middlewares.
	router := gin.Default()

	// Liveness endpoint used by Docker, load balancers and smoke tests.
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	h := salesHandler{sales: sales}
	router.POST("/sales", h.create)

	return router
}
