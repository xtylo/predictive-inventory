// Command api is the entry point of the Predictive Inventory service.
//
// In Go, an executable is a package named "main" with a func main().
// By convention each binary lives in its own folder under cmd/.
package main

import (
	"context"
	"log"
	"time"

	// Packages of this module are imported by their full path:
	// module name (from go.mod) + folder.
	"github.com/xtylo/predictive-inventory/internal/cache"
	"github.com/xtylo/predictive-inventory/internal/config"
	"github.com/xtylo/predictive-inventory/internal/httpapi"
	"github.com/xtylo/predictive-inventory/internal/store"
)

// forecastTTL is how long a forecast stays valid in Redis. After that the
// inventory endpoint reports "forecast_pending" until a new one is computed.
const forecastTTL = 24 * time.Hour

func main() {
	// Fail fast: a service with a broken configuration must not start.
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("invalid configuration: %v", err)
	}

	// Startup gets a deadline: if a dependency does not answer within
	// 10 seconds, the context is cancelled and the calls below return an error.
	startupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db, err := store.New(startupCtx, cfg.PostgresDSN)
	if err != nil {
		log.Fatalf("connect to database: %v", err)
	}
	// defer runs when main returns, in reverse order of declaration.
	defer db.Close()

	applied, err := db.Migrate(startupCtx)
	if err != nil {
		log.Fatalf("migrate database: %v", err)
	}
	log.Printf("database ready, %d migration(s) applied", applied)

	forecasts, err := cache.New(startupCtx, cfg.RedisEndpoint, forecastTTL)
	if err != nil {
		log.Fatalf("connect to redis: %v", err)
	}
	defer forecasts.Close()

	// *store.Store satisfies both SaleRecorder and StockReader;
	// *cache.Cache satisfies ForecastReader. No registration needed.
	router := httpapi.NewRouter(httpapi.Dependencies{
		Sales:     db,
		Stock:     db,
		Forecasts: forecasts,
	})

	// Go has no exceptions: functions return an error value and the caller
	// decides what to do with it. Run blocks until the server stops.
	if err := router.Run(":" + cfg.Port); err != nil {
		log.Fatalf("server stopped: %v", err)
	}
}
