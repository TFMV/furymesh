package node

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/TFMV/furymesh/common"
	"github.com/TFMV/furymesh/file"
	"github.com/TFMV/furymesh/fury/fury"
	"github.com/TFMV/furymesh/metrics"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

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

// WebRTCTransfer represents a file transfer over WebRTC
type WebRTCTransfer struct {
	ID               string
	FileID           string
	PeerID           string
	State            common.TransferState
	StartTime        time.Time
	EndTime          time.Time
	TotalChunks      int
	CompletedChunks  int
	FailedChunks     int
	RetryCount       int
	BytesTransferred int64
	TransferRate     float64 // bytes per second
	LastActivity     time.Time
	Error            error
	mu               sync.RWMutex
}

// ToTransferInfo converts a WebRTCTransfer to a WebRTCTransferInfo
func (t *WebRTCTransfer) ToTransferInfo() *common.WebRTCTransferInfo {
	t.mu.RLock()
	defer t.mu.RUnlock()

	errorMsg := ""
	if t.Error != nil {
		errorMsg = t.Error.Error()
	}

	return &common.WebRTCTransferInfo{
		ID:               t.ID,
		FileID:           t.FileID,
		PeerID:           t.PeerID,
		State:            t.State,
		StartTime:        t.StartTime,
		EndTime:          t.EndTime,
		TotalChunks:      t.TotalChunks,
		CompletedChunks:  t.CompletedChunks,
		FailedChunks:     t.FailedChunks,
		RetryCount:       t.RetryCount,
		BytesTransferred: t.BytesTransferred,
		TransferRate:     t.TransferRate,
		LastActivity:     t.LastActivity,
		ErrorMessage:     errorMsg,
	}
}

// WebRTCTransferManager manages file transfers over WebRTC
type WebRTCTransferManager struct {
	logger          *zap.Logger
	config          common.WebRTCTransferConfig
	webrtcMgr       *WebRTCManager
	messaging       *WebRTCMessaging
	fileManager     *FileManager
	transfers       map[string]*WebRTCTransfer
	activeTransfers map[string]context.CancelFunc
	mu              sync.RWMutex
	ctx             context.Context
	cancel          context.CancelFunc
}

// NewWebRTCTransferManager creates a new WebRTC transfer manager
func NewWebRTCTransferManager(
	logger *zap.Logger,
	config common.WebRTCTransferConfig,
	webrtcMgr *WebRTCManager,
	messaging *WebRTCMessaging,
	fileManager *FileManager,
) *WebRTCTransferManager {
	ctx, cancel := context.WithCancel(context.Background())
	manager := &WebRTCTransferManager{
		logger:          logger,
		config:          config,
		webrtcMgr:       webrtcMgr,
		messaging:       messaging,
		fileManager:     fileManager,
		transfers:       make(map[string]*WebRTCTransfer),
		activeTransfers: make(map[string]context.CancelFunc),
		ctx:             ctx,
		cancel:          cancel,
	}

	// Register message handlers
	messaging.RegisterHandler(fury.MessageTypeFileMetadata, manager.handleFileMetadata)
	messaging.RegisterHandler(fury.MessageTypeFileChunk, manager.handleFileChunk)
	messaging.RegisterHandler(fury.MessageTypeChunkRequest, manager.handleChunkRequest)
	messaging.RegisterHandler(fury.MessageTypeTransferStatus, manager.handleTransferStatus)
	messaging.RegisterHandler(fury.MessageTypeErrorMessage, manager.handleErrorMessage)

	return manager
}

// Start starts the WebRTC transfer manager
func (w *WebRTCTransferManager) Start() {
	go w.monitorTransfers()
}

// Stop stops the WebRTC transfer manager
func (w *WebRTCTransferManager) Stop() {
	w.cancel()
	w.cancelAllTransfers()
}

// RequestFile requests a file from a peer
func (w *WebRTCTransferManager) RequestFile(ctx context.Context, peerID, fileID string) (string, error) {
	// Check if peer is connected
	state, err := w.webrtcMgr.GetPeerConnectionState(peerID)
	if err != nil || state != PeerConnectionStateConnected {
		return "", fmt.Errorf("peer not connected: %w", err)
	}

	// Create transfer
	transfer := &WebRTCTransfer{
		ID:           uuid.New().String(),
		FileID:       fileID,
		PeerID:       peerID,
		State:        common.TransferStateInitializing,
		StartTime:    time.Now(),
		LastActivity: time.Now(),
	}

	// Store transfer
	w.mu.Lock()
	w.transfers[transfer.ID] = transfer
	w.mu.Unlock()

	// Create transfer context
	transferCtx, cancelFunc := context.WithCancel(ctx)
	w.mu.Lock()
	w.activeTransfers[transfer.ID] = cancelFunc
	w.mu.Unlock()

	// Start transfer
	go w.startFileTransfer(transferCtx, transfer)

	return transfer.ID, nil
}

