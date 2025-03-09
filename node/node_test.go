package node

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/viper"
	"go.uber.org/zap"
)

func TestNode(t *testing.T) {
	// Create a logger
	logger, _ := zap.NewDevelopment()

	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "node-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Set up viper configuration for testing
	viper.Reset()
	viper.Set("storage.work_dir", filepath.Join(tempDir, "work"))
	viper.Set("storage.base_dir", tempDir)
	viper.Set("storage.chunk_size", 1024)
	viper.Set("transfer.request_timeout", 1*time.Second)
	viper.Set("transfer.max_retries", 3)
	viper.Set("transfer.concurrent_transfers", 5)
	viper.Set("webrtc.stun_servers", []string{"stun:stun.l.google.com:19302"})
	viper.Set("encryption.enabled", false)
	viper.Set("dht.enabled", false) // Disable DHT for basic tests

	// Create a file manager
	fileManager, err := NewFileManager(logger)
	if err != nil {
		t.Fatalf("Failed to create file manager: %v", err)
	}

	// Create a node
	node, err := NewNode(logger, fileManager)
	if err != nil {
		t.Fatalf("Failed to create node: %v", err)
	}

	// Test node creation
	t.Run("NodeCreation", func(t *testing.T) {
		if node == nil {
			t.Fatal("Node is nil")
		}
		if node.fileManager != fileManager {
			t.Error("File manager not set correctly")
		}
		if node.logger != logger {
			t.Error("Logger not set correctly")
		}
		if node.ctx == nil {
			t.Error("Context not initialized")
		}
		if node.cancel == nil {
			t.Error("Cancel function not initialized")
		}
		if node.dhtNode != nil {
			t.Error("DHT node should be nil when DHT is disabled")
		}
	})

	// Test node start and stop
	t.Run("NodeStartStop", func(t *testing.T) {
		// Start the node
		err := node.Start()
		if err != nil {
			t.Fatalf("Failed to start node: %v", err)
		}

		// Wait a bit for the node to initialize
		time.Sleep(100 * time.Millisecond)

		// Stop the node
		node.Stop()

		// Wait for the node to stop
		done := make(chan struct{})
		go func() {
			node.Wait()
			close(done)
		}()

		select {
		case <-done:
			// Node stopped successfully
		case <-time.After(1 * time.Second):
			t.Fatal("Node did not stop within timeout")
		}
	})

	// Test file manager access
	t.Run("FileManagerAccess", func(t *testing.T) {
		fm := node.GetFileManager()
		if fm == nil {
			t.Fatal("GetFileManager returned nil")
		}
		if fm != fileManager {
			t.Error("GetFileManager returned wrong file manager")
		}
	})
}

func TestNodeWithDHT(t *testing.T) {
	// Skip in short mode as DHT tests can be slow
	if testing.Short() {
		t.Skip("Skipping DHT test in short mode")
	}

	// Create a logger
	logger, _ := zap.NewDevelopment()

	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "node-dht-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Set up viper configuration for testing
	viper.Reset()
	viper.Set("storage.work_dir", filepath.Join(tempDir, "work"))
	viper.Set("storage.base_dir", tempDir)
	viper.Set("storage.chunk_size", 1024)
	viper.Set("transfer.request_timeout", 1*time.Second)
	viper.Set("transfer.max_retries", 3)
	viper.Set("transfer.concurrent_transfers", 5)
	viper.Set("webrtc.stun_servers", []string{"stun:stun.l.google.com:19302"})
	viper.Set("encryption.enabled", false)
	viper.Set("dht.enabled", true)
	viper.Set("dht.address", "127.0.0.1")
	viper.Set("dht.port", 8000)
	viper.Set("dht.bootstrap_nodes", []string{})

	// Create a file manager
	fileManager, err := NewFileManager(logger)
	if err != nil {
		t.Fatalf("Failed to create file manager: %v", err)
	}

	// Create a node with DHT enabled
	node, err := NewNode(logger, fileManager)
	if err != nil {
		t.Fatalf("Failed to create node: %v", err)
	}

	// Test DHT initialization
	t.Run("DHTInitialization", func(t *testing.T) {
		if node.dhtNode == nil {
			t.Fatal("DHT node is nil when DHT is enabled")
		}
		if node.dhtTransport == nil {
			t.Fatal("DHT transport is nil when DHT is enabled")
		}
	})

	// Test node start and stop with DHT
	t.Run("NodeStartStopWithDHT", func(t *testing.T) {
		// Start the node
		err := node.Start()
		if err != nil {
			t.Fatalf("Failed to start node: %v", err)
		}

		// Wait a bit for the node to initialize
		time.Sleep(100 * time.Millisecond)

		// Stop the node
		node.Stop()

		// Wait for the node to stop
		done := make(chan struct{})
		go func() {
			node.Wait()
			close(done)
		}()

		select {
		case <-done:
			// Node stopped successfully
		case <-time.After(1 * time.Second):
			t.Fatal("Node did not stop within timeout")
		}
	})

	// Test DHT file operations (limited test without actual network communication)
	t.Run("DHTFileOperations", func(t *testing.T) {
		// This will fail because we don't have a real DHT network
		_, err := node.FindFileInDHT("test-file")
		if err == nil {
			t.Error("Expected error when finding file in DHT without a network")
		}

		// This will fail because we don't have a real DHT network
		_, err = node.FindPeersInDHT("test-peer")
		if err == nil {
			t.Error("Expected error when finding peers in DHT without a network")
		}
	})
}
