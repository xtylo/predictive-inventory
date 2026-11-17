// Command api is the entry point of the Predictive Inventory service.
//
// In Go, an executable is a package named "main" with a func main().
// By convention each binary lives in its own folder under cmd/.
package main

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	// Packages of this module are imported by their full path:
	// module name (from go.mod) + folder.
	"github.com/xtylo/predictive-inventory/internal/config"
)

func main() {
	// Fail fast: a service with a broken configuration must not start.
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("invalid configuration: %v", err)
	}

	// gin.Default() returns a router with two middlewares already attached:
	// request logging and panic recovery.
	router := gin.Default()

	// Liveness endpoint used by Docker, load balancers and smoke tests.
	// gin.H is just a shortcut for map[string]any.
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Go has no exceptions: functions return an error value and the caller
	// decides what to do with it. Run blocks until the server stops.
	if err := router.Run(":" + cfg.Port); err != nil {
		log.Fatalf("server stopped: %v", err)
	}
}