// RequestFileFromMultiplePeers requests a file from multiple peers
func (w *WebRTCTransferManager) RequestFileFromMultiplePeers(ctx context.Context, fileID string, peerIDs []string) (string, error) {
	if len(peerIDs) == 0 {
		return "", errors.New("no peers specified")
	}

	// Create transfer
	transfer := &WebRTCTransfer{
		ID:           uuid.New().String(),
		FileID:       fileID,
		State:        common.TransferStateInitializing,
		StartTime:    time.Now(),
		LastActivity: time.Now(),
	}

	// Store transfer
	w.mu.Lock()
	w.transfers[transfer.ID] = transfer
	w.mu.Unlock()

	// Create transfer context
	transferCtx, cancelFunc := context.WithCancel(ctx)
	w.mu.Lock()
	w.activeTransfers[transfer.ID] = cancelFunc
	w.mu.Unlock()

	// Start multi-peer transfer
	go w.startMultiPeerFileTransfer(transferCtx, transfer, peerIDs)

	return transfer.ID, nil
}

// CancelTransfer cancels a file transfer
func (w *WebRTCTransferManager) CancelTransfer(transferID string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Check if transfer exists
	transfer, exists := w.transfers[transferID]
	if !exists {
		return errors.New("transfer not found")
	}

	// Cancel transfer
	if cancelFunc, exists := w.activeTransfers[transferID]; exists {
		cancelFunc()
		delete(w.activeTransfers, transferID)
	}

	// Update transfer state
	transfer.mu.Lock()
	transfer.State = common.TransferStateCancelled
	transfer.EndTime = time.Now()
	transfer.mu.Unlock()

	return nil
}

// GetTransfer gets a file transfer
func (w *WebRTCTransferManager) GetTransfer(transferID string) (*common.WebRTCTransferInfo, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	transfer, exists := w.transfers[transferID]
	if !exists {
		return nil, errors.New("transfer not found")
	}

	return transfer.ToTransferInfo(), nil
}

// ListTransfers lists all file transfers
func (w *WebRTCTransferManager) ListTransfers() []*common.WebRTCTransferInfo {
	w.mu.RLock()
	defer w.mu.RUnlock()

	transfers := make([]*common.WebRTCTransferInfo, 0, len(w.transfers))
	for _, transfer := range w.transfers {
		transfers = append(transfers, transfer.ToTransferInfo())
	}

	return transfers
}

// ListActiveTransfers lists active file transfers
func (w *WebRTCTransferManager) ListActiveTransfers() []*common.WebRTCTransferInfo {
	w.mu.RLock()
	defer w.mu.RUnlock()

	transfers := make([]*common.WebRTCTransferInfo, 0)
	for _, transfer := range w.transfers {
		transfer.mu.RLock()
		if transfer.State == common.TransferStateActive || transfer.State == common.TransferStateInitializing {
			transfers = append(transfers, transfer.ToTransferInfo())
		}
		transfer.mu.RUnlock()
	}

	return transfers
}

// CleanupCompletedTransfers cleans up completed transfers
func (w *WebRTCTransferManager) CleanupCompletedTransfers() {
	w.mu.Lock()
	defer w.mu.Unlock()

	for id, transfer := range w.transfers {
		transfer.mu.RLock()
		state := transfer.State
		endTime := transfer.EndTime
		transfer.mu.RUnlock()

		if (state == common.TransferStateCompleted || state == common.TransferStateFailed || state == common.TransferStateCancelled) &&
			time.Since(endTime) > 24*time.Hour {
			delete(w.transfers, id)
		}
	}
}

