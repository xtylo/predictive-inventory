package httpapi

import (
	"errors"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/xtylo/predictive-inventory/internal/store"
	"github.com/xtylo/predictive-inventory/internal/worker"
)

// retryAfterSeconds tells clients how long to back off when the queue is full.
const retryAfterSeconds = "5"

type forecastHandler struct {
	stock       StockReader
	predictions PredictionQueue
}

// refresh handles POST /forecasts/:sku/refresh: an explicit request to
// recompute a forecast, for example from an admin screen or a scheduler.
//
// Responses: 202 queued, 404 unknown SKU, 429 queue full (with
// Retry-After), 503 shutting down. This is where back-pressure becomes
// visible to clients; POST /sales never fails because of the queue.
func (h forecastHandler) refresh(c *gin.Context) {
	sku := c.Param("sku")

	// Reject unknown SKUs here so they never consume a slot in the queue.
	_, err := h.stock.GetStock(c.Request.Context(), sku)
	switch {
	case errors.Is(err, store.ErrSKUNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	case err != nil:
		log.Printf("get stock of %s: %v", sku, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	err = h.predictions.Enqueue(sku)
	switch {
	case errors.Is(err, worker.ErrQueueFull):
		c.Header("Retry-After", retryAfterSeconds)
		c.JSON(http.StatusTooManyRequests, gin.H{"error": err.Error()})
		return
	case errors.Is(err, worker.ErrClosed):
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	case err != nil:
		log.Printf("enqueue forecast for %s: %v", sku, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	// 202 Accepted: the request is valid and queued, the work happens later.
	c.JSON(http.StatusAccepted, gin.H{"sku": sku, "status": "queued"})
}
