package file

import (
	"bytes"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestStorageManager(t *testing.T) {
	// Create a logger
	logger, _ := zap.NewDevelopment()

	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "storage-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a storage manager
	config := StorageConfig{
		BaseDir: tempDir,
	}
	sm, err := NewStorageManager(logger, config)
	if err != nil {
		t.Fatalf("Failed to create storage manager: %v", err)
	}

	// Test metadata storage and retrieval
	t.Run("MetadataStorage", func(t *testing.T) {
		// Create test metadata
		metadata := &ChunkMetadata{
			FileID:      "test-file-id",
			FileName:    "test-file.txt",
			FilePath:    "/path/to/test-file.txt",
			FileSize:    1024,
			ChunkSize:   256,
			TotalChunks: 4,
			ChunkHashes: []string{"hash1", "hash2", "hash3", "hash4"},
			FileHash:    "file-hash",
		}

		// Save metadata
		err := sm.SaveMetadata(metadata)
		if err != nil {
			t.Fatalf("Failed to save metadata: %v", err)
		}

		// Get metadata
		retrievedMetadata, err := sm.GetMetadata("test-file-id")
		if err != nil {
			t.Fatalf("Failed to get metadata: %v", err)
		}

		// Verify metadata
		if retrievedMetadata.FileID != metadata.FileID {
			t.Errorf("Wrong file ID: got %s, want %s", retrievedMetadata.FileID, metadata.FileID)
		}
		if retrievedMetadata.FileName != metadata.FileName {
			t.Errorf("Wrong file name: got %s, want %s", retrievedMetadata.FileName, metadata.FileName)
		}
		if retrievedMetadata.FileSize != metadata.FileSize {
			t.Errorf("Wrong file size: got %d, want %d", retrievedMetadata.FileSize, metadata.FileSize)
		}
		if retrievedMetadata.ChunkSize != metadata.ChunkSize {
			t.Errorf("Wrong chunk size: got %d, want %d", retrievedMetadata.ChunkSize, metadata.ChunkSize)
		}
		if retrievedMetadata.TotalChunks != metadata.TotalChunks {
			t.Errorf("Wrong total chunks: got %d, want %d", retrievedMetadata.TotalChunks, metadata.TotalChunks)
		}
		if len(retrievedMetadata.ChunkHashes) != len(metadata.ChunkHashes) {
			t.Errorf("Wrong number of chunk hashes: got %d, want %d", len(retrievedMetadata.ChunkHashes), len(metadata.ChunkHashes))
		}
		if retrievedMetadata.FileHash != metadata.FileHash {
			t.Errorf("Wrong file hash: got %s, want %s", retrievedMetadata.FileHash, metadata.FileHash)
		}

		// List metadata
		metadataList := sm.ListMetadata()
		if len(metadataList) != 1 {
			t.Errorf("Wrong number of metadata entries: got %d, want 1", len(metadataList))
		}
		if metadataList[0].FileID != metadata.FileID {
			t.Errorf("Wrong file ID in list: got %s, want %s", metadataList[0].FileID, metadata.FileID)
		}

		// Delete metadata
		err = sm.DeleteMetadata("test-file-id")
		if err != nil {
			t.Fatalf("Failed to delete metadata: %v", err)
		}

		// Verify metadata is deleted
		_, err = sm.GetMetadata("test-file-id")
		if err == nil {
			t.Error("Metadata still exists after deletion")
		}

		metadataList = sm.ListMetadata()
		if len(metadataList) != 0 {
			t.Errorf("Metadata still in list after deletion: got %d entries", len(metadataList))
		}
	})

	// Test chunk storage and retrieval
	t.Run("ChunkStorage", func(t *testing.T) {
		// Create test chunks
		fileID := "test-chunk-file"
		numChunks := 3
		chunkSize := 1024
		chunks := make([][]byte, numChunks)

		for i := 0; i < numChunks; i++ {
			chunks[i] = make([]byte, chunkSize)
			_, err := rand.Read(chunks[i])
			if err != nil {
				t.Fatalf("Failed to generate random chunk data: %v", err)
			}

			// Save chunk
			err = sm.SaveChunk(fileID, i, chunks[i])
			if err != nil {
				t.Fatalf("Failed to save chunk %d: %v", i, err)
			}
		}

		// Get chunks
		for i := 0; i < numChunks; i++ {
			retrievedChunk, err := sm.GetChunk(fileID, i)
			if err != nil {
				t.Fatalf("Failed to get chunk %d: %v", i, err)
			}

			// Verify chunk data
			if !bytes.Equal(retrievedChunk, chunks[i]) {
				t.Errorf("Chunk data does not match for chunk %d", i)
			}
		}

		// Delete a single chunk
		err = sm.DeleteChunk(fileID, 0)
		if err != nil {
			t.Fatalf("Failed to delete chunk: %v", err)
		}

		// Verify chunk is deleted
		_, err = sm.GetChunk(fileID, 0)
		if err == nil {
			t.Error("Chunk still exists after deletion")
		}

		// Delete all chunks
		err = sm.DeleteAllChunks(fileID)
		if err != nil {
			t.Fatalf("Failed to delete all chunks: %v", err)
		}

		// Verify all chunks are deleted
		for i := 0; i < numChunks; i++ {
			_, err = sm.GetChunk(fileID, i)
			if err == nil && i > 0 { // Chunk 0 was already deleted
				t.Errorf("Chunk %d still exists after deletion", i)
			}
		}
	})

	// Test storage stats
	t.Run("StorageStats", func(t *testing.T) {
		// Create test metadata and chunks
		metadata := &ChunkMetadata{
			FileID:      "stats-test-file",
			FileName:    "stats-test.txt",
			FileSize:    2048,
			ChunkSize:   1024,
			TotalChunks: 2,
			ChunkHashes: []string{"hash1", "hash2"},
			FileHash:    "file-hash",
		}

		// Save metadata
		err := sm.SaveMetadata(metadata)
		if err != nil {
			t.Fatalf("Failed to save metadata: %v", err)
		}

		// Save chunks
		for i := 0; i < 2; i++ {
			chunkData := make([]byte, 1024)
			_, err := rand.Read(chunkData)
			if err != nil {
				t.Fatalf("Failed to generate random chunk data: %v", err)
			}

			err = sm.SaveChunk("stats-test-file", i, chunkData)
			if err != nil {
				t.Fatalf("Failed to save chunk %d: %v", i, err)
			}
		}

		// Get storage stats
		stats, err := sm.GetStorageStats()
		if err != nil {
			t.Fatalf("Failed to get storage stats: %v", err)
		}

		// Verify stats
		if stats["file_count"].(int) != 1 {
			t.Errorf("Wrong total files: got %d, want 1", stats["file_count"].(int))
		}
		if stats["total_chunks"].(int) != 2 {
			t.Errorf("Wrong total chunks: got %d, want 2", stats["total_chunks"].(int))
		}
		if stats["total_size"].(int64) < 2048 {
			t.Errorf("Wrong total size: got %d, want at least 2048", stats["total_size"].(int64))
		}
	})

	// Test file expiration
	t.Run("FileExpiration", func(t *testing.T) {
		// Create test metadata with old timestamp
		oldMetadata := &ChunkMetadata{
			FileID:      "expired-file",
			FileName:    "expired.txt",
			FileSize:    1024,
			ChunkSize:   1024,
			TotalChunks: 1,
			ChunkHashes: []string{"hash"},
			FileHash:    "file-hash",
		}

		// Save metadata
		err := sm.SaveMetadata(oldMetadata)
		if err != nil {
			t.Fatalf("Failed to save metadata: %v", err)
		}

		// Manually set the file's modification time to 8 days ago
		metadataPath := filepath.Join(tempDir, "metadata", "expired-file.json")
		eightDaysAgo := time.Now().Add(-8 * 24 * time.Hour)
		err = os.Chtimes(metadataPath, eightDaysAgo, eightDaysAgo)
		if err != nil {
			t.Fatalf("Failed to change file time: %v", err)
		}

		// Create a chunk for the expired file
		chunkData := make([]byte, 1024)
		_, err = rand.Read(chunkData)
		if err != nil {
			t.Fatalf("Failed to generate random chunk data: %v", err)
		}

		err = sm.SaveChunk("expired-file", 0, chunkData)
		if err != nil {
			t.Fatalf("Failed to save chunk: %v", err)
		}

		// Set the chunk's modification time to 8 days ago
		chunkPath := filepath.Join(tempDir, "chunks", "expired-file", "0.chunk")
		err = os.Chtimes(chunkPath, eightDaysAgo, eightDaysAgo)
		if err != nil {
			t.Fatalf("Failed to change chunk time: %v", err)
		}

		// Create test metadata with recent timestamp
		recentMetadata := &ChunkMetadata{
			FileID:      "recent-file",
			FileName:    "recent.txt",
			FileSize:    1024,
			ChunkSize:   1024,
			TotalChunks: 1,
			ChunkHashes: []string{"hash"},
			FileHash:    "file-hash",
		}

		// Save metadata
		err = sm.SaveMetadata(recentMetadata)
		if err != nil {
			t.Fatalf("Failed to save metadata: %v", err)
		}

		// Clean up expired files (older than 7 days)
		err = sm.CleanupExpiredFiles(7 * 24 * time.Hour)
		if err != nil {
			t.Fatalf("Failed to clean up expired files: %v", err)
		}

		// Verify expired file is gone
		_, err = sm.GetMetadata("expired-file")
		if err == nil {
			t.Error("Expired file metadata still exists after cleanup")
		}

		_, err = sm.GetChunk("expired-file", 0)
		if err == nil {
			t.Error("Expired file chunk still exists after cleanup")
		}

		// Verify recent file is still there
		_, err = sm.GetMetadata("recent-file")
		if err != nil {
			t.Errorf("Recent file metadata was deleted: %v", err)
		}
	})
}