// startFileTransfer starts a file transfer
func (w *WebRTCTransferManager) startFileTransfer(ctx context.Context, transfer *WebRTCTransfer) {
	// Update transfer state
	transfer.mu.Lock()
	transfer.State = common.TransferStateActive
	transfer.mu.Unlock()

	// Request file metadata
	err := w.requestFileMetadata(transfer.PeerID, transfer.FileID)
	if err != nil {
		w.handleTransferError(transfer, fmt.Errorf("failed to request file metadata: %w", err))
		return
	}

	// Wait for metadata or context cancellation
	select {
	case <-ctx.Done():
		w.handleTransferError(transfer, ctx.Err())
		return
	case <-time.After(w.config.IdleTimeout):
		w.handleTransferError(transfer, errors.New("timeout waiting for file metadata"))
		return
	}

	// Note: The rest of the transfer process is handled by message handlers
}

// startMultiPeerFileTransfer starts a file transfer from multiple peers
func (w *WebRTCTransferManager) startMultiPeerFileTransfer(ctx context.Context, transfer *WebRTCTransfer, peerIDs []string) {
	// Update transfer state
	transfer.mu.Lock()
	transfer.State = common.TransferStateActive
	transfer.mu.Unlock()

	// Request file metadata from first peer
	err := w.requestFileMetadata(peerIDs[0], transfer.FileID)
	if err != nil {
		w.handleTransferError(transfer, fmt.Errorf("failed to request file metadata: %w", err))
		return
	}

	// Wait for metadata or context cancellation
	select {
	case <-ctx.Done():
		w.handleTransferError(transfer, ctx.Err())
		return
	case <-time.After(w.config.IdleTimeout):
		w.handleTransferError(transfer, errors.New("timeout waiting for file metadata"))
		return
	}

	// Note: The rest of the transfer process is handled by message handlers
}

// requestFileMetadata requests file metadata from a peer
func (w *WebRTCTransferManager) requestFileMetadata(peerID, fileID string) error {
	// Create a data channel for file transfer if it doesn't exist
	_, err := w.webrtcMgr.CreateDataChannel(peerID, "file-transfer", true, nil)
	if err != nil {
		return fmt.Errorf("failed to create data channel: %w", err)
	}

	// Send error message to request metadata
	return w.messaging.SendErrorMessage(peerID, 0, "REQUEST_METADATA", fileID)
}

// requestFileChunk requests a file chunk from a peer
func (w *WebRTCTransferManager) requestFileChunk(peerID, fileID string, chunkIndex int) error {
	return w.messaging.SendChunkRequest(peerID, fileID, chunkIndex, 1)
}

// sendFileChunk sends a file chunk to a peer
func (w *WebRTCTransferManager) sendFileChunk(peerID, fileID string, chunkIndex int) error {
	ctx, span := otel.Tracer("furymesh/transfer").Start(context.Background(), "sendFileChunk")
	defer span.End()
	span.SetAttributes(
		attribute.String("peer_id", peerID),
		attribute.String("file_id", fileID),
		attribute.Int("chunk_index", chunkIndex),
	)

	// Get chunk data
	chunk, err := w.fileManager.GetChunker().GetChunk(fileID, chunkIndex)
	if err != nil {
		return fmt.Errorf("failed to get chunk: %w", err)
	}

	// Send chunk
	err = w.messaging.SendFileChunk(peerID, fileID, chunkIndex, chunk.Data, false, "")
	if err == nil {
		metrics.TransferThroughput.Add(float64(len(chunk.Data)))
	}
	return err
}

// handleFileMetadata handles a file metadata message
func (w *WebRTCTransferManager) handleFileMetadata(peerID string, messageType fury.MessageType, data []byte) error {
	// Deserialize metadata
	metadata, err := w.messaging.GetSerializer().DeserializeFileMetadata(data)
	if err != nil {
		return fmt.Errorf("failed to deserialize file metadata: %w", err)
	}

	// Find transfer for this file
	var transfer *WebRTCTransfer
	w.mu.RLock()
	for _, t := range w.transfers {
		if t.FileID == metadata.FileID && (t.PeerID == peerID || t.PeerID == "") {
			transfer = t
			break
		}
	}
	w.mu.RUnlock()

	if transfer == nil {
		// This might be a metadata request, save the metadata
		err = w.fileManager.storageManager.SaveMetadata(metadata)
		if err != nil {
			return fmt.Errorf("failed to save metadata: %w", err)
		}
		return nil
	}

	// Update transfer with metadata
	transfer.mu.Lock()
	transfer.TotalChunks = len(metadata.ChunkHashes)
	transfer.LastActivity = time.Now()
	transfer.mu.Unlock()

	// Save metadata
	err = w.fileManager.storageManager.SaveMetadata(metadata)
	if err != nil {
		return fmt.Errorf("failed to save metadata: %w", err)
	}

	// Start requesting chunks
	go w.requestChunks(transfer, metadata)

	return nil
}

