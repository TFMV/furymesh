package file

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/TFMV/furymesh/crypto"
	"go.uber.org/zap"
)

const (
	// DefaultRequestTimeout is the default timeout for transfer requests
	DefaultRequestTimeout = 30 * time.Second
	// DefaultMaxRetries is the default number of retries for failed transfers
	DefaultMaxRetries = 3
	// DefaultConcurrentTransfers is the default number of concurrent transfers
	DefaultConcurrentTransfers = 5
)

// TransferStatus represents the status of a file transfer
type TransferStatus string

const (
	// TransferStatusPending indicates the transfer is pending
	TransferStatusPending TransferStatus = "pending"
	// TransferStatusInProgress indicates the transfer is in progress
	TransferStatusInProgress TransferStatus = "in_progress"
	// TransferStatusCompleted indicates the transfer is completed
	TransferStatusCompleted TransferStatus = "completed"
	// TransferStatusFailed indicates the transfer failed
	TransferStatusFailed TransferStatus = "failed"
	// TransferStatusCancelled indicates the transfer was cancelled
	TransferStatusCancelled TransferStatus = "cancelled"
)

// TransferDirection represents the direction of a file transfer
type TransferDirection string

const (
	// TransferDirectionUpload indicates an upload transfer
	TransferDirectionUpload TransferDirection = "upload"
	// TransferDirectionDownload indicates a download transfer
	TransferDirectionDownload TransferDirection = "download"
)

// TransferStats contains statistics about a file transfer
type TransferStats struct {
	StartTime         time.Time      `json:"start_time"`
	EndTime           time.Time      `json:"end_time"`
	BytesTransferred  int64          `json:"bytes_transferred"`
	TotalBytes        int64          `json:"total_bytes"`
	ChunksTransferred int            `json:"chunks_transferred"`
	TotalChunks       int            `json:"total_chunks"`
	FailedChunks      int            `json:"failed_chunks"`
	RetryCount        int            `json:"retry_count"`
	TransferRate      float64        `json:"transfer_rate_bps"`
	Status            TransferStatus `json:"status"`
	Error             string         `json:"error,omitempty"`
}

// TransferRequest represents a request to transfer a file
type TransferRequest struct {
	FileID      string            `json:"file_id"`
	PeerID      string            `json:"peer_id"`
	Direction   TransferDirection `json:"direction"`
	Priority    int               `json:"priority"`
	RequestTime time.Time         `json:"request_time"`
}

// DataRequest represents a request for a specific chunk of data
type DataRequest struct {
	RequestID    string    `json:"request_id"`
	FileID       string    `json:"file_id"`
	ChunkIndex   int       `json:"chunk_index"`
	PeerID       string    `json:"peer_id"`
	Timestamp    time.Time `json:"timestamp"`
	IsKeyRequest bool      `json:"is_key_request"`
}

// DataResponse represents a response containing a chunk of data
type DataResponse struct {
	RequestID     string         `json:"request_id"`
	FileID        string         `json:"file_id"`
	Chunk         *FileChunkData `json:"chunk"`
	EncryptedKey  []byte         `json:"encrypted_key"`
	IsKeyResponse bool           `json:"is_key_response"`
}

// ChunkStatus represents the status of a chunk
type ChunkStatus int

const (
	// ChunkStatusPending indicates the chunk is pending transfer
	ChunkStatusPending ChunkStatus = iota
	// ChunkStatusInProgress indicates the chunk is being transferred
	ChunkStatusInProgress
	// ChunkStatusTransferred indicates the chunk has been transferred
	ChunkStatusTransferred
	// ChunkStatusFailed indicates the chunk transfer failed
	ChunkStatusFailed
)

// TransferManager handles file transfers between peers
type TransferManager struct {
	logger              *zap.Logger
	chunker             *Chunker
	requestTimeout      time.Duration
	maxRetries          int
	concurrentTransfers int
	workDir             string

	// Maps of active transfers by ID
	activeTransfers map[string]*TransferStats
	transfersMu     sync.RWMutex

	// Channel for sending data requests to peers
	dataRequestCh chan *DataRequest
	// Channel for receiving data responses from peers
	dataResponseCh chan *DataResponse

	// Wait group for active transfers
	wg sync.WaitGroup
	// Context for cancellation
	ctx    context.Context
	cancel context.CancelFunc

	encryptionMgr *crypto.EncryptionManager
	sessionKeys   map[string][]byte
	sessionKeysMu sync.RWMutex
}

