// Command api is the entry point of the Predictive Inventory service.
//
// In Go, an executable is a package named "main" with a func main().
// By convention each binary lives in its own folder under cmd/.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	// Packages of this module are imported by their full path:
	// module name (from go.mod) + folder.
	"github.com/xtylo/predictive-inventory/internal/cache"
	"github.com/xtylo/predictive-inventory/internal/config"
	"github.com/xtylo/predictive-inventory/internal/httpapi"
	"github.com/xtylo/predictive-inventory/internal/predictor"
	"github.com/xtylo/predictive-inventory/internal/store"
	"github.com/xtylo/predictive-inventory/internal/worker"
)

const (
	// forecastTTL is how long a forecast stays valid in Redis. After that the
	// inventory endpoint reports "forecast_pending" until a new one is computed.
	forecastTTL = 24 * time.Hour

	// shutdownTimeout bounds the whole graceful shutdown. Container platforms
	// such as ECS or Kubernetes kill the process some seconds after SIGTERM,
	// so this must stay below their grace period.
	shutdownTimeout = 15 * time.Second
)

func main() {
	// Fail fast: a service with a broken configuration must not start.
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("invalid configuration: %v", err)
	}

	// Startup gets a deadline: if a dependency does not answer within
	// 10 seconds, the context is cancelled and the calls below return an error.
	startupCtx, cancelStartup := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelStartup()

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

	// The fake predictor stands in for Claude until session 5. The 2-second
	// delay imitates a slow model call, so the async flow is observable.
	model := predictor.Fake{Demand: 800, HorizonDays: 30, Delay: 2 * time.Second}

	// *cache.Cache is both the forecast store and the distributed lock.
	predictions := worker.New(worker.Config{
		Workers:     4,
		QueueSize:   100,
		DedupWindow: 5 * time.Minute,
		JobTimeout:  30 * time.Second,
	}, model, forecasts, forecasts)
	predictions.Start()

	router := httpapi.NewRouter(httpapi.Dependencies{
		Sales:       db,
		Stock:       db,
		Forecasts:   forecasts,
		Predictions: predictions,
	})

	// An explicit http.Server instead of router.Run: Run cannot be stopped
	// gracefully. ReadHeaderTimeout protects against clients that open a
	// connection and never finish sending headers (Slowloris).
	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	// ctx is cancelled when the process receives Ctrl+C (SIGINT) or
	// SIGTERM, the signal Docker sends on "docker stop".
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// ListenAndServe blocks, so it runs in its own goroutine while main
	// waits for the shutdown signal.
	go func() {
		log.Printf("listening on %s", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server failed: %v", err)
		}
	}()

	<-ctx.Done()
	log.Print("shutdown signal received")

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancelShutdown()

	// Order matters. First the HTTP server: it stops accepting connections
	// and waits for in-flight requests, so no handler can enqueue afterwards.
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("http shutdown: %v", err)
	}
	// Then the workers: they finish what is queued within the same deadline.
	if err := predictions.Shutdown(shutdownCtx); err != nil {
		log.Printf("worker shutdown: %v", err)
	}
	// Finally the deferred Close calls release Redis and PostgreSQL.
	log.Print("stopped")
}