// handleFileChunk handles a file chunk message
func (w *WebRTCTransferManager) handleFileChunk(peerID string, messageType fury.MessageType, data []byte) error {
	ctx, span := otel.Tracer("furymesh/transfer").Start(context.Background(), "handleFileChunk")
	defer span.End()
	span.SetAttributes(attribute.String("peer_id", peerID))

	// Get the file chunk from the message
	fileChunk, err := w.messaging.GetSerializer().DeserializeFileChunk(data)
	if err != nil {
		w.logger.Error("Failed to deserialize file chunk", zap.Error(err))
		return err
	}

	// Find the transfer for this file
	var transfer *WebRTCTransfer
	w.mu.RLock()
	for _, t := range w.transfers {
		if t.FileID == fileChunk.ID && (t.PeerID == peerID || t.PeerID == "") {
			transfer = t
			break
		}
	}
	w.mu.RUnlock()

	if transfer == nil {
		// This might be a chunk request, check if we have the file
		_, err := w.fileManager.GetStorageManager().GetMetadata(fileChunk.ID)
		if err != nil {
			return fmt.Errorf("file not found: %s", fileChunk.ID)
		}

		// Send the requested chunk
		return w.sendFileChunk(peerID, fileChunk.ID, int(fileChunk.Index))
	}

	// Save chunk
	err = w.saveChunk(fileChunk.ID, int(fileChunk.Index), fileChunk.Data)
	if err != nil {
		w.logger.Error("Failed to save chunk", zap.Error(err))
		return err
	}

	// Update transfer
	transfer.mu.Lock()
	transfer.CompletedChunks++
	transfer.BytesTransferred += int64(len(fileChunk.Data))
	metrics.TransferThroughput.Add(float64(len(fileChunk.Data)))
	transfer.LastActivity = time.Now()

	// Calculate transfer rate
	elapsedSeconds := time.Since(transfer.StartTime).Seconds()
	if elapsedSeconds > 0 {
		transfer.TransferRate = float64(transfer.BytesTransferred) / elapsedSeconds
	}

	// Check if transfer is complete
	if transfer.CompletedChunks >= transfer.TotalChunks {
		transfer.State = common.TransferStateCompleted
		transfer.EndTime = time.Now()
		w.logger.Info("Transfer completed",
			zap.String("file_id", transfer.FileID),
			zap.String("peer_id", transfer.PeerID),
			zap.Int("total_chunks", transfer.TotalChunks),
			zap.Int64("bytes", transfer.BytesTransferred),
			zap.Float64("rate_bps", transfer.TransferRate))
	}
	transfer.mu.Unlock()

	return nil
}

// handleChunkRequest handles a chunk request message
func (w *WebRTCTransferManager) handleChunkRequest(peerID string, messageType fury.MessageType, data []byte) error {
	// Deserialize request
	request, err := w.messaging.GetSerializer().DeserializeChunkRequest(data)
	if err != nil {
		return fmt.Errorf("failed to deserialize chunk request: %w", err)
	}

	// Send the requested chunk
	return w.sendFileChunk(peerID, request.FileID, request.ChunkIndex)
}

// handleTransferStatus handles a transfer status message
func (w *WebRTCTransferManager) handleTransferStatus(peerID string, messageType fury.MessageType, data []byte) error {
	// Deserialize status
	status, err := w.messaging.GetSerializer().DeserializeTransferStatus(data)
	if err != nil {
		return fmt.Errorf("failed to deserialize transfer status: %w", err)
	}

	// Find transfer for this file
	var transfer *WebRTCTransfer
	w.mu.RLock()
	for _, t := range w.transfers {
		// We need to match by transfer ID since TransferStats doesn't have FileID
		// In a real implementation, we would include the file ID in the transfer status
		if t.PeerID == peerID || t.PeerID == "" {
			transfer = t
			break
		}
	}
	w.mu.RUnlock()

	if transfer == nil {
		// Ignore status for unknown transfer
		return nil
	}

	// Update transfer with status
	transfer.mu.Lock()
	transfer.CompletedChunks = status.ChunksTransferred
	transfer.FailedChunks = status.FailedChunks
	transfer.BytesTransferred = status.BytesTransferred
	transfer.LastActivity = time.Now()

	// Calculate transfer rate
	elapsedSeconds := time.Since(transfer.StartTime).Seconds()
	if elapsedSeconds > 0 {
		transfer.TransferRate = float64(transfer.BytesTransferred) / elapsedSeconds
	}
	transfer.mu.Unlock()

	return nil
}

