package predictor

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// modelTokens tracks token usage, the main cost driver of the service.
// Multiplied by the model's price per token, it gives the spend.
var modelTokens = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "predictor_model_tokens_total",
	Help: "Tokens used by the forecasting model, by direction (input, output).",
}, []string{"direction"})
