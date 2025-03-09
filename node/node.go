package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/TFMV/furymesh/dht"
	"github.com/TFMV/furymesh/file"
	"github.com/pion/webrtc/v3"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

// Node represents a FuryMesh P2P node
type Node struct {
	logger       *zap.Logger
	fileManager  *FileManager
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	dhtNode      *dht.KademliaNode
	dhtTransport *dht.TCPTransport
}

// NewNode creates a new Node instance
func NewNode(logger *zap.Logger, fileManager *FileManager) (*Node, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// Create a new node
	node := &Node{
		logger:      logger,
		fileManager: fileManager,
		ctx:         ctx,
		cancel:      cancel,
	}

	// Initialize DHT if enabled in config
	if viper.GetBool("dht.enabled") {
		// Get DHT configuration
		address := viper.GetString("dht.address")
		if address == "" {
			address = "0.0.0.0" // Default to all interfaces
		}

		port := viper.GetInt("dht.port")
		if port == 0 {
			port = 8000 // Default port
		}

		// Get bootstrap nodes
		bootstrapNodesConfig := viper.GetStringSlice("dht.bootstrap_nodes")
		var bootstrapNodes []*dht.Contact

		for _, nodeAddr := range bootstrapNodesConfig {
			parts := strings.Split(nodeAddr, ":")
			if len(parts) != 2 {
				logger.Warn("Invalid bootstrap node address", zap.String("address", nodeAddr))
				continue
			}

			host := parts[0]
			port, err := strconv.Atoi(parts[1])
			if err != nil {
				logger.Warn("Invalid port in bootstrap node address",
					zap.String("address", nodeAddr),
					zap.Error(err))
				continue
			}

			// Create a node ID based on the address
			id := dht.NewNodeID(nodeAddr)
			contact := dht.NewContact(id, host, port)
			bootstrapNodes = append(bootstrapNodes, contact)
		}

		// Create the DHT node
		dhtNode, err := dht.NewKademliaNode(logger, address, port, bootstrapNodes)
		if err != nil {
			return nil, fmt.Errorf("failed to create DHT node: %w", err)
		}

		// Create the DHT transport
		dhtTransport := dht.NewTCPTransport(dhtNode, logger)

		// Set the transport on the DHT node
		dhtNode.RPCClient = dhtTransport

		node.dhtNode = dhtNode
		node.dhtTransport = dhtTransport

		logger.Info("DHT node initialized",
			zap.String("address", address),
			zap.Int("port", port),
			zap.Int("bootstrap_nodes", len(bootstrapNodes)))
	}

	return node, nil
}

// Start starts the node
func (n *Node) Start() error {
	n.logger.Info("Starting FuryMesh node")

	// Start the DHT node if enabled
	if n.dhtNode != nil {
		n.logger.Info("Starting DHT node")

		// Bootstrap the DHT node
		if err := n.dhtNode.Bootstrap(); err != nil {
			n.logger.Warn("Failed to bootstrap DHT node", zap.Error(err))
			// Continue anyway, as we might still be able to operate
		}
	}

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

			// Announce available files to the DHT if enabled
			if n.dhtNode != nil {
				n.announceFilesToDHT()
			}
		}
	}
}

// announceFilesToDHT announces available files to the DHT
func (n *Node) announceFilesToDHT() {
	// Get available files
	files := n.fileManager.ListAvailableFiles()

	for _, metadata := range files {
		// Create a key for the file
		key := []byte("file:" + metadata.FileID)

		// Create a value with file metadata
		value, err := json.Marshal(metadata)
		if err != nil {
			n.logger.Error("Failed to marshal file metadata",
				zap.String("file_id", metadata.FileID),
				zap.Error(err))
			continue
		}

		// Store the file metadata in the DHT
		if err := n.dhtNode.Store(key, value); err != nil {
			n.logger.Error("Failed to announce file to DHT",
				zap.String("file_id", metadata.FileID),
				zap.Error(err))
			continue
		}

		n.logger.Debug("Announced file to DHT",
			zap.String("file_id", metadata.FileID))
	}
}

// FindFileInDHT finds a file in the DHT by its ID
func (n *Node) FindFileInDHT(fileID string) (*file.ChunkMetadata, error) {
	if n.dhtNode == nil {
		return nil, errors.New("DHT is not enabled")
	}

	// Create a key for the file
	key := []byte("file:" + fileID)

	// Find the file in the DHT
	value, err := n.dhtNode.FindValue(key)
	if err != nil {
		return nil, fmt.Errorf("failed to find file in DHT: %w", err)
	}

	// Unmarshal the file metadata
	var metadata file.ChunkMetadata
	if err := json.Unmarshal(value, &metadata); err != nil {
		return nil, fmt.Errorf("failed to unmarshal file metadata: %w", err)
	}

	return &metadata, nil
}

// FindPeersInDHT finds peers in the DHT
func (n *Node) FindPeersInDHT(targetID string) ([]*dht.Contact, error) {
	if n.dhtNode == nil {
		return nil, errors.New("DHT is not enabled")
	}

	// Create a target ID
	target := dht.NewNodeID(targetID)

	// Find nodes in the DHT
	contacts, err := n.dhtNode.FindNode(target)
	if err != nil {
		return nil, fmt.Errorf("failed to find peers in DHT: %w", err)
	}

	return contacts, nil
}

// Stop stops the node
func (n *Node) Stop() {
	n.logger.Info("Stopping node")

	// Stop the DHT node if enabled
	if n.dhtNode != nil {
		n.logger.Info("Stopping DHT node")
		n.dhtNode.Stop()
	}

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