// handleErrorMessage handles an error message
func (w *WebRTCTransferManager) handleErrorMessage(peerID string, messageType fury.MessageType, data []byte) error {
	// Deserialize error
	errorMsg, err := w.messaging.GetSerializer().DeserializeErrorMessage(data)
	if err != nil {
		return fmt.Errorf("failed to deserialize error message: %w", err)
	}

	// Check if this is a metadata request
	if errorMsg.Message == "REQUEST_METADATA" {
		// Get metadata for the requested file
		metadata, err := w.fileManager.storageManager.GetMetadata(errorMsg.Context)
		if err != nil {
			// Send error response
			return w.messaging.SendErrorMessage(peerID, 404, "FILE_NOT_FOUND", errorMsg.Context)
		}

		// Send metadata
		return w.messaging.SendFileMetadata(peerID, metadata)
	}

	// Find transfer for this file
	var transfer *WebRTCTransfer
	w.mu.RLock()
	for _, t := range w.transfers {
		if t.FileID == errorMsg.Context && (t.PeerID == peerID || t.PeerID == "") {
			transfer = t
			break
		}
	}
	w.mu.RUnlock()

	if transfer == nil {
		// Ignore error for unknown transfer
		return nil
	}

	// Update transfer with error
	w.handleTransferError(transfer, fmt.Errorf("peer error: %s", errorMsg.Message))

	return nil
}

// handleTransferError handles a transfer error
func (w *WebRTCTransferManager) handleTransferError(transfer *WebRTCTransfer, err error) {
	transfer.mu.Lock()
	defer transfer.mu.Unlock()

	// Update transfer state
	transfer.State = common.TransferStateFailed
	transfer.Error = err
	transfer.EndTime = time.Now()

	// Log error
	w.logger.Error("File transfer failed",
		zap.String("transferID", transfer.ID),
		zap.String("fileID", transfer.FileID),
		zap.String("peerID", transfer.PeerID),
		zap.Error(err))

	// Cancel the transfer context
	w.mu.Lock()
	if cancelFunc, exists := w.activeTransfers[transfer.ID]; exists {
		cancelFunc()
		delete(w.activeTransfers, transfer.ID)
	}
	w.mu.Unlock()
}

