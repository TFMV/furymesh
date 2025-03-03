package node

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/pion/webrtc/v3"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

// Node represents a FuryMesh P2P node
type Node struct {
	logger      *zap.Logger
	fileManager *FileManager
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
}

// NewNode creates a new Node instance
func NewNode(logger *zap.Logger, fileManager *FileManager) (*Node, error) {
	ctx, cancel := context.WithCancel(context.Background())

	return &Node{
		logger:      logger,
		fileManager: fileManager,
		ctx:         ctx,
		cancel:      cancel,
	}, nil
}

// Start starts the node
func (n *Node) Start() error {
	n.logger.Info("Starting FuryMesh node")

	// Set up signal handling for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Start a goroutine to handle signals
	n.wg.Add(1)
	go func() {
		defer n.wg.Done()
		select {
		case <-sigCh:
			n.logger.Info("Received shutdown signal")
			n.cancel()
		case <-n.ctx.Done():
			return
		}
	}()

	// Start the node's main loop
	n.wg.Add(1)
	go func() {
		defer n.wg.Done()
		n.run()
	}()

	return nil
}

// run is the main loop of the node
func (n *Node) run() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-n.ctx.Done():
			n.logger.Info("Node shutting down")
			return
		case <-ticker.C:
			// Periodic maintenance tasks
			n.logger.Debug("Performing periodic maintenance")
		}
	}
}

// Stop stops the node
func (n *Node) Stop() {
	n.logger.Info("Stopping node")
	n.cancel()
	n.Wait()
}

// Wait waits for the node to exit
func (n *Node) Wait() {
	n.wg.Wait()
	n.logger.Info("Node stopped")
}

// LegacyStartNode starts a FuryMesh node (legacy function)
func LegacyStartNode(logger *zap.Logger) {
	// Create a file manager
	fileManager, err := NewFileManager(logger)
	if err != nil {
		logger.Error("Failed to create file manager", zap.Error(err))
		return
	}

	// Start the file manager
	fileManager.Start()
	defer fileManager.Stop()

	// Create a node
	node, err := NewNode(logger, fileManager)
	if err != nil {
		logger.Error("Failed to create node", zap.Error(err))
		return
	}

	// Start the node
	if err := node.Start(); err != nil {
		logger.Error("Failed to start node", zap.Error(err))
		return
	}

	// Wait for the node to exit
	node.Wait()
}

// StartNode initializes and starts a WebRTC peer node using Pion.
// It reads STUN server addresses from configuration, creates a DataChannel,
// and sets up a message handler for incoming data.
func StartNode(logger *zap.Logger) {
	// Retrieve STUN servers from Viper config.
	stunServers := viper.GetStringSlice("webrtc.stun_servers")
	var iceServers []webrtc.ICEServer
	for _, server := range stunServers {
		iceServers = append(iceServers, webrtc.ICEServer{URLs: []string{server}})
	}

	config := webrtc.Configuration{
		ICEServers: iceServers,
	}

	peerConnection, err := webrtc.NewPeerConnection(config)
	if err != nil {
		logger.Fatal("Failed to create WebRTC peer", zap.Error(err))
	}

	// Create a DataChannel for data exchange.
	dataChannel, err := peerConnection.CreateDataChannel("fury-data", nil)
	if err != nil {
		logger.Fatal("Failed to create DataChannel", zap.Error(err))
	}

	// Set a handler for incoming messages on the DataChannel.
	dataChannel.OnMessage(func(msg webrtc.DataChannelMessage) {
		logger.Info("Received message", zap.ByteString("data", msg.Data))
		fmt.Println("Received:", string(msg.Data))
	})

	logger.Info("FuryMesh node is running. Press CTRL+C to exit.")
	// Block forever to keep the node running.
	select {}
}

// GetFileManager returns the node's file manager
func (n *Node) GetFileManager() *FileManager {
	return n.fileManager
}
