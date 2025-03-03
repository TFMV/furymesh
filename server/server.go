package server

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

// StartAPIServer starts the Fiber-based API server that exposes monitoring endpoints.
func StartAPIServer(logger *zap.Logger) {
	app := fiber.New()

	// Define an endpoint to check the node status.
	app.Get("/status", func(c *fiber.Ctx) error {
		logger.Info("Status request received")
		return c.JSON(fiber.Map{
			"status": "running",
			"peers":  []string{"peer1", "peer2"},
		})
	})

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