// NewTransferManager creates a new TransferManager
func NewTransferManager(
	logger *zap.Logger,
	chunker *Chunker,
	workDir string,
	requestTimeout time.Duration,
	maxRetries int,
	concurrentTransfers int,
	encryptionMgr *crypto.EncryptionManager,
) *TransferManager {
	if requestTimeout <= 0 {
		requestTimeout = DefaultRequestTimeout
	}
	if maxRetries < 0 {
		maxRetries = DefaultMaxRetries
	}
	if concurrentTransfers <= 0 {
		concurrentTransfers = DefaultConcurrentTransfers
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &TransferManager{
		logger:              logger,
		chunker:             chunker,
		requestTimeout:      requestTimeout,
		maxRetries:          maxRetries,
		concurrentTransfers: concurrentTransfers,
		workDir:             workDir,
		activeTransfers:     make(map[string]*TransferStats),
		dataRequestCh:       make(chan *DataRequest, 100),
		dataResponseCh:      make(chan *DataResponse, 100),
		ctx:                 ctx,
		cancel:              cancel,
		encryptionMgr:       encryptionMgr,
		sessionKeys:         make(map[string][]byte),
	}
}

// Start starts the transfer manager
func (tm *TransferManager) Start() {
	tm.logger.Info("Starting transfer manager")

	// Start worker goroutines to process transfers
	for i := 0; i < tm.concurrentTransfers; i++ {
		tm.wg.Add(1)
		go tm.transferWorker(i)
	}
}

// Stop stops the transfer manager
func (tm *TransferManager) Stop() {
	tm.logger.Info("Stopping transfer manager")
	tm.cancel()
	tm.wg.Wait()
}

// transferWorker processes transfers from the queue
func (tm *TransferManager) transferWorker(workerID int) {
	defer tm.wg.Done()

	tm.logger.Debug("Transfer worker started", zap.Int("worker_id", workerID))

	for {
		select {
		case <-tm.ctx.Done():
			tm.logger.Debug("Transfer worker stopping", zap.Int("worker_id", workerID))
			return
		case response := <-tm.dataResponseCh:
			tm.handleDataResponse(response)
		}
	}
}

// RequestDataChunk requests a chunk of data from a peer
func (tm *TransferManager) RequestDataChunk(peerID, fileID string, chunkIndex int) error {
	// Create a request
	request := &DataRequest{
		RequestID:  fmt.Sprintf("%s-%s-%d", peerID, fileID, chunkIndex),
		FileID:     fileID,
		ChunkIndex: chunkIndex,
		PeerID:     peerID,
		Timestamp:  time.Now(),
	}

	// Check if we have a session key for this file
	if tm.encryptionMgr != nil {
		tm.sessionKeysMu.RLock()
		_, hasSessionKey := tm.sessionKeys[fileID]
		tm.sessionKeysMu.RUnlock()

		// If we don't have a session key, request it first
		if !hasSessionKey {
			tm.logger.Debug("No session key found, requesting key first",
				zap.String("file_id", fileID),
				zap.String("peer_id", peerID))

			// Create a key request
			keyRequest := &DataRequest{
				RequestID:    fmt.Sprintf("%s-%s-key", peerID, fileID),
				FileID:       fileID,
				ChunkIndex:   -1, // Special value for key request
				PeerID:       peerID,
				Timestamp:    time.Now(),
				IsKeyRequest: true,
			}

			// Send the key request
			select {
			case tm.dataRequestCh <- keyRequest:
				tm.logger.Debug("Sent session key request",
					zap.String("file_id", fileID),
					zap.String("peer_id", peerID))
			default:
				return fmt.Errorf("request channel full")
			}

			// Wait for the key to be received
			// In a real implementation, this would be more sophisticated
			time.Sleep(500 * time.Millisecond)
		}
	}

	// Send the request
	select {
	case tm.dataRequestCh <- request:
		tm.logger.Debug("Sent data chunk request",
			zap.String("file_id", fileID),
			zap.Int("chunk_index", chunkIndex),
			zap.String("peer_id", peerID))
		return nil
	default:
		return fmt.Errorf("request channel full")
	}
}

// handleDataResponse processes a data response
func (tm *TransferManager) handleDataResponse(response *DataResponse) {
	// Extract the file ID and chunk index
	fileID := response.FileID

	// Check if this is a session key response
	if response.IsKeyResponse && tm.encryptionMgr != nil {
		tm.logger.Debug("Received session key response",
			zap.String("file_id", fileID))

		// Decrypt the session key
		sessionKey, err := tm.encryptionMgr.DecryptSessionKey(response.EncryptedKey)
		if err != nil {
			tm.logger.Error("Failed to decrypt session key",
				zap.String("file_id", fileID),
				zap.Error(err))
			return
		}

		// Store the session key
		tm.sessionKeysMu.Lock()
		tm.sessionKeys[fileID] = sessionKey
		tm.sessionKeysMu.Unlock()

		tm.logger.Info("Received and stored session key",
			zap.String("file_id", fileID))
		return
	}

	// Regular chunk response
	chunk := response.Chunk
	if chunk == nil {
		tm.logger.Error("Received nil chunk in data response",
			zap.String("file_id", fileID))
		return
	}

	chunkIndex := int(chunk.Index)

	tm.logger.Debug("Received data chunk response",
		zap.String("file_id", fileID),
		zap.Int("chunk_index", chunkIndex))

	// Get the transfer
	tm.transfersMu.Lock()
	stats, exists := tm.activeTransfers[fileID]
	if !exists {
		tm.logger.Warn("Received data for unknown transfer",
			zap.String("file_id", fileID),
			zap.Int("chunk_index", chunkIndex))
		tm.transfersMu.Unlock()
		return
	}

	// Decrypt the chunk if encryption is enabled
	var chunkData []byte
	if tm.encryptionMgr != nil {
		tm.sessionKeysMu.RLock()
		sessionKey, hasKey := tm.sessionKeys[fileID]
		tm.sessionKeysMu.RUnlock()

		if !hasKey {
			tm.logger.Error("No session key found for decryption",
				zap.String("file_id", fileID))
			tm.transfersMu.Unlock()
			return
		}

		var err error
		chunkData, err = tm.encryptionMgr.DecryptChunk(chunk.Data, sessionKey)
		if err != nil {
			tm.logger.Error("Failed to decrypt chunk",
				zap.String("file_id", fileID),
				zap.Int("chunk_index", chunkIndex),
				zap.Error(err))
			tm.transfersMu.Unlock()
			return
		}
	} else {
		chunkData = chunk.Data
	}

	// Update the transfer status
	stats.BytesTransferred += int64(len(chunkData))
	stats.ChunksTransferred++

	// Check if the transfer is complete
	if stats.ChunksTransferred >= stats.TotalChunks {
		stats.Status = TransferStatusCompleted
		stats.EndTime = time.Now()

		// Calculate transfer rate
		duration := stats.EndTime.Sub(stats.StartTime).Seconds()
		if duration > 0 {
			stats.TransferRate = float64(stats.BytesTransferred) / duration
		}

		tm.logger.Info("Transfer completed",
			zap.String("file_id", fileID),
			zap.Int("chunks_transferred", stats.ChunksTransferred),
			zap.Int("total_chunks", stats.TotalChunks),
			zap.Float64("transfer_rate_bps", stats.TransferRate))
	}

	tm.transfersMu.Unlock()

	// Save the chunk data
	chunkDir := filepath.Join(tm.workDir, fileID)
	if err := os.MkdirAll(chunkDir, 0755); err != nil {
		tm.logger.Error("Failed to create chunk directory",
			zap.String("file_id", fileID),
			zap.Error(err))
		return
	}

	chunkPath := filepath.Join(chunkDir, fmt.Sprintf("%d.chunk", chunkIndex))
	if err := os.WriteFile(chunkPath, chunkData, 0644); err != nil {
		tm.logger.Error("Failed to write chunk file",
			zap.String("file_id", fileID),
			zap.Int("chunk_index", chunkIndex),
			zap.Error(err))
		return
	}

	tm.logger.Debug("Saved chunk data",
		zap.String("file_id", fileID),
		zap.Int("chunk_index", chunkIndex),
		zap.Int("data_size", len(chunkData)))
}

// StartUpload starts an upload transfer
func (tm *TransferManager) StartUpload(ctx context.Context, peerID string, fileID string) (*TransferStats, error) {
	// Get metadata for the file
	metadata, err := tm.chunker.GetFileMetadata(fileID)
	if err != nil {
		return nil, fmt.Errorf("failed to get file metadata: %w", err)
	}

	tm.transfersMu.Lock()
	defer tm.transfersMu.Unlock()

	// Check if the transfer already exists
	if stats, exists := tm.activeTransfers[fileID]; exists {
		return stats, nil
	}

	// Create a new transfer
	stats := &TransferStats{
		StartTime:   time.Now(),
		TotalBytes:  metadata.FileSize,
		TotalChunks: metadata.TotalChunks,
		Status:      TransferStatusInProgress,
	}

	// Store the transfer
	tm.activeTransfers[fileID] = stats

	tm.logger.Info("Starting upload",
		zap.String("file_id", fileID),
		zap.String("peer_id", peerID),
		zap.String("file_name", metadata.FileName),
		zap.Int64("file_size", metadata.FileSize),
		zap.Int("total_chunks", metadata.TotalChunks))

	return stats, nil
}

// StartDownload initiates a download transfer
func (tm *TransferManager) StartDownload(ctx context.Context, peerID string, fileID string, fileName string, totalChunks int, fileSize int64) (*TransferStats, error) {
	tm.transfersMu.Lock()
	defer tm.transfersMu.Unlock()

	// Check if the transfer already exists
	if stats, exists := tm.activeTransfers[fileID]; exists {
		return stats, nil
	}

	// Create a new transfer
	stats := &TransferStats{
		StartTime:   time.Now(),
		TotalBytes:  fileSize,
		TotalChunks: totalChunks,
		Status:      TransferStatusInProgress,
	}

	// Store the transfer
	tm.activeTransfers[fileID] = stats

	// Start requesting chunks
	go func() {
		for i := 0; i < totalChunks; i++ {
			// Create a context with timeout for this chunk request
			err := tm.RequestDataChunk(peerID, fileID, i)
			if err != nil {
				tm.logger.Error("Failed to request chunk",
					zap.String("file_id", fileID),
					zap.Int("chunk_index", i),
					zap.Error(err))
				continue
			}

			// Sleep to avoid overwhelming the peer
			time.Sleep(10 * time.Millisecond)
		}
	}()

	return stats, nil
}

// GetTransferStats returns the statistics for a transfer
func (tm *TransferManager) GetTransferStats(fileID string) (*TransferStats, error) {
	tm.transfersMu.RLock()
	defer tm.transfersMu.RUnlock()

	stats, exists := tm.activeTransfers[fileID]
	if !exists {
		return nil, fmt.Errorf("no active transfer for file ID %s", fileID)
	}

	return stats, nil
}

// CancelTransfer cancels an active transfer
func (tm *TransferManager) CancelTransfer(fileID string) error {
	tm.transfersMu.Lock()
	defer tm.transfersMu.Unlock()

	stats, exists := tm.activeTransfers[fileID]
	if !exists {
		return fmt.Errorf("no active transfer for file ID %s", fileID)
	}

	if stats.Status != TransferStatusInProgress && stats.Status != TransferStatusPending {
		return fmt.Errorf("transfer is not in progress or pending")
	}

	stats.Status = TransferStatusCancelled
	stats.EndTime = time.Now()
	stats.Error = "transfer was cancelled by user"

	tm.logger.Info("Transfer cancelled",
		zap.String("file_id", fileID),
		zap.Int64("bytes_transferred", stats.BytesTransferred),
		zap.Int("chunks_transferred", stats.ChunksTransferred))

	return nil
}

// ListActiveTransfers returns a list of all active transfers
func (tm *TransferManager) ListActiveTransfers() map[string]*TransferStats {
	tm.transfersMu.RLock()
	defer tm.transfersMu.RUnlock()

	// Create a copy of the active transfers map
	transfers := make(map[string]*TransferStats, len(tm.activeTransfers))
	for id, stats := range tm.activeTransfers {
		transfers[id] = stats
	}

	return transfers
}

// CleanupCompletedTransfers removes completed transfers from the active transfers map
func (tm *TransferManager) CleanupCompletedTransfers() {
	tm.transfersMu.Lock()
	defer tm.transfersMu.Unlock()

	for id, stats := range tm.activeTransfers {
		if stats.Status == TransferStatusCompleted ||
			stats.Status == TransferStatusFailed ||
			stats.Status == TransferStatusCancelled {
			delete(tm.activeTransfers, id)
		}
	}
}
