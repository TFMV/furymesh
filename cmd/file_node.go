package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/TFMV/furymesh/node"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var (
	nodePeerID  string
	nodeFileID  string
	nodeTimeout int
)

// fileRequestCmd represents the command to request a file from a peer
var fileRequestCmd = &cobra.Command{
	Use:   "request",
	Short: "Request a file from a peer",
	Long:  `Request a file from a peer using the node's file transfer system.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Initialize logger
		logger, err := zap.NewProduction()
		if err != nil {
			return fmt.Errorf("failed to initialize logger: %w", err)
		}
		defer logger.Sync()

		// Get the active node
		n, err := getActiveNode(logger)
		if err != nil {
			return fmt.Errorf("failed to get active node: %w", err)
		}

		// Create a context with timeout
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(nodeTimeout)*time.Second)
		defer cancel()

		// Request the file
		if err := n.GetFileManager().RequestFileFromPeer(ctx, nodePeerID, nodeFileID); err != nil {
			return fmt.Errorf("failed to request file: %w", err)
		}

		fmt.Printf("File request sent to peer %s for file %s\n", nodePeerID, nodeFileID)
		return nil
	},
}

// fileStatusCmd represents the command to check the status of a file transfer
var fileStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check the status of a file transfer",
	Long:  `Check the status of a file transfer using the node's file transfer system.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Initialize logger
		logger, err := zap.NewProduction()
		if err != nil {
			return fmt.Errorf("failed to initialize logger: %w", err)
		}
		defer logger.Sync()

		// Get the active node
		n, err := getActiveNode(logger)
		if err != nil {
			return fmt.Errorf("failed to get active node: %w", err)
		}

		// Get transfer stats
		stats, err := n.GetFileManager().GetTransferStats(nodeFileID)
		if err != nil {
			return fmt.Errorf("failed to get transfer stats: %w", err)
		}

		// Print stats
		fmt.Printf("Transfer status for file %s:\n", nodeFileID)
		fmt.Printf("  Status: %s\n", stats.Status)
		fmt.Printf("  Start time: %s\n", stats.StartTime.Format(time.RFC3339))
		if !stats.EndTime.IsZero() {
			fmt.Printf("  End time: %s\n", stats.EndTime.Format(time.RFC3339))
		}
		fmt.Printf("  Progress: %d/%d chunks (%d%%)\n",
			stats.ChunksTransferred,
			stats.TotalChunks,
			int(float64(stats.ChunksTransferred)/float64(stats.TotalChunks)*100))
		fmt.Printf("  Transfer rate: %.2f KB/s\n", float64(stats.TransferRate)/1024)
		fmt.Printf("  Failed chunks: %d\n", stats.FailedChunks)
		fmt.Printf("  Retry count: %d\n", stats.RetryCount)
		if stats.Error != "" {
			fmt.Printf("  Error: %s\n", stats.Error)
		}

		return nil
	},
}

// filePeersCmd represents the command to list peers with available files
var filePeersCmd = &cobra.Command{
	Use:   "peers",
	Short: "List peers with available files",
	Long:  `List peers with available files using the node's file transfer system.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Initialize logger
		logger, err := zap.NewProduction()
		if err != nil {
			return fmt.Errorf("failed to initialize logger: %w", err)
		}
		defer logger.Sync()

		// Get the active node
		n, err := getActiveNode(logger)
		if err != nil {
			return fmt.Errorf("failed to get active node: %w", err)
		}

		// Get peer files
		peerFiles := n.GetFileManager().GetAllPeerFiles()

		// Print peer files
		fmt.Println("Peers with available files:")
		if len(peerFiles) == 0 {
			fmt.Println("  No peers with files found")
			return nil
		}

		for peer, files := range peerFiles {
			fmt.Printf("  Peer %s:\n", peer)
			if len(files) == 0 {
				fmt.Println("    No files available")
				continue
			}

			for _, file := range files {
				fmt.Printf("    - %s\n", file)
			}
		}

		return nil
	},
}

// getActiveNode returns the active node or an error if no node is running
func getActiveNode(logger *zap.Logger) (*node.Node, error) {
	// TODO: Implement a way to get the active node
	// This could be done by checking a PID file, using a socket, or some other IPC mechanism
	return nil, fmt.Errorf("no active node found - please start a node first with 'furymesh node'")
}

func init() {
	fileCmd.AddCommand(fileRequestCmd)
	fileCmd.AddCommand(fileStatusCmd)
	fileCmd.AddCommand(filePeersCmd)

	// Add flags for file request command
	fileRequestCmd.Flags().StringVarP(&nodePeerID, "peer", "p", "", "ID of the peer to request the file from")
	fileRequestCmd.Flags().StringVarP(&nodeFileID, "file", "f", "", "ID of the file to request")
	fileRequestCmd.Flags().IntVarP(&nodeTimeout, "timeout", "t", 60, "Timeout in seconds for the request")
	fileRequestCmd.MarkFlagRequired("peer")
	fileRequestCmd.MarkFlagRequired("file")

	// Add flags for file status command
	fileStatusCmd.Flags().StringVarP(&nodeFileID, "file", "f", "", "ID of the file to check status for")
	fileStatusCmd.MarkFlagRequired("file")
}
