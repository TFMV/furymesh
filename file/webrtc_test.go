package file

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/pion/webrtc/v3"
	"go.uber.org/zap"
)

// MockTransferManager implements a minimal TransferManager for testing
type MockTransferManager struct {
	logger         *zap.Logger
	dataRequestCh  chan *DataRequest
	dataResponseCh chan *DataResponse
	transfers      map[string]*TransferStats
}

func NewMockTransferManager(logger *zap.Logger) *MockTransferManager {
	return &MockTransferManager{
		logger:         logger,
		dataRequestCh:  make(chan *DataRequest, 10),
		dataResponseCh: make(chan *DataResponse, 10),
		transfers:      make(map[string]*TransferStats),
	}
}

func (m *MockTransferManager) Start() {}

func (m *MockTransferManager) Stop() {}

func (m *MockTransferManager) RequestDataChunk(peerID, fileID string, chunkIndex int) error {
	return nil
}

func (m *MockTransferManager) GetTransferStats(fileID string) (*TransferStats, error) {
	if stats, exists := m.transfers[fileID]; exists {
		return stats, nil
	}
	return nil, fmt.Errorf("transfer not found")
}

func (m *MockTransferManager) CancelTransfer(fileID string) error {
	delete(m.transfers, fileID)
	return nil
}

func (m *MockTransferManager) ListActiveTransfers() map[string]*TransferStats {
	return m.transfers
}

func (m *MockTransferManager) CleanupCompletedTransfers() {}

// MockStorageManager implements a minimal StorageManager for testing
type MockStorageManager struct {
	logger *zap.Logger
	files  map[string]*ChunkMetadata
	chunks map[string]map[int][]byte
}

func NewMockStorageManager(logger *zap.Logger) *MockStorageManager {
	return &MockStorageManager{
		logger: logger,
		files:  make(map[string]*ChunkMetadata),
		chunks: make(map[string]map[int][]byte),
	}
}

func (m *MockStorageManager) GetMetadata(fileID string) (*ChunkMetadata, error) {
	if metadata, exists := m.files[fileID]; exists {
		return metadata, nil
	}
	return nil, fmt.Errorf("file not found")
}

func (m *MockStorageManager) SaveMetadata(metadata *ChunkMetadata) error {
	m.files[metadata.FileID] = metadata
	return nil
}

func (m *MockStorageManager) DeleteMetadata(fileID string) error {
	delete(m.files, fileID)
	return nil
}

func (m *MockStorageManager) ListMetadata() []*ChunkMetadata {
	result := make([]*ChunkMetadata, 0, len(m.files))
	for _, metadata := range m.files {
		result = append(result, metadata)
	}
	return result
}

func (m *MockStorageManager) GetChunk(fileID string, chunkIndex int) ([]byte, error) {
	if fileChunks, exists := m.chunks[fileID]; exists {
		if data, exists := fileChunks[chunkIndex]; exists {
			return data, nil
		}
	}
	return nil, fmt.Errorf("chunk not found")
}

func (m *MockStorageManager) SaveChunk(fileID string, chunkIndex int, data []byte) error {
	if _, exists := m.chunks[fileID]; !exists {
		m.chunks[fileID] = make(map[int][]byte)
	}
	m.chunks[fileID][chunkIndex] = data
	return nil
}

func (m *MockStorageManager) DeleteChunk(fileID string, chunkIndex int) error {
	if fileChunks, exists := m.chunks[fileID]; exists {
		delete(fileChunks, chunkIndex)
	}
	return nil
}

func (m *MockStorageManager) DeleteAllChunks(fileID string) error {
	delete(m.chunks, fileID)
	return nil
}

func (m *MockStorageManager) CleanupExpiredFiles(maxAge time.Duration) error {
	return nil
}

func (m *MockStorageManager) GetStorageStats() (map[string]interface{}, error) {
	return map[string]interface{}{
		"total_files":  len(m.files),
		"total_chunks": len(m.chunks),
		"total_size":   int64(0),
	}, nil
}

// NewTestWebRTCTransport creates a WebRTCTransport for testing
func NewTestWebRTCTransport(
	logger *zap.Logger,
	transferManager *MockTransferManager,
	storageManager *MockStorageManager,
	config WebRTCConfig,
) *WebRTCTransport {
	// Convert ICE server strings to webrtc.ICEServer objects
	var iceServers []webrtc.ICEServer
	for _, server := range config.ICEServers {
		iceServers = append(iceServers, webrtc.ICEServer{
			URLs: []string{server},
		})
	}

	return &WebRTCTransport{
		logger:          logger,
		transferManager: nil, // We'll mock this functionality
		storageManager:  nil, // We'll mock this functionality
		peerConnections: make(map[string]*webrtc.PeerConnection),
		dataChannels:    make(map[string]*webrtc.DataChannel),
		activeTransfers: make(map[string]bool),
		config: webrtc.Configuration{
			ICEServers: iceServers,
		},
	}
}

