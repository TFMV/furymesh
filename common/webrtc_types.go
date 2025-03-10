package common

import (
	"context"
	"time"
)

// TransferState represents the state of a file transfer
type TransferState int

const (
	// TransferStateInitializing indicates the transfer is initializing
	TransferStateInitializing TransferState = iota
	// TransferStateActive indicates the transfer is active
	TransferStateActive
	// TransferStatePaused indicates the transfer is paused
	TransferStatePaused
	// TransferStateCompleted indicates the transfer is completed
	TransferStateCompleted
	// TransferStateFailed indicates the transfer failed
	TransferStateFailed
	// TransferStateCancelled indicates the transfer was cancelled
	TransferStateCancelled
)

// String returns a string representation of the transfer state
func (s TransferState) String() string {
	switch s {
	case TransferStateInitializing:
		return "initializing"
	case TransferStateActive:
		return "active"
	case TransferStatePaused:
		return "paused"
	case TransferStateCompleted:
		return "completed"
	case TransferStateFailed:
		return "failed"
	case TransferStateCancelled:
		return "cancelled"
	default:
		return "unknown"
	}
}

// WebRTCTransferConfig contains configuration for WebRTC file transfers
type WebRTCTransferConfig struct {
	ChunkSize           int           `json:"chunk_size"`
	MaxConcurrentChunks int           `json:"max_concurrent_chunks"`
	RetryInterval       time.Duration `json:"retry_interval"`
	MaxRetries          int           `json:"max_retries"`
	IdleTimeout         time.Duration `json:"idle_timeout"`
	BufferSize          int           `json:"buffer_size"`
}

// DefaultWebRTCTransferConfig returns a default WebRTC transfer configuration
func DefaultWebRTCTransferConfig() WebRTCTransferConfig {
	return WebRTCTransferConfig{
		ChunkSize:           1024 * 1024, // 1MB
		MaxConcurrentChunks: 5,
		RetryInterval:       5 * time.Second,
		MaxRetries:          3,
		IdleTimeout:         30 * time.Second,
		BufferSize:          10, // Buffer 10 chunks ahead
	}
}

// WebRTCTransferInfo contains information about a WebRTC transfer
type WebRTCTransferInfo struct {
	ID               string
	FileID           string
	PeerID           string
	State            TransferState
	StartTime        time.Time
	EndTime          time.Time
	TotalChunks      int
	CompletedChunks  int
	FailedChunks     int
	RetryCount       int
	BytesTransferred int64
	TransferRate     float64 // bytes per second
	LastActivity     time.Time
	ErrorMessage     string
}

// WebRTCTransferManagerInterface defines the interface for a WebRTC transfer manager
type WebRTCTransferManagerInterface interface {
	// RequestFile requests a file from a peer
	RequestFile(ctx context.Context, peerID, fileID string) (string, error)

	// RequestFileFromMultiplePeers requests a file from multiple peers
	RequestFileFromMultiplePeers(ctx context.Context, fileID string, peerIDs []string) (string, error)

	// CancelTransfer cancels a file transfer
	CancelTransfer(transferID string) error

	// GetTransfer gets a file transfer
	GetTransfer(transferID string) (*WebRTCTransferInfo, error)

	// ListTransfers lists all file transfers
	ListTransfers() []*WebRTCTransferInfo

	// ListActiveTransfers lists active file transfers
	ListActiveTransfers() []*WebRTCTransferInfo

	// CleanupCompletedTransfers cleans up completed transfers
	CleanupCompletedTransfers()
}

// WebRTCTransportInterface defines the interface for a WebRTC transport
type WebRTCTransportInterface interface {
	// Start starts the WebRTC transport
	Start() error

	// Stop stops the WebRTC transport
	Stop() error

	// RequestFile requests a file from a peer
	RequestFile(ctx context.Context, peerID, fileID string) error

	// RequestFileFromMultiplePeers requests a file from multiple peers
	RequestFileFromMultiplePeers(ctx context.Context, fileID string, peerIDs []string) error

	// CancelTransfer cancels a file transfer
	CancelTransfer(fileID string) error

	// AddPeerForTransfer adds a peer for a file transfer
	AddPeerForTransfer(fileID string, peerID string, availableChunks []int) error

	// GetAvailablePeersForFile gets the available peers for a file
	GetAvailablePeersForFile(fileID string) map[string][]int

	// CleanupCompletedTransfers cleans up completed transfers
	CleanupCompletedTransfers()
}
