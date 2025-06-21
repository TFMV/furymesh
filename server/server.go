package server

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/viper"
	"go.uber.org/zap"

	"github.com/TFMV/furymesh/metrics"
)

// StartAPIServer starts the Fiber-based API server that exposes monitoring endpoints.
func StartAPIServer(logger *zap.Logger) {
	metrics.Register()
	app := fiber.New()

	// Define an endpoint to check the node status.
	app.Get("/status", func(c *fiber.Ctx) error {
		logger.Info("Status request received")
		return c.JSON(fiber.Map{
			"status": "running",
			"peers":  []string{"peer1", "peer2"},
		})
	})

	// Expose Prometheus metrics
	app.Get("/metrics", adaptor.HTTPHandler(promhttp.Handler()))

	// Get the API port from configuration; default to 8080 if not set.
	port := viper.GetInt("api.port")
	if port == 0 {
		port = 8080
	}

	logger.Info("Starting FuryMesh API server", zap.Int("port", port))
	err := app.Listen(fmt.Sprintf(":%d", port))
	if err != nil {
		logger.Fatal("Failed to start API server", zap.Error(err))
	}
}
