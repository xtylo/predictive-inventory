package worker

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Values of the outcome label.
const (
	outcomeSuccess = "success"
	outcomeSkipped = "skipped" // dedup lock held: forecast recently
	outcomeFailed  = "failed"
)

var (
	queueDepth = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "worker_queue_depth",
		Help: "SKUs waiting in the prediction queue.",
	})

	enqueueRejected = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "worker_enqueue_rejected_total",
		Help: "Enqueue calls rejected, by reason (full, closed).",
	}, []string{"reason"})

	jobsProcessed = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "worker_jobs_total",
		Help: "Prediction jobs processed, by outcome (success, skipped, failed).",
	}, []string{"outcome"})

	predictionDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name: "worker_prediction_duration_seconds",
		Help: "Time spent in Predictor.Predict, successful or not.",
		// Model calls take seconds, not milliseconds.
		Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 20, 30, 60},
	})
)
