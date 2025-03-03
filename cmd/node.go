package cmd

import (
	"fmt"

	"github.com/TFMV/furymesh/node"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

// nodeCmd starts the FuryMesh P2P node.
var nodeCmd = &cobra.Command{
	Use:   "node",
	Short: "Start FuryMesh P2P node",
	RunE: func(cmd *cobra.Command, args []string) error {
		return initNode(cmd, args)
	},
}

func init() {
	rootCmd.AddCommand(nodeCmd)
}

// initNode initializes and starts a node
func initNode(cmd *cobra.Command, args []string) error {
	// Initialize Zap logger for structured logging.
	logger, err := zap.NewProduction()
	if err != nil {
		fmt.Println("Failed to initialize logger:", err)
		return err
	}
	defer logger.Sync()

	// Initialize file manager
	fileManager, err := node.NewFileManager(logger)
	if err != nil {
		logger.Error("Failed to initialize file manager", zap.Error(err))
		return err
	}

	// Start file manager
	fileManager.Start()
	defer fileManager.Stop()

	// Start the WebRTC node with file manager
	n, err := node.NewNode(logger, fileManager)
	if err != nil {
		logger.Error("Failed to create node", zap.Error(err))
		return err
	}

	// Start the node
	if err := n.Start(); err != nil {
		logger.Error("Failed to start node", zap.Error(err))
		return err
	}

	// Wait for the node to exit
	n.Wait()

	return nil
}
