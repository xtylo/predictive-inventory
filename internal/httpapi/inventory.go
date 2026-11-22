package httpapi

import (
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/xtylo/predictive-inventory/internal/cache"
	"github.com/xtylo/predictive-inventory/internal/planning"
	"github.com/xtylo/predictive-inventory/internal/store"
)

// Values of inventoryResponse.Status.
const (
	statusOK              = "ok"
	statusForecastPending = "forecast_pending"
)

type inventoryHandler struct {
	stock     StockReader
	forecasts ForecastReader
}

type forecastResponse struct {
	Demand      int       `json:"demand"`
	HorizonDays int       `json:"horizon_days"`
	GeneratedAt time.Time `json:"generated_at"`
	Rationale   string    `json:"rationale,omitempty"`
}

// Pointer fields serialize as JSON null when nil. That is how Go expresses
// "no value", the role Nullable<int> plays in C#.
type inventoryResponse struct {
	SKU             string            `json:"sku"`
	Stock           int               `json:"stock"`
	Forecast        *forecastResponse `json:"forecast"`
	ReorderQuantity *int              `json:"reorder_quantity"`
	Status          string            `json:"status"`
}

// get handles GET /inventory/:sku.
//
// Responses: 200 with the reorder quantity, 200 with status
// "forecast_pending" when no forecast is cached yet, 404 unknown SKU.
// A missing forecast is a normal state, not an error: the original sketch
// answered it with a 500.
func (h inventoryHandler) get(c *gin.Context) {
	sku := c.Param("sku")
	ctx := c.Request.Context()

	stock, err := h.stock.GetStock(ctx, sku)
	switch {
	case errors.Is(err, store.ErrSKUNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	case err != nil:
		log.Printf("get stock of %s: %v", sku, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	resp := inventoryResponse{SKU: sku, Stock: stock, Status: statusForecastPending}

	forecast, err := h.forecasts.GetForecast(ctx, sku)
	switch {
	case errors.Is(err, cache.ErrNotFound):
		c.JSON(http.StatusOK, resp)
		return
	case err != nil:
		log.Printf("get forecast of %s: %v", sku, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	reorder := planning.ReorderQuantity(stock, forecast.Demand)
	resp.Forecast = &forecastResponse{
		Demand:      forecast.Demand,
		HorizonDays: forecast.HorizonDays,
		GeneratedAt: forecast.GeneratedAt,
		Rationale:   forecast.Rationale,
	}
	// &reorder takes the address of the local variable. Go's escape
	// analysis moves it to the heap, so the pointer stays valid after
	// the function returns.
	resp.ReorderQuantity = &reorder
	resp.Status = statusOK

	c.JSON(http.StatusOK, resp)
}
