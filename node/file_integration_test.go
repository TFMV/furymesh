package node

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/viper"
	"go.uber.org/zap"
)

func TestFileManager(t *testing.T) {
	// Create a logger
	logger, _ := zap.NewDevelopment()

	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "file-manager-test")
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

	// Create a file manager
	fm, err := NewFileManager(logger)
	if err != nil {
		t.Fatalf("Failed to create file manager: %v", err)
	}

	// Start the file manager
	fm.Start()
	defer fm.Stop()

	// Test peer connection handling
	t.Run("PeerConnectionHandling", func(t *testing.T) {
		// Test peer connected
		peerID := "test-peer"
		fm.handlePeerConnected(peerID)

		// Verify peer files map was initialized
		fm.peerFilesMu.RLock()
		_, exists := fm.peerFiles[peerID]
		fm.peerFilesMu.RUnlock()
		if !exists {
			t.Error("Peer files map not initialized on peer connection")
		}

		// Test peer disconnected
		fm.handlePeerDisconnected(peerID)

		// Verify peer files map was cleaned up
		fm.peerFilesMu.RLock()
		_, exists = fm.peerFiles[peerID]
		fm.peerFilesMu.RUnlock()
		if exists {
			t.Error("Peer files map not cleaned up on peer disconnection")
		}
	})

	// Test peer files management
	t.Run("PeerFilesManagement", func(t *testing.T) {
		peerID := "test-peer"
		files := []string{"file1", "file2", "file3"}

		// Update peer files
		fm.updatePeerFiles(peerID, files)

		// Get peer files
		peerFiles := fm.GetPeerFiles(peerID)
		if len(peerFiles) != len(files) {
			t.Errorf("Wrong number of peer files: got %d, want %d", len(peerFiles), len(files))
		}
		for i, file := range files {
			if peerFiles[i] != file {
				t.Errorf("Wrong file at index %d: got %s, want %s", i, peerFiles[i], file)
			}
		}

		// Get all peer files
		allPeerFiles := fm.GetAllPeerFiles()
		if len(allPeerFiles) != 1 {
			t.Errorf("Wrong number of peers: got %d, want 1", len(allPeerFiles))
		}
		if _, exists := allPeerFiles[peerID]; !exists {
			t.Errorf("Peer %s not found in all peer files", peerID)
		}
		if len(allPeerFiles[peerID]) != len(files) {
			t.Errorf("Wrong number of files for peer %s: got %d, want %d", peerID, len(allPeerFiles[peerID]), len(files))
		}
	})

	// Test file operations (limited test without actual file I/O)
	t.Run("FileOperations", func(t *testing.T) {
		// Create a test file
		testFilePath := filepath.Join(tempDir, "test-file.txt")
		testData := []byte("This is a test file for FuryMesh file manager")
		err := os.WriteFile(testFilePath, testData, 0644)
		if err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}

		// Test chunking a file
		metadata, err := fm.ChunkFile(testFilePath)
		if err != nil {
			t.Fatalf("Failed to chunk file: %v", err)
		}
		if metadata.FileID == "" {
			t.Error("File ID is empty")
		}
		if metadata.FileName != "test-file.txt" {
			t.Errorf("Wrong file name: got %s, want test-file.txt", metadata.FileName)
		}
		if metadata.FileSize != int64(len(testData)) {
			t.Errorf("Wrong file size: got %d, want %d", metadata.FileSize, len(testData))
		}

		// Test listing available files
		files := fm.ListAvailableFiles()
		if len(files) != 1 {
			t.Errorf("Wrong number of available files: got %d, want 1", len(files))
		}
		if files[0].FileID != metadata.FileID {
			t.Errorf("Wrong file ID in list: got %s, want %s", files[0].FileID, metadata.FileID)
		}

		// Test reassembling a file
		outputPath := filepath.Join(tempDir, "reassembled-file.txt")
		err = fm.ReassembleFile(metadata.FileID, outputPath)
		if err != nil {
			t.Fatalf("Failed to reassemble file: %v", err)
		}

		// Verify reassembled file
		reassembledData, err := os.ReadFile(outputPath)
		if err != nil {
			t.Fatalf("Failed to read reassembled file: %v", err)
		}
		if string(reassembledData) != string(testData) {
			t.Error("Reassembled file does not match original")
		}

		// Test deleting a file
		err = fm.DeleteFile(metadata.FileID)
		if err != nil {
			t.Fatalf("Failed to delete file: %v", err)
		}

		// Verify file is deleted
		files = fm.ListAvailableFiles()
		if len(files) != 0 {
			t.Errorf("File still in list after deletion: got %d files", len(files))
		}
	})

	// Test storage stats
	t.Run("StorageStats", func(t *testing.T) {
		stats, err := fm.GetStorageStats()
		if err != nil {
			t.Fatalf("Failed to get storage stats: %v", err)
		}
		if stats == nil {
			t.Fatal("Storage stats is nil")
		}
		if _, exists := stats["file_count"]; !exists {
			t.Error("file_count not found in storage stats")
		}
		if _, exists := stats["total_size"]; !exists {
			t.Error("total_size not found in storage stats")
		}
	})

	// Test requesting a file from a peer (limited test without actual network communication)
	t.Run("RequestFileFromPeer", func(t *testing.T) {
		peerID := "test-peer"
		fileID := "test-file"

		// This will fail because the file doesn't exist locally
		err := fm.RequestFileFromPeer(context.Background(), peerID, fileID)
		if err == nil {
			t.Error("Expected error when requesting non-existent file")
		}
	})
}
