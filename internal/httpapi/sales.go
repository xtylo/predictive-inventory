package httpapi

import (
	"errors"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/xtylo/predictive-inventory/internal/store"
)

type salesHandler struct {
	sales       SaleRecorder
	predictions PredictionQueue
}

// Struct tags (the text between backticks) are metadata read at runtime:
// "json" maps field names, "binding" declares validation rules for Gin.
type createSaleRequest struct {
	SKU      string `json:"sku" binding:"required"`
	Quantity int    `json:"quantity" binding:"required,gt=0"`
}

type saleResponse struct {
	SaleID   int64  `json:"sale_id"`
	SKU      string `json:"sku"`
	Quantity int    `json:"quantity"`
	Stock    int    `json:"stock"`
	Replayed bool   `json:"replayed"`
}

// create handles POST /sales.
//
// Responses: 201 new sale, 200 replay of a known Idempotency-Key,
// 400 invalid request, 404 unknown SKU, 409 not enough stock.
func (h salesHandler) create(c *gin.Context) {
	key := c.GetHeader("Idempotency-Key")
	if key == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Idempotency-Key header is required"})
		return
	}

	var req createSaleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// The request context is cancelled if the client disconnects, which
	// also cancels the database calls made with it.
	result, err := h.sales.RecordSale(c.Request.Context(), store.Sale{
		IdempotencyKey: key,
		SKU:            req.SKU,
		Quantity:       req.Quantity,
	})

	// errors.Is walks the chain of wrapped errors, so it still matches
	// ErrInsufficientStock after fmt.Errorf added details with %w.
	switch {
	case errors.Is(err, store.ErrSKUNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	case errors.Is(err, store.ErrInsufficientStock):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	case err != nil:
		// Unexpected failures are logged in full but not exposed to clients.
		log.Printf("record sale: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	status := http.StatusCreated
	if result.Replayed {
		status = http.StatusOK
	} else {
		// A new sale changes the demand picture, so schedule a forecast.
		// Best effort: the sale is already committed and must not fail
		// because the refresh could not be queued. The worker's dedup lock
		// collapses bursts of sales of one SKU into a single prediction.
		if err := h.predictions.Enqueue(result.SKU); err != nil {
			log.Printf("schedule forecast for %s: %v", result.SKU, err)
		}
	}

	c.JSON(status, saleResponse{
		SaleID:   result.SaleID,
		SKU:      result.SKU,
		Quantity: result.Quantity,
		Stock:    result.Stock,
		Replayed: result.Replayed,
	})
}
