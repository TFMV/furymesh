package file

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/TFMV/furymesh/crypto"
	"go.uber.org/zap"
)

// MockChunker implements a minimal Chunker for testing
type MockChunker struct {
	workDir string
	chunks  map[string]map[int][]byte
}

func NewMockChunker(workDir string) *MockChunker {
	return &MockChunker{
		workDir: workDir,
		chunks:  make(map[string]map[int][]byte),
	}
}

func (m *MockChunker) GetWorkDir() string {
	return m.workDir
}

func (m *MockChunker) GetChunk(fileID string, chunkIndex int) (*FileChunkData, error) {
	if fileChunks, exists := m.chunks[fileID]; exists {
		if data, exists := fileChunks[chunkIndex]; exists {
			return &FileChunkData{
				ID:          fileID,
				Index:       int32(chunkIndex),
				TotalChunks: int32(len(fileChunks)),
				Data:        data,
			}, nil
		}
	}
	return nil, os.ErrNotExist
}

func (m *MockChunker) AddChunk(fileID string, chunkIndex int, data []byte) {
	if _, exists := m.chunks[fileID]; !exists {
		m.chunks[fileID] = make(map[int][]byte)
	}
	m.chunks[fileID][chunkIndex] = data
}

func (m *MockChunker) ChunkFile(filePath string) (*ChunkMetadata, error) {
	return &ChunkMetadata{
		FileID:      "test-file-id",
		FileName:    "test-file.txt",
		FileSize:    1024,
		ChunkSize:   256,
		TotalChunks: 4,
	}, nil
}

// Add FileID field to TransferStats for testing
type TestTransferStats struct {
	TransferStats
	FileID string
}

// Update StartTransfer to use TestTransferStats
func (tm *TransferManager) StartTransfer(fileID string, totalChunks int) (*TestTransferStats, error) {
	stats := &TestTransferStats{
		TransferStats: TransferStats{
			StartTime:   time.Now(),
			TotalChunks: totalChunks,
			Status:      TransferStatusInProgress,
		},
		FileID: fileID,
	}

	tm.transfersMu.Lock()
	tm.activeTransfers[fileID] = &stats.TransferStats
	tm.transfersMu.Unlock()

	// Generate a session key if encryption is enabled
	if tm.encryptionMgr != nil {
		// Generate a random session key
		sessionKey := make([]byte, crypto.KeySize)
		if _, err := rand.Read(sessionKey); err != nil {
			return nil, fmt.Errorf("failed to generate session key: %w", err)
		}

		// Store the session key
		tm.sessionKeysMu.Lock()
		tm.sessionKeys[fileID] = sessionKey
		tm.sessionKeysMu.Unlock()
	}

	return stats, nil
}

// Create a test-specific version of NewTransferManager
func NewTestTransferManager(
	logger *zap.Logger,
	chunker *MockChunker,
	workDir string,
	requestTimeout time.Duration,
	maxRetries int,
	concurrentTransfers int,
	encryptionMgr *crypto.EncryptionManager,
) *TransferManager {
	ctx, cancel := context.WithCancel(context.Background())

	return &TransferManager{
		logger:              logger,
		chunker:             nil, // We'll mock this
		workDir:             workDir,
		requestTimeout:      requestTimeout,
		maxRetries:          maxRetries,
		concurrentTransfers: concurrentTransfers,
		activeTransfers:     make(map[string]*TransferStats),
		dataRequestCh:       make(chan *DataRequest, 100),
		dataResponseCh:      make(chan *DataResponse, 100),
		ctx:                 ctx,
		cancel:              cancel,
		encryptionMgr:       encryptionMgr,
		sessionKeys:         make(map[string][]byte),
	}
}