func TestWebRTCTransport(t *testing.T) {
	// Skip in short mode as WebRTC tests can be slow
	if testing.Short() {
		t.Skip("Skipping WebRTC test in short mode")
	}

	// Create a logger
	logger, _ := zap.NewDevelopment()

	// Create a mock WebRTC transport directly
	transport := &WebRTCTransport{
		logger:          logger,
		peerConnections: make(map[string]*webrtc.PeerConnection),
		dataChannels:    make(map[string]*webrtc.DataChannel),
		activeTransfers: make(map[string]bool),
		config: webrtc.Configuration{
			ICEServers: []webrtc.ICEServer{
				{
					URLs: []string{"stun:stun.l.google.com:19302"},
				},
			},
		},
	}

	// Test creating a peer connection
	t.Run("CreatePeerConnection", func(t *testing.T) {
		peerID := "test-peer"
		pc, err := transport.CreatePeerConnection(peerID)
		if err != nil {
			t.Fatalf("Failed to create peer connection: %v", err)
		}
		if pc == nil {
			t.Fatal("Peer connection is nil")
		}

		// Verify the peer connection was stored
		transport.mu.RLock()
		storedPC, exists := transport.peerConnections[peerID]
		transport.mu.RUnlock()
		if !exists {
			t.Error("Peer connection not stored in transport")
		}
		if storedPC != pc {
			t.Error("Stored peer connection does not match created one")
		}

		// Verify data channel was created
		transport.mu.RLock()
		_, exists = transport.dataChannels[peerID]
		transport.mu.RUnlock()
		if !exists {
			t.Error("Data channel not created")
		}
	})

	// Test sending a data channel message
	t.Run("SendDataChannelMessage", func(t *testing.T) {
		// This is a limited test since we can't easily test actual WebRTC communication
		// without setting up a full WebRTC connection
		peerID := "test-peer"
		message := map[string]interface{}{
			"type": "test-message",
			"data": "test-data",
		}

		err := transport.SendDataChannelMessage(peerID, message)
		// We expect an error since the data channel is not connected
		if err == nil {
			t.Error("Expected error when sending to disconnected data channel")
		}
	})

	// Test requesting a file
	t.Run("RequestFile", func(t *testing.T) {
		peerID := "test-peer"
		fileID := "test-file"

		// Create a context with timeout
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()

		err := transport.RequestFile(ctx, peerID, fileID)
		// We expect this to succeed even though the message won't be sent
		if err != nil {
			t.Errorf("Failed to request file: %v", err)
		}

		// Verify the transfer was registered
		transport.transfersMu.RLock()
		transferID := peerID + ":" + fileID
		active := transport.activeTransfers[transferID]
		transport.transfersMu.RUnlock()
		if !active {
			t.Error("Transfer not registered as active")
		}
	})

	// Test cancelling a transfer
	t.Run("CancelTransfer", func(t *testing.T) {
		peerID := "test-peer"
		fileID := "test-file"

		err := transport.CancelTransfer(peerID, fileID)
		if err != nil {
			t.Errorf("Failed to cancel transfer: %v", err)
		}

		// Verify the transfer was removed
		transport.transfersMu.RLock()
		transferID := peerID + ":" + fileID
		_, exists := transport.activeTransfers[transferID]
		transport.transfersMu.RUnlock()
		if exists {
			t.Error("Transfer still active after cancellation")
		}
	})

	// Test completing a transfer
	t.Run("CompleteTransfer", func(t *testing.T) {
		peerID := "test-peer"
		fileID := "complete-file"

		// Register the transfer
		transport.transfersMu.Lock()
		transferID := peerID + ":" + fileID
		transport.activeTransfers[transferID] = true
		transport.transfersMu.Unlock()

		err := transport.CompleteTransfer(peerID, fileID)
		if err != nil {
			t.Errorf("Failed to complete transfer: %v", err)
		}

		// Verify the transfer was removed
		transport.transfersMu.RLock()
		_, exists := transport.activeTransfers[transferID]
		transport.transfersMu.RUnlock()
		if exists {
			t.Error("Transfer still active after completion")
		}
	})

	// Test closing the transport
	t.Run("Close", func(t *testing.T) {
		transport.Close()

		// Verify peer connections were cleared
		transport.mu.RLock()
		peerCount := len(transport.peerConnections)
		dataChannelCount := len(transport.dataChannels)
		transport.mu.RUnlock()

		if peerCount != 0 {
			t.Errorf("Peer connections not cleared: %d remaining", peerCount)
		}
		if dataChannelCount != 0 {
			t.Errorf("Data channels not cleared: %d remaining", dataChannelCount)
		}

		// Verify active transfers were cleared
		transport.transfersMu.RLock()
		transferCount := len(transport.activeTransfers)
		transport.transfersMu.RUnlock()
		if transferCount != 0 {
			t.Errorf("Active transfers not cleared: %d remaining", transferCount)
		}
	})
}
