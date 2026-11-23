package reorder

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var reordersPublished = promauto.NewCounter(prometheus.CounterOpts{
	Name: "reorder_messages_published_total",
	Help: "Reorder messages published to the queue.",
})