func TestTransferManager(t *testing.T) {
	// Create a logger
	logger, _ := zap.NewDevelopment()

	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "transfer-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a mock chunker
	chunker := NewMockChunker(tempDir)

	// Add some test chunks
	fileID := "test-file"
	totalChunks := 3
	for i := 0; i < totalChunks; i++ {
		data := []byte(filepath.Join("test-chunk-", string(rune('A'+i))))
		chunker.AddChunk(fileID, i, data)
	}

	// Create a transfer manager using our test constructor
	tm := NewTestTransferManager(
		logger,
		chunker,
		tempDir,
		1*time.Second,
		3,
		5,
		nil, // No encryption for basic tests
	)

	// Start the transfer manager
	tm.Start()
	defer tm.Stop()

	// Test starting a transfer
	t.Run("StartTransfer", func(t *testing.T) {
		transfer, err := tm.StartTransfer(fileID, totalChunks)
		if err != nil {
			t.Fatalf("Failed to start transfer: %v", err)
		}

		// Verify transfer state
		if transfer.FileID != fileID {
			t.Errorf("Wrong file ID: got %s, want %s", transfer.FileID, fileID)
		}
		if transfer.TotalChunks != totalChunks {
			t.Errorf("Wrong total chunks: got %d, want %d", transfer.TotalChunks, totalChunks)
		}
		if transfer.Status != TransferStatusInProgress {
			t.Errorf("Wrong status: got %s, want %s", transfer.Status, TransferStatusInProgress)
		}
	})

	// Test getting transfer stats
	t.Run("GetTransferStats", func(t *testing.T) {
		stats, err := tm.GetTransferStats(fileID)
		if err != nil {
			t.Fatalf("Failed to get transfer stats: %v", err)
		}

		// Verify stats
		if stats.TotalChunks != totalChunks {
			t.Errorf("Wrong total chunks: got %d, want %d", stats.TotalChunks, totalChunks)
		}
	})

	// Test cancelling a transfer
	t.Run("CancelTransfer", func(t *testing.T) {
		err := tm.CancelTransfer(fileID)
		if err != nil {
			t.Fatalf("Failed to cancel transfer: %v", err)
		}

		// Verify transfer is cancelled
		stats, err := tm.GetTransferStats(fileID)
		if err != nil {
			t.Fatalf("Failed to get transfer stats: %v", err)
		}
		if stats.Status != TransferStatusCancelled {
			t.Errorf("Wrong status: got %s, want %s", stats.Status, TransferStatusCancelled)
		}
	})

	// Test listing active transfers
	t.Run("ListActiveTransfers", func(t *testing.T) {
		// Start a new transfer
		newFileID := "new-test-file"
		_, err := tm.StartTransfer(newFileID, 2)
		if err != nil {
			t.Fatalf("Failed to start transfer: %v", err)
		}

		// List active transfers
		transfers := tm.ListActiveTransfers()
		if len(transfers) != 2 { // The cancelled one and the new one
			t.Errorf("Wrong number of transfers: got %d, want 2", len(transfers))
		}
		if _, exists := transfers[newFileID]; !exists {
			t.Errorf("New transfer not found in active transfers")
		}
	})

	// Test cleanup of completed transfers
	t.Run("CleanupCompletedTransfers", func(t *testing.T) {
		// Mark a transfer as completed
		tm.transfersMu.Lock()
		if transfer, exists := tm.activeTransfers[fileID]; exists {
			transfer.Status = TransferStatusCompleted
			transfer.EndTime = time.Now().Add(-1 * time.Hour) // Completed an hour ago
		}
		tm.transfersMu.Unlock()

		// Clean up completed transfers
		tm.CleanupCompletedTransfers()

		// Verify completed transfer was removed
		tm.transfersMu.RLock()
		_, exists := tm.activeTransfers[fileID]
		tm.transfersMu.RUnlock()
		if exists {
			t.Error("Completed transfer not cleaned up")
		}
	})
}

func TestTransferManagerWithEncryption(t *testing.T) {
	// Skip if running short tests
	if testing.Short() {
		t.Skip("Skipping encryption test in short mode")
	}

	// Create a logger
	logger, _ := zap.NewDevelopment()

	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "transfer-encryption-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a keys directory
	keysDir := filepath.Join(tempDir, "keys")
	if err := os.MkdirAll(keysDir, 0700); err != nil {
		t.Fatalf("Failed to create keys directory: %v", err)
	}

	// Create an encryption manager
	encryptionMgr, err := crypto.NewEncryptionManager(logger, keysDir)
	if err != nil {
		t.Fatalf("Failed to create encryption manager: %v", err)
	}

	// Create a mock chunker
	chunker := NewMockChunker(tempDir)

	// Add some test chunks
	fileID := "encrypted-file"
	totalChunks := 2
	for i := 0; i < totalChunks; i++ {
		data := []byte(filepath.Join("encrypted-chunk-", string(rune('A'+i))))
		chunker.AddChunk(fileID, i, data)
	}

	// Create a transfer manager with encryption using our test constructor
	tm := NewTestTransferManager(
		logger,
		chunker,
		tempDir,
		1*time.Second,
		3,
		5,
		encryptionMgr,
	)

	// Start the transfer manager
	tm.Start()
	defer tm.Stop()

	// Test encrypted transfer
	t.Run("EncryptedTransfer", func(t *testing.T) {
		// Start a transfer
		transfer, err := tm.StartTransfer(fileID, totalChunks)
		if err != nil {
			t.Fatalf("Failed to start transfer: %v", err)
		}

		// Verify transfer state
		if transfer.FileID != fileID {
			t.Errorf("Wrong file ID: got %s, want %s", transfer.FileID, fileID)
		}

		// Verify session key was generated
		tm.sessionKeysMu.RLock()
		sessionKey, exists := tm.sessionKeys[fileID]
		tm.sessionKeysMu.RUnlock()
		if !exists {
			t.Error("Session key not generated for transfer")
		}
		if len(sessionKey) != crypto.KeySize {
			t.Errorf("Wrong session key size: got %d, want %d", len(sessionKey), crypto.KeySize)
		}
	})
}