// requestChunks requests chunks for a file transfer
func (w *WebRTCTransferManager) requestChunks(transfer *WebRTCTransfer, metadata *file.ChunkMetadata) {
	// Create a semaphore to limit concurrent requests
	sem := make(chan struct{}, w.config.MaxConcurrentChunks)

	// Create a wait group to wait for all requests to complete
	var wg sync.WaitGroup

	// Get transfer context
	w.mu.RLock()
	cancelFunc, exists := w.activeTransfers[transfer.ID]
	w.mu.RUnlock()

	if !exists {
		w.handleTransferError(transfer, errors.New("transfer context not found"))
		return
	}

	// Get the context from the cancel function
	var ctx context.Context
	if cancelFunc != nil {
		// In a real implementation, we would store the context along with the cancel function
		// For now, we'll use a background context
		ctx = context.Background()
	} else {
		ctx = context.Background()
	}

	// Request each chunk
	for i := 0; i < len(metadata.ChunkHashes); i++ {
		// Check if we already have this chunk
		_, err := w.fileManager.storageManager.GetChunk(metadata.FileID, i)
		if err == nil {
			// We already have this chunk, skip it
			transfer.mu.Lock()
			transfer.CompletedChunks++
			transfer.mu.Unlock()
			continue
		}

		// Acquire semaphore
		sem <- struct{}{}
		wg.Add(1)

		// Request chunk
		go func(chunkIndex int) {
			defer wg.Done()
			defer func() { <-sem }()

			// Request chunk with retries
			for retry := 0; retry < w.config.MaxRetries; retry++ {
				// Check if context is cancelled
				select {
				case <-ctx.Done():
					return
				default:
				}

				// Request chunk
				err := w.requestFileChunk(transfer.PeerID, metadata.FileID, chunkIndex)
				if err != nil {
					w.logger.Error("Failed to request chunk",
						zap.String("transferID", transfer.ID),
						zap.String("fileID", metadata.FileID),
						zap.Int("chunkIndex", chunkIndex),
						zap.Error(err))

					// Wait before retrying
					select {
					case <-ctx.Done():
						return
					case <-time.After(w.config.RetryInterval):
					}
					continue
				}

				// Wait for chunk to be received or timeout
				timeout := time.After(w.config.IdleTimeout)
				ticker := time.NewTicker(100 * time.Millisecond)
				defer ticker.Stop()

			chunkWaitLoop:
				for {
					select {
					case <-ctx.Done():
						return
					case <-timeout:
						// Timeout waiting for chunk
						w.logger.Warn("Timeout waiting for chunk",
							zap.String("transferID", transfer.ID),
							zap.String("fileID", metadata.FileID),
							zap.Int("chunkIndex", chunkIndex))
						break chunkWaitLoop
					case <-ticker.C:
						// Check if chunk has been received
						_, err := w.fileManager.GetStorageManager().GetChunk(metadata.FileID, chunkIndex)
						if err == nil {
							// Chunk received
							return
						}
					}
				}
			}

			// Failed to receive chunk after retries
			transfer.mu.Lock()
			transfer.FailedChunks++
			transfer.mu.Unlock()

			w.logger.Error("Failed to receive chunk after retries",
				zap.String("transferID", transfer.ID),
				zap.String("fileID", metadata.FileID),
				zap.Int("chunkIndex", chunkIndex))
		}(i)
	}

	// Wait for all requests to complete
	wg.Wait()

	// Check if transfer is complete
	transfer.mu.Lock()
	if transfer.CompletedChunks+transfer.FailedChunks == transfer.TotalChunks {
		if transfer.FailedChunks > 0 {
			// Some chunks failed
			transfer.State = common.TransferStateFailed
			transfer.Error = fmt.Errorf("failed to receive %d chunks", transfer.FailedChunks)
		} else {
			// All chunks received
			transfer.State = common.TransferStateCompleted
		}
		transfer.EndTime = time.Now()

		// Cancel the transfer context
		w.mu.Lock()
		if cancelFunc, exists := w.activeTransfers[transfer.ID]; exists {
			cancelFunc()
			delete(w.activeTransfers, transfer.ID)
		}
		w.mu.Unlock()
	}
	transfer.mu.Unlock()
}

// monitorTransfers monitors transfers for timeouts
func (w *WebRTCTransferManager) monitorTransfers() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			w.checkTransferTimeouts()
		case <-w.ctx.Done():
			return
		}
	}
}

// checkTransferTimeouts checks for timed out transfers
func (w *WebRTCTransferManager) checkTransferTimeouts() {
	w.mu.RLock()
	defer w.mu.RUnlock()

	now := time.Now()

	for _, transfer := range w.transfers {
		transfer.mu.RLock()
		state := transfer.State
		lastActivity := transfer.LastActivity
		transfer.mu.RUnlock()

		if (state == common.TransferStateActive || state == common.TransferStateInitializing) &&
			now.Sub(lastActivity) > w.config.IdleTimeout {
			// Transfer has timed out
			w.handleTransferError(transfer, errors.New("transfer timed out"))
		}
	}
}

// cancelAllTransfers cancels all active transfers
func (w *WebRTCTransferManager) cancelAllTransfers() {
	w.mu.Lock()
	defer w.mu.Unlock()

	for transferID, cancelFunc := range w.activeTransfers {
		cancelFunc()
		delete(w.activeTransfers, transferID)

		// Update transfer state
		if transfer, exists := w.transfers[transferID]; exists {
			transfer.mu.Lock()
			transfer.State = common.TransferStateCancelled
			transfer.EndTime = time.Now()
			transfer.mu.Unlock()
		}
	}
}

// saveChunk saves a chunk to storage
func (w *WebRTCTransferManager) saveChunk(fileID string, chunkIndex int, data []byte) error {
	// Save the chunk to storage
	err := w.fileManager.GetStorageManager().SaveChunk(fileID, chunkIndex, data)
	if err != nil {
		return fmt.Errorf("failed to save chunk: %w", err)
	}
	return nil
}
