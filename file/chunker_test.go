package file

import (
	"bytes"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/zap"
)

func TestChunker(t *testing.T) {
	// Create a logger
	logger, _ := zap.NewDevelopment()

	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "chunker-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a chunker with a small chunk size for testing
	chunkSize := 1024 // 1KB
	chunker, err := NewChunker(logger, tempDir, chunkSize)
	if err != nil {
		t.Fatalf("Failed to create chunker: %v", err)
	}

	// Create a test file
	testFilePath := filepath.Join(tempDir, "test-file.bin")
	testFileSize := chunkSize * 5 // 5KB
	testFileData := make([]byte, testFileSize)
	_, err = rand.Read(testFileData) // Fill with random data
	if err != nil {
		t.Fatalf("Failed to generate random data: %v", err)
	}
	err = os.WriteFile(testFilePath, testFileData, 0644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Test chunking
	t.Run("ChunkFile", func(t *testing.T) {
		metadata, err := chunker.ChunkFile(testFilePath)
		if err != nil {
			t.Fatalf("Failed to chunk file: %v", err)
		}

		// Verify metadata
		if metadata.FileID == "" {
			t.Error("FileID is empty")
		}
		if metadata.FileName != "test-file.bin" {
			t.Errorf("Wrong file name: got %s, want test-file.bin", metadata.FileName)
		}
		if metadata.FileSize != int64(testFileSize) {
			t.Errorf("Wrong file size: got %d, want %d", metadata.FileSize, testFileSize)
		}
		if metadata.ChunkSize != chunkSize {
			t.Errorf("Wrong chunk size: got %d, want %d", metadata.ChunkSize, chunkSize)
		}
		if metadata.TotalChunks != 5 {
			t.Errorf("Wrong total chunks: got %d, want 5", metadata.TotalChunks)
		}
		if len(metadata.ChunkHashes) != 5 {
			t.Errorf("Wrong number of chunk hashes: got %d, want 5", len(metadata.ChunkHashes))
		}
		if metadata.FileHash == "" {
			t.Error("FileHash is empty")
		}

		// Test GetChunk
		for i := 0; i < metadata.TotalChunks; i++ {
			chunk, err := chunker.GetChunk(metadata.FileID, i)
			if err != nil {
				t.Fatalf("Failed to get chunk %d: %v", i, err)
			}
			if chunk.ID != metadata.FileID {
				t.Errorf("Wrong chunk ID: got %s, want %s", chunk.ID, metadata.FileID)
			}
			if int(chunk.Index) != i {
				t.Errorf("Wrong chunk index: got %d, want %d", chunk.Index, i)
			}
			if int(chunk.TotalChunks) != metadata.TotalChunks {
				t.Errorf("Wrong total chunks: got %d, want %d", chunk.TotalChunks, metadata.TotalChunks)
			}

			// Verify chunk size
			expectedSize := chunkSize
			if i == metadata.TotalChunks-1 && testFileSize%chunkSize != 0 {
				expectedSize = testFileSize % chunkSize
			}
			if len(chunk.Data) != expectedSize {
				t.Errorf("Wrong chunk size for chunk %d: got %d, want %d", i, len(chunk.Data), expectedSize)
			}

			// Verify chunk data
			expectedData := testFileData[i*chunkSize : i*chunkSize+len(chunk.Data)]
			if !bytes.Equal(chunk.Data, expectedData) {
				t.Errorf("Chunk data does not match for chunk %d", i)
			}
		}

		// Test GetFileMetadata
		retrievedMetadata, err := chunker.GetFileMetadata(metadata.FileID)
		if err != nil {
			t.Fatalf("Failed to get file metadata: %v", err)
		}
		if retrievedMetadata.FileID != metadata.FileID {
			t.Errorf("Wrong file ID in retrieved metadata: got %s, want %s", retrievedMetadata.FileID, metadata.FileID)
		}

		// Test ListFiles
		files := chunker.ListFiles()
		if len(files) != 1 {
			t.Errorf("Wrong number of files: got %d, want 1", len(files))
		}
		if files[0].FileID != metadata.FileID {
			t.Errorf("Wrong file ID in list: got %s, want %s", files[0].FileID, metadata.FileID)
		}

		// Test ReassembleFile
		outputPath := filepath.Join(tempDir, "reassembled-file.bin")
		err = chunker.ReassembleFile(metadata.FileID, outputPath)
		if err != nil {
			t.Fatalf("Failed to reassemble file: %v", err)
		}

		// Verify reassembled file
		reassembledData, err := os.ReadFile(outputPath)
		if err != nil {
			t.Fatalf("Failed to read reassembled file: %v", err)
		}
		if !bytes.Equal(reassembledData, testFileData) {
			t.Error("Reassembled file does not match original")
		}

		// Test DeleteFile
		err = chunker.DeleteFile(metadata.FileID)
		if err != nil {
			t.Fatalf("Failed to delete file: %v", err)
		}

		// Verify file is deleted
		_, err = chunker.GetFileMetadata(metadata.FileID)
		if err == nil {
			t.Error("File metadata still exists after deletion")
		}

		files = chunker.ListFiles()
		if len(files) != 0 {
			t.Errorf("File still in list after deletion: got %d files", len(files))
		}
	})
}
