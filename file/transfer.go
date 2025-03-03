package file

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

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
	RequestID string `json:"request_id"`
	FileID    string `json:"file_id"`
}

// DataResponse represents a response containing a chunk of data
type DataResponse struct {
	RequestID string         `json:"request_id"`
	FileID    string         `json:"file_id"`
	Chunk     *FileChunkData `json:"chunk"`
}

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
}

// NewTransferManager creates a new TransferManager
func NewTransferManager(
	logger *zap.Logger,
	chunker *Chunker,
	workDir string,
	requestTimeout time.Duration,
	maxRetries int,
	concurrentTransfers int,
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

// RequestDataChunk sends a request for a specific chunk of a file
func (tm *TransferManager) RequestDataChunk(ctx context.Context, peerID, fileID string, chunkIndex int) error {
	// Create a unique request ID
	requestID := fmt.Sprintf("%s-%s-%d", peerID, fileID, chunkIndex)

	// Create a DataRequest
	request := &DataRequest{
		RequestID: requestID,
		FileID:    fileID,
	}

	// Send the request
	select {
	case tm.dataRequestCh <- request:
		tm.logger.Debug("Data request sent",
			zap.String("request_id", requestID),
			zap.String("file_id", fileID),
			zap.String("peer_id", peerID),
			zap.Int("chunk_index", chunkIndex))
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-tm.ctx.Done():
		return errors.New("transfer manager is shutting down")
	}
}

// handleDataResponse processes a data response from a peer
func (tm *TransferManager) handleDataResponse(response *DataResponse) {
	// Extract data from the response
	requestID := response.RequestID
	fileID := response.FileID
	chunk := response.Chunk

	if chunk == nil {
		tm.logger.Error("Received data response with nil chunk",
			zap.String("request_id", requestID),
			zap.String("file_id", fileID))
		return
	}

	chunkIndex := int(chunk.Index)

	tm.logger.Debug("Received data response",
		zap.String("request_id", requestID),
		zap.String("file_id", fileID),
		zap.Int("chunk_index", chunkIndex),
		zap.Int("data_size", len(chunk.Data)))

	// Update transfer stats
	tm.updateTransferStats(fileID, len(chunk.Data), 1, 0)
}

// updateTransferStats updates the statistics for a transfer
func (tm *TransferManager) updateTransferStats(
	fileID string,
	bytesTransferred int,
	chunksTransferred int,
	failedChunks int,
) {
	tm.transfersMu.Lock()
	defer tm.transfersMu.Unlock()

	stats, exists := tm.activeTransfers[fileID]
	if !exists {
		// This might be a response for a transfer that was cancelled or completed
		return
	}

	// Update stats
	stats.BytesTransferred += int64(bytesTransferred)
	stats.ChunksTransferred += chunksTransferred
	stats.FailedChunks += failedChunks

	// Check if transfer is complete
	if stats.ChunksTransferred == stats.TotalChunks {
		stats.Status = TransferStatusCompleted
		stats.EndTime = time.Now()

		// Calculate transfer rate
		duration := stats.EndTime.Sub(stats.StartTime).Seconds()
		if duration > 0 {
			stats.TransferRate = float64(stats.BytesTransferred) / duration
		}

		tm.logger.Info("Transfer completed",
			zap.String("file_id", fileID),
			zap.Int64("bytes_transferred", stats.BytesTransferred),
			zap.Int("chunks_transferred", stats.ChunksTransferred),
			zap.Float64("transfer_rate_bps", stats.TransferRate))
	}
}

// StartUpload initiates an upload transfer
func (tm *TransferManager) StartUpload(ctx context.Context, peerID string, fileID string) (*TransferStats, error) {
	// Get file metadata
	metadata, err := tm.chunker.GetFileMetadata(fileID)
	if err != nil {
		return nil, fmt.Errorf("failed to get file metadata: %w", err)
	}

	// Create transfer stats
	stats := &TransferStats{
		StartTime:         time.Now(),
		TotalBytes:        metadata.FileSize,
		TotalChunks:       metadata.TotalChunks,
		Status:            TransferStatusInProgress,
		BytesTransferred:  0,
		ChunksTransferred: 0,
		FailedChunks:      0,
		RetryCount:        0,
	}

	// Register the transfer
	tm.transfersMu.Lock()
	tm.activeTransfers[fileID] = stats
	tm.transfersMu.Unlock()

	tm.logger.Info("Starting upload",
		zap.String("file_id", fileID),
		zap.String("peer_id", peerID),
		zap.String("file_name", metadata.FileName),
		zap.Int64("file_size", metadata.FileSize),
		zap.Int("total_chunks", metadata.TotalChunks))

	// Start a goroutine to handle the upload
	go func() {
		defer func() {
			// If the transfer is still in progress when this goroutine exits,
			// mark it as failed
			tm.transfersMu.Lock()
			if stats.Status == TransferStatusInProgress {
				stats.Status = TransferStatusFailed
				stats.EndTime = time.Now()
				stats.Error = "upload was interrupted"
			}
			tm.transfersMu.Unlock()
		}()

		// Create a context with timeout for the entire upload
		uploadCtx, cancel := context.WithTimeout(tm.ctx, tm.requestTimeout*time.Duration(metadata.TotalChunks))
		defer cancel()

		// Process each chunk
		for i := 0; i < metadata.TotalChunks; i++ {
			// Check if the upload has been cancelled
			select {
			case <-uploadCtx.Done():
				tm.logger.Warn("Upload cancelled or timed out",
					zap.String("file_id", fileID),
					zap.String("peer_id", peerID),
					zap.Error(uploadCtx.Err()))
				return
			default:
				// Continue with the upload
			}

			// Get the chunk
			chunk, err := tm.chunker.GetChunk(fileID, i)
			if err != nil {
				tm.logger.Error("Failed to get chunk",
					zap.String("file_id", fileID),
					zap.Int("chunk_index", i),
					zap.Error(err))

				tm.transfersMu.Lock()
				stats.FailedChunks++
				tm.transfersMu.Unlock()
				continue
			}

			// TODO: Implement the actual sending of the chunk to the peer
			// This would typically involve serializing the chunk using FlatBuffers
			// and sending it over a WebRTC data channel

			// For now, we'll just update the stats
			tm.updateTransferStats(fileID, len(chunk.Data), 1, 0)

			// Add a small delay to simulate network latency
			time.Sleep(10 * time.Millisecond)
		}
	}()

	return stats, nil
}

// StartDownload initiates a download transfer
func (tm *TransferManager) StartDownload(ctx context.Context, peerID string, fileID string, fileName string, totalChunks int, fileSize int64) (*TransferStats, error) {
	// Create transfer stats
	stats := &TransferStats{
		StartTime:         time.Now(),
		TotalBytes:        fileSize,
		TotalChunks:       totalChunks,
		Status:            TransferStatusInProgress,
		BytesTransferred:  0,
		ChunksTransferred: 0,
		FailedChunks:      0,
		RetryCount:        0,
	}

	// Register the transfer
	tm.transfersMu.Lock()
	tm.activeTransfers[fileID] = stats
	tm.transfersMu.Unlock()

	tm.logger.Info("Starting download",
		zap.String("file_id", fileID),
		zap.String("peer_id", peerID),
		zap.String("file_name", fileName),
		zap.Int64("file_size", fileSize),
		zap.Int("total_chunks", totalChunks))

	// Start a goroutine to handle the download
	go func() {
		defer func() {
			// If the transfer is still in progress when this goroutine exits,
			// mark it as failed
			tm.transfersMu.Lock()
			if stats.Status == TransferStatusInProgress {
				stats.Status = TransferStatusFailed
				stats.EndTime = time.Now()
				stats.Error = "download was interrupted"
			}
			tm.transfersMu.Unlock()
		}()

		// Create a context with timeout for the entire download
		downloadCtx, cancel := context.WithTimeout(tm.ctx, tm.requestTimeout*time.Duration(totalChunks))
		defer cancel()

		// Request each chunk
		for i := 0; i < totalChunks; i++ {
			// Check if the download has been cancelled
			select {
			case <-downloadCtx.Done():
				tm.logger.Warn("Download cancelled or timed out",
					zap.String("file_id", fileID),
					zap.String("peer_id", peerID),
					zap.Error(downloadCtx.Err()))
				return
			default:
				// Continue with the download
			}

			// Request the chunk with retries
			var success bool
			for retry := 0; retry <= tm.maxRetries; retry++ {
				// Create a context with timeout for this chunk request
				chunkCtx, chunkCancel := context.WithTimeout(downloadCtx, tm.requestTimeout)

				// Request the chunk
				err := tm.RequestDataChunk(chunkCtx, peerID, fileID, i)
				chunkCancel()

				if err == nil {
					success = true
					break
				}

				tm.logger.Warn("Failed to request chunk, retrying",
					zap.String("file_id", fileID),
					zap.String("peer_id", peerID),
					zap.Int("chunk_index", i),
					zap.Int("retry", retry),
					zap.Error(err))

				tm.transfersMu.Lock()
				stats.RetryCount++
				tm.transfersMu.Unlock()

				// Wait before retrying
				select {
				case <-downloadCtx.Done():
					return
				case <-time.After(time.Duration(retry*500) * time.Millisecond):
					// Exponential backoff
				}
			}

			if !success {
				tm.logger.Error("Failed to request chunk after retries",
					zap.String("file_id", fileID),
					zap.String("peer_id", peerID),
					zap.Int("chunk_index", i))

				tm.transfersMu.Lock()
				stats.FailedChunks++
				tm.transfersMu.Unlock()
			}
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
