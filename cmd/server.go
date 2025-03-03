package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"github.com/TFMV/furymesh/server"
)

// serverCmd starts the FuryMesh API server.
var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Start FuryMesh API server",
	Run: func(cmd *cobra.Command, args []string) {
		// Initialize Zap logger.
		logger, err := zap.NewProduction()
		if err != nil {
			fmt.Println("Failed to initialize logger:", err)
			return
		}
		defer logger.Sync()

		// Start the Fiber-based API server.
		server.StartAPIServer(logger)
	},
}

func init() {
	rootCmd.AddCommand(serverCmd)
}
