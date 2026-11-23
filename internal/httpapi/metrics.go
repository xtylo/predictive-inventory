package httpapi

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// promauto registers the metric in the default registry when the package
// loads. Package-level variables are created once per process, so building
// many routers (as the tests do) never registers a metric twice.
var httpRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name: "http_request_duration_seconds",
	Help: "HTTP request latency by method, route and status code.",
	// Buckets around the README target (p99 < 200 ms), so quantiles near
	// it are precise.
	Buckets: []float64{0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.2, 0.5, 1, 2.5},
}, []string{"method", "route", "status"})

// metricsMiddleware times every request. Gin middleware is a handler that
// calls c.Next() to run the rest of the chain and then resumes, like
// ASP.NET Core middleware awaiting next(context).
func metricsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		// FullPath is the route template ("/inventory/:sku"), not the raw
		// path ("/inventory/SKU-42"). Using the raw path would create one
		// time series per SKU and grow without bound (label cardinality).
		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}

		httpRequestDuration.
			WithLabelValues(c.Request.Method, route, strconv.Itoa(c.Writer.Status())).
			Observe(time.Since(start).Seconds())
	}
}
