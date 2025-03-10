package file

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"go.uber.org/zap"

	"github.com/TFMV/furymesh/common"
)

// WebRTCTransportAdapter implements a file transport over WebRTC
type WebRTCTransportAdapter struct {
	logger          *zap.Logger
	transferManager *TransferManager
	storageManager  *StorageManager
	webrtcTransfer  common.WebRTCTransferManagerInterface
	nodeID          string
	ctx             context.Context
	cancel          context.CancelFunc
	mu              sync.RWMutex
}

// NewWebRTCTransportAdapter creates a new WebRTC transport adapter
func NewWebRTCTransportAdapter(
	logger *zap.Logger,
	transferManager *TransferManager,
	storageManager *StorageManager,
	webrtcTransfer common.WebRTCTransferManagerInterface,
	nodeID string,
) *WebRTCTransportAdapter {
	ctx, cancel := context.WithCancel(context.Background())
	return &WebRTCTransportAdapter{
		logger:          logger,
		transferManager: transferManager,
		storageManager:  storageManager,
		webrtcTransfer:  webrtcTransfer,
		nodeID:          nodeID,
		ctx:             ctx,
		cancel:          cancel,
	}
}

// Start starts the WebRTC transport
func (w *WebRTCTransportAdapter) Start() error {
	w.logger.Info("Starting WebRTC transport adapter")
	return nil
}

// Stop stops the WebRTC transport
func (w *WebRTCTransportAdapter) Stop() error {
	w.logger.Info("Stopping WebRTC transport adapter")
	w.cancel()
	return nil
}

// RequestFile requests a file from a peer
func (w *WebRTCTransportAdapter) RequestFile(ctx context.Context, peerID, fileID string) error {
	w.logger.Info("Requesting file via WebRTC", zap.String("peerID", peerID), zap.String("fileID", fileID))

	// Request file via WebRTC
	_, err := w.webrtcTransfer.RequestFile(ctx, peerID, fileID)
	if err != nil {
		return fmt.Errorf("failed to request file via WebRTC: %w", err)
	}

	return nil
}

// RequestFileFromMultiplePeers requests a file from multiple peers
func (w *WebRTCTransportAdapter) RequestFileFromMultiplePeers(ctx context.Context, fileID string, peerIDs []string) error {
	w.logger.Info("Requesting file from multiple peers via WebRTC",
		zap.String("fileID", fileID),
		zap.Strings("peerIDs", peerIDs))

	// Request file via WebRTC
	_, err := w.webrtcTransfer.RequestFileFromMultiplePeers(ctx, fileID, peerIDs)
	if err != nil {
		return fmt.Errorf("failed to request file from multiple peers via WebRTC: %w", err)
	}

	return nil
}

// SendFile sends a file to a peer
func (w *WebRTCTransportAdapter) SendFile(ctx context.Context, peerID, fileID string) error {
	w.logger.Info("Sending file via WebRTC", zap.String("peerID", peerID), zap.String("fileID", fileID))

	// Check if we have the file
	metadata, err := w.storageManager.GetMetadata(fileID)
	if err != nil {
		return fmt.Errorf("failed to get file metadata: %w", err)
	}

	// The actual sending is handled by the WebRTC transfer manager when it receives requests
	// We just need to make sure the file is available
	w.logger.Info("File ready for WebRTC transfer",
		zap.String("fileID", fileID),
		zap.String("fileName", metadata.FileName),
		zap.Int64("fileSize", metadata.FileSize))

	return nil
}

// GetTransferStats gets the transfer stats for a file
func (w *WebRTCTransportAdapter) GetTransferStats(fileID string) (*TransferStats, error) {
	// Get transfer stats from the transfer manager
	stats, err := w.transferManager.GetTransferStats(fileID)
	if err != nil {
		return nil, fmt.Errorf("failed to get transfer stats: %w", err)
	}

	return stats, nil
}

// CancelTransfer cancels a file transfer
func (w *WebRTCTransportAdapter) CancelTransfer(fileID string) error {
	w.logger.Info("Cancelling WebRTC transfer", zap.String("fileID", fileID))

	// Find the transfer in the WebRTC transfer manager
	transfers := w.webrtcTransfer.ListTransfers()
	for _, transfer := range transfers {
		if transfer.FileID == fileID {
			return w.webrtcTransfer.CancelTransfer(transfer.ID)
		}
	}

	return errors.New("transfer not found")
}

// ListActiveTransfers lists active file transfers
func (w *WebRTCTransportAdapter) ListActiveTransfers() map[string]*TransferStats {
	w.logger.Info("Listing active WebRTC transfers")

	// Get active transfers from the WebRTC transfer manager
	webrtcTransfers := w.webrtcTransfer.ListActiveTransfers()

	// Convert to TransferStats
	transfers := make(map[string]*TransferStats)
	for _, transfer := range webrtcTransfers {
		stats := &TransferStats{
			StartTime:         transfer.StartTime,
			EndTime:           transfer.EndTime,
			BytesTransferred:  transfer.BytesTransferred,
			ChunksTransferred: transfer.CompletedChunks,
			TotalChunks:       transfer.TotalChunks,
			FailedChunks:      transfer.FailedChunks,
			RetryCount:        transfer.RetryCount,
			TransferRate:      transfer.TransferRate,
		}

		// Map the transfer state
		switch transfer.State {
		case common.TransferStateCompleted:
			stats.Status = TransferStatusCompleted
		case common.TransferStateFailed:
			stats.Status = TransferStatusFailed
			stats.Error = transfer.ErrorMessage
		case common.TransferStatePaused:
			stats.Status = TransferStatusInProgress // Use in_progress for paused
		case common.TransferStateCancelled:
			stats.Status = TransferStatusCancelled
		default:
			stats.Status = TransferStatusInProgress // Use in_progress for active
		}

		transfers[transfer.FileID] = stats
	}

	return transfers
}

// CleanupCompletedTransfers cleans up completed transfers
func (w *WebRTCTransportAdapter) CleanupCompletedTransfers() {
	w.logger.Info("Cleaning up completed WebRTC transfers")
	w.webrtcTransfer.CleanupCompletedTransfers()
}

// AddPeerForTransfer adds a peer for a file transfer
func (w *WebRTCTransportAdapter) AddPeerForTransfer(fileID string, peerID string, availableChunks []int) error {
	w.logger.Info("Adding peer for WebRTC transfer",
		zap.String("fileID", fileID),
		zap.String("peerID", peerID),
		zap.Ints("availableChunks", availableChunks))

	// In a real implementation, we would update the WebRTC transfer manager with this information
	// For now, we'll just log it
	return nil
}

// SetChunkSelectionStrategy sets the chunk selection strategy
func (w *WebRTCTransportAdapter) SetChunkSelectionStrategy(strategy ChunkSelectionStrategy) {
	w.logger.Info("Setting chunk selection strategy for WebRTC transport")

	// In a real implementation, we would update the WebRTC transfer manager with this information
	// For now, we'll just log it
}

// GetAvailablePeersForFile gets the available peers for a file
func (w *WebRTCTransportAdapter) GetAvailablePeersForFile(fileID string) map[string][]int {
	w.logger.Info("Getting available peers for file via WebRTC", zap.String("fileID", fileID))

	// In a real implementation, we would query the WebRTC transfer manager for this information
	// For now, we'll just return an empty map
	return make(map[string][]int)
}
