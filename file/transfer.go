package file

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
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

// ChunkSelectionStrategy defines how chunks are selected from available peers
type ChunkSelectionStrategy interface {
	// SelectChunksFromPeers selects which chunks to request from which peers
	SelectChunksFromPeers(fileID string, neededChunks []int, availablePeers map[string][]int) map[string][]int
}

// RoundRobinStrategy implements a simple round-robin chunk selection strategy
type RoundRobinStrategy struct{}

// SelectChunksFromPeers implements the ChunkSelectionStrategy interface
func (s *RoundRobinStrategy) SelectChunksFromPeers(fileID string, neededChunks []int, availablePeers map[string][]int) map[string][]int {
	result := make(map[string][]int)
	if len(neededChunks) == 0 || len(availablePeers) == 0 {
		return result
	}

	// Create a list of peer IDs
	peerIDs := make([]string, 0, len(availablePeers))
	for peerID := range availablePeers {
		peerIDs = append(peerIDs, peerID)
	}

	// Assign chunks to peers in a round-robin fashion
	for i, chunkIndex := range neededChunks {
		peerID := peerIDs[i%len(peerIDs)]
		result[peerID] = append(result[peerID], chunkIndex)
	}

	return result
}

// RarestFirstStrategy implements a rarest-first chunk selection strategy
type RarestFirstStrategy struct{}

// SelectChunksFromPeers implements the ChunkSelectionStrategy interface
func (s *RarestFirstStrategy) SelectChunksFromPeers(fileID string, neededChunks []int, availablePeers map[string][]int) map[string][]int {
	result := make(map[string][]int)
	if len(neededChunks) == 0 || len(availablePeers) == 0 {
		return result
	}

	// Count how many peers have each chunk
	chunkCounts := make(map[int]int)
	chunkToPeers := make(map[int][]string)

	for peerID, chunks := range availablePeers {
		for _, chunk := range chunks {
			chunkCounts[chunk]++
			chunkToPeers[chunk] = append(chunkToPeers[chunk], peerID)
		}
	}

	// Sort chunks by rarity (ascending count)
	type chunkRarity struct {
		index int
		count int
	}

	rarities := make([]chunkRarity, 0, len(chunkCounts))
	for chunk, count := range chunkCounts {
		if contains(neededChunks, chunk) {
			rarities = append(rarities, chunkRarity{index: chunk, count: count})
		}
	}

	// Sort by count (rarest first)
	sort.Slice(rarities, func(i, j int) bool {
		return rarities[i].count < rarities[j].count
	})

	// Assign chunks to peers, preferring peers with fewer assigned chunks
	peerAssignments := make(map[string]int)

	for _, rarity := range rarities {
		chunk := rarity.index
		peers := chunkToPeers[chunk]

		if len(peers) == 0 {
			continue
		}

		// Find the peer with the fewest assignments
		var selectedPeer string
		minAssignments := math.MaxInt32

		for _, peer := range peers {
			assignments := peerAssignments[peer]
			if assignments < minAssignments {
				minAssignments = assignments
				selectedPeer = peer
			}
		}

		// Assign the chunk to the selected peer
		result[selectedPeer] = append(result[selectedPeer], chunk)
		peerAssignments[selectedPeer]++
	}

	return result
}

// Helper function to check if a slice contains a value
func contains(slice []int, val int) bool {
	for _, item := range slice {
		if item == val {
			return true
		}
	}
	return false
}

// TransferStats contains statistics about a file transfer
type TransferStats struct {
	StartTime         time.Time           `json:"start_time"`
	EndTime           time.Time           `json:"end_time"`
	BytesTransferred  int64               `json:"bytes_transferred"`
	TotalBytes        int64               `json:"total_bytes"`
	ChunksTransferred int                 `json:"chunks_transferred"`
	TotalChunks       int                 `json:"total_chunks"`
	FailedChunks      int                 `json:"failed_chunks"`
	RetryCount        int                 `json:"retry_count"`
	TransferRate      float64             `json:"transfer_rate_bps"`
	Status            TransferStatus      `json:"status"`
	Error             string              `json:"error,omitempty"`
	ChunkStatus       map[int]ChunkStatus `json:"chunk_status,omitempty"`    // Track status of each chunk
	ChunkSources      map[int]string      `json:"chunk_sources,omitempty"`   // Map of chunk index to peer ID
	ChunkRetries      map[int]int         `json:"chunk_retries,omitempty"`   // Track retries for each chunk
	AvailablePeers    map[string][]int    `json:"available_peers,omitempty"` // Map of peer ID to available chunks
	ResumeData        *TransferResumeData `json:"resume_data,omitempty"`     // Data for resuming transfers
}

// TransferResumeData contains data needed to resume an interrupted transfer
type TransferResumeData struct {
	FileID            string       `json:"file_id"`
	FileName          string       `json:"file_name"`
	FileSize          int64        `json:"file_size"`
	TotalChunks       int          `json:"total_chunks"`
	CompletedChunks   map[int]bool `json:"completed_chunks"`
	LastUpdated       time.Time    `json:"last_updated"`
	EncryptionEnabled bool         `json:"encryption_enabled"`
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

	// Resume support
	resumeDir     string
	resumeEnabled bool

	// Multi-peer support
	peerChunkMap   map[string]map[string][]int // Map of fileID -> peerID -> []chunkIndex
	peerChunkMapMu sync.RWMutex

	// Chunk selection strategy
	chunkSelectionStrategy ChunkSelectionStrategy
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

	// Create resume directory
	resumeDir := filepath.Join(workDir, "resume")
	if err := os.MkdirAll(resumeDir, 0755); err != nil {
		logger.Warn("Failed to create resume directory", zap.Error(err))
	}

	return &TransferManager{
		logger:                 logger,
		chunker:                chunker,
		requestTimeout:         requestTimeout,
		maxRetries:             maxRetries,
		concurrentTransfers:    concurrentTransfers,
		workDir:                workDir,
		activeTransfers:        make(map[string]*TransferStats),
		dataRequestCh:          make(chan *DataRequest, 100),
		dataResponseCh:         make(chan *DataResponse, 100),
		ctx:                    ctx,
		cancel:                 cancel,
		encryptionMgr:          encryptionMgr,
		sessionKeys:            make(map[string][]byte),
		resumeDir:              resumeDir,
		resumeEnabled:          true,
		peerChunkMap:           make(map[string]map[string][]int),
		chunkSelectionStrategy: &RoundRobinStrategy{}, // Default to round-robin
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
	defer tm.transfersMu.Unlock()

	stats, exists := tm.activeTransfers[fileID]
	if !exists {
		tm.logger.Warn("Received data for unknown transfer",
			zap.String("file_id", fileID),
			zap.Int("chunk_index", chunkIndex))
		return
	}

	// Check if this chunk was already processed
	if stats.ChunkStatus != nil && stats.ChunkStatus[chunkIndex] == ChunkStatusTransferred {
		tm.logger.Debug("Chunk already processed, ignoring duplicate",
			zap.String("file_id", fileID),
			zap.Int("chunk_index", chunkIndex))
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

			// Mark this chunk as failed
			if stats.ChunkStatus != nil {
				stats.ChunkStatus[chunkIndex] = ChunkStatusFailed
				stats.FailedChunks++
			}

			// Retry the chunk if possible
			tm.retryChunkIfNeeded(fileID, chunkIndex)
			return
		}

		var err error
		chunkData, err = tm.encryptionMgr.DecryptChunk(chunk.Data, sessionKey)
		if err != nil {
			tm.logger.Error("Failed to decrypt chunk",
				zap.String("file_id", fileID),
				zap.Int("chunk_index", chunkIndex),
				zap.Error(err))

			// Mark this chunk as failed
			if stats.ChunkStatus != nil {
				stats.ChunkStatus[chunkIndex] = ChunkStatusFailed
				stats.FailedChunks++
			}

			// Retry the chunk if possible
			tm.retryChunkIfNeeded(fileID, chunkIndex)
			return
		}
	} else {
		chunkData = chunk.Data
	}

	// Update the transfer status
	stats.BytesTransferred += int64(len(chunkData))
	stats.ChunksTransferred++

	// Update chunk status
	if stats.ChunkStatus != nil {
		stats.ChunkStatus[chunkIndex] = ChunkStatusTransferred
	}

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

	// Update resume data
	if tm.resumeEnabled && stats.ResumeData != nil {
		if stats.ResumeData.CompletedChunks == nil {
			stats.ResumeData.CompletedChunks = make(map[int]bool)
		}
		stats.ResumeData.CompletedChunks[chunkIndex] = true
		stats.ResumeData.LastUpdated = time.Now()

		// Save resume data periodically (every 5 chunks or when transfer is complete)
		if stats.ChunksTransferred%5 == 0 || stats.ChunksTransferred >= stats.TotalChunks {
			go tm.saveResumeData(fileID, stats.ResumeData)
		}
	}

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
	} else {
		// Check if we need to request more chunks
		go tm.checkAndRequestMoreChunks(fileID)
	}
}

// retryChunkIfNeeded attempts to retry a failed chunk if the retry limit hasn't been reached
func (tm *TransferManager) retryChunkIfNeeded(fileID string, chunkIndex int) {
	stats, exists := tm.activeTransfers[fileID]
	if !exists {
		return
	}

	// Check if we've reached the retry limit for this chunk
	retryCount := stats.ChunkRetries[chunkIndex]
	if retryCount >= tm.maxRetries {
		tm.logger.Warn("Max retries reached for chunk",
			zap.String("file_id", fileID),
			zap.Int("chunk_index", chunkIndex),
			zap.Int("retries", retryCount))
		return
	}

	// Increment retry count
	stats.ChunkRetries[chunkIndex] = retryCount + 1
	stats.RetryCount++

	// Get the peer that has this chunk
	var peerID string
	if sourcePeer, exists := stats.ChunkSources[chunkIndex]; exists {
		peerID = sourcePeer
	} else if len(stats.AvailablePeers) > 0 {
		// Pick any peer that has chunks available
		for peer := range stats.AvailablePeers {
			peerID = peer
			break
		}
	}

	if peerID == "" {
		tm.logger.Error("No peer available to retry chunk",
			zap.String("file_id", fileID),
			zap.Int("chunk_index", chunkIndex))
		return
	}

	// Set chunk status back to pending
	stats.ChunkStatus[chunkIndex] = ChunkStatusPending

	// Request the chunk again
	go func() {
		time.Sleep(100 * time.Millisecond) // Small delay before retry
		err := tm.RequestDataChunk(peerID, fileID, chunkIndex)
		if err != nil {
			tm.logger.Error("Failed to retry chunk request",
				zap.String("file_id", fileID),
				zap.Int("chunk_index", chunkIndex),
				zap.Error(err))
		}
	}()
}

// checkAndRequestMoreChunks checks if there are pending chunks and requests them
func (tm *TransferManager) checkAndRequestMoreChunks(fileID string) {
	tm.transfersMu.RLock()
	stats, exists := tm.activeTransfers[fileID]
	if !exists || stats.Status != TransferStatusInProgress {
		tm.transfersMu.RUnlock()
		return
	}

	// Find pending chunks
	pendingChunks := make([]int, 0)
	for chunkIndex, status := range stats.ChunkStatus {
		if status == ChunkStatusPending {
			pendingChunks = append(pendingChunks, chunkIndex)
		}
	}

	// Get available peers
	availablePeers := make(map[string][]int)
	for peerID, chunks := range stats.AvailablePeers {
		availablePeers[peerID] = chunks
	}
	tm.transfersMu.RUnlock()

	if len(pendingChunks) == 0 || len(availablePeers) == 0 {
		return
	}

	// Use the chunk selection strategy to decide which chunks to request from which peers
	chunkAssignments := tm.chunkSelectionStrategy.SelectChunksFromPeers(fileID, pendingChunks, availablePeers)

	// Request chunks from peers
	for peerID, chunks := range chunkAssignments {
		for _, chunkIndex := range chunks {
			err := tm.RequestDataChunk(peerID, fileID, chunkIndex)
			if err != nil {
				tm.logger.Error("Failed to request additional chunk",
					zap.String("file_id", fileID),
					zap.Int("chunk_index", chunkIndex),
					zap.Error(err))
				continue
			}

			// Update chunk status and source
			tm.transfersMu.Lock()
			if stats, exists := tm.activeTransfers[fileID]; exists {
				stats.ChunkStatus[chunkIndex] = ChunkStatusInProgress
				stats.ChunkSources[chunkIndex] = peerID
			}
			tm.transfersMu.Unlock()

			// Sleep to avoid overwhelming the peer
			time.Sleep(10 * time.Millisecond)
		}
	}
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

	// Check if we have resume data for this file
	var resumeData *TransferResumeData
	if tm.resumeEnabled {
		var err error
		resumeData, err = tm.loadResumeData(fileID)
		if err != nil && !os.IsNotExist(err) {
			tm.logger.Warn("Failed to load resume data",
				zap.String("file_id", fileID),
				zap.Error(err))
		}
	}

	// Create a new transfer
	stats := &TransferStats{
		StartTime:      time.Now(),
		TotalBytes:     fileSize,
		TotalChunks:    totalChunks,
		Status:         TransferStatusInProgress,
		ChunkStatus:    make(map[int]ChunkStatus, totalChunks),
		ChunkSources:   make(map[int]string),
		ChunkRetries:   make(map[int]int),
		AvailablePeers: make(map[string][]int),
		ResumeData:     resumeData,
	}

	// Initialize chunk status
	for i := 0; i < totalChunks; i++ {
		stats.ChunkStatus[i] = ChunkStatusPending
	}

	// If we have resume data, mark completed chunks
	if resumeData != nil && resumeData.CompletedChunks != nil {
		for chunkIndex, completed := range resumeData.CompletedChunks {
			if completed {
				stats.ChunkStatus[chunkIndex] = ChunkStatusTransferred
				stats.ChunksTransferred++

				// Estimate bytes transferred based on chunk index and total size
				chunkSize := fileSize / int64(totalChunks)
				if chunkIndex == totalChunks-1 {
					// Last chunk might be smaller
					stats.BytesTransferred += fileSize - (int64(totalChunks-1) * chunkSize)
				} else {
					stats.BytesTransferred += chunkSize
				}
			}
		}

		tm.logger.Info("Resuming transfer",
			zap.String("file_id", fileID),
			zap.Int("completed_chunks", stats.ChunksTransferred),
			zap.Int("total_chunks", totalChunks))
	}

	// Register the initial peer as having all chunks
	tm.registerPeerChunks(fileID, peerID, generateSequence(0, totalChunks-1))

	// Add the peer to available peers for this transfer
	stats.AvailablePeers[peerID] = generateSequence(0, totalChunks-1)

	// Store the transfer
	tm.activeTransfers[fileID] = stats

	// Start requesting chunks
	go tm.downloadChunks(ctx, fileID, fileName)

	return stats, nil
}

// Helper function to generate a sequence of integers
func generateSequence(start, end int) []int {
	result := make([]int, end-start+1)
	for i := range result {
		result[i] = start + i
	}
	return result
}

// downloadChunks handles the download of chunks for a file
func (tm *TransferManager) downloadChunks(ctx context.Context, fileID string, fileName string) {
	tm.transfersMu.RLock()
	stats, exists := tm.activeTransfers[fileID]
	if !exists {
		tm.transfersMu.RUnlock()
		tm.logger.Error("Transfer not found for downloading chunks", zap.String("file_id", fileID))
		return
	}

	// Get the list of chunks that need to be downloaded
	neededChunks := make([]int, 0)
	for chunkIndex, status := range stats.ChunkStatus {
		if status == ChunkStatusPending {
			neededChunks = append(neededChunks, chunkIndex)
		}
	}

	// Get available peers for this file
	availablePeers := make(map[string][]int)
	for peerID, chunks := range stats.AvailablePeers {
		availablePeers[peerID] = chunks
	}
	tm.transfersMu.RUnlock()

	if len(neededChunks) == 0 {
		tm.logger.Info("No chunks needed, transfer already complete", zap.String("file_id", fileID))
		return
	}

	// Use the chunk selection strategy to decide which chunks to request from which peers
	chunkAssignments := tm.chunkSelectionStrategy.SelectChunksFromPeers(fileID, neededChunks, availablePeers)

	// Request chunks from peers
	for peerID, chunks := range chunkAssignments {
		for _, chunkIndex := range chunks {
			// Create a context with timeout for this chunk request
			err := tm.RequestDataChunk(peerID, fileID, chunkIndex)
			if err != nil {
				tm.logger.Error("Failed to request chunk",
					zap.String("file_id", fileID),
					zap.Int("chunk_index", chunkIndex),
					zap.Error(err))
				continue
			}

			// Update chunk status and source
			tm.transfersMu.Lock()
			if stats, exists := tm.activeTransfers[fileID]; exists {
				stats.ChunkStatus[chunkIndex] = ChunkStatusInProgress
				stats.ChunkSources[chunkIndex] = peerID
			}
			tm.transfersMu.Unlock()

			// Sleep to avoid overwhelming the peer
			time.Sleep(10 * time.Millisecond)
		}
	}
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

// loadResumeData loads resume data for a file from disk
func (tm *TransferManager) loadResumeData(fileID string) (*TransferResumeData, error) {
	resumePath := filepath.Join(tm.resumeDir, fileID+".resume")
	data, err := os.ReadFile(resumePath)
	if err != nil {
		return nil, err
	}

	var resumeData TransferResumeData
	err = json.Unmarshal(data, &resumeData)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal resume data: %w", err)
	}

	return &resumeData, nil
}

// saveResumeData saves resume data for a file to disk
func (tm *TransferManager) saveResumeData(fileID string, resumeData *TransferResumeData) error {
	if !tm.resumeEnabled {
		return nil
	}

	resumePath := filepath.Join(tm.resumeDir, fileID+".resume")
	data, err := json.Marshal(resumeData)
	if err != nil {
		return fmt.Errorf("failed to marshal resume data: %w", err)
	}

	err = os.WriteFile(resumePath, data, 0644)
	if err != nil {
		return fmt.Errorf("failed to write resume data: %w", err)
	}

	return nil
}

// registerPeerChunks registers which chunks a peer has available
func (tm *TransferManager) registerPeerChunks(fileID string, peerID string, chunks []int) {
	// First, update the peer chunk map
	tm.peerChunkMapMu.Lock()

	// Initialize the map for this file if it doesn't exist
	if tm.peerChunkMap == nil {
		tm.peerChunkMap = make(map[string]map[string][]int)
	}

	if _, exists := tm.peerChunkMap[fileID]; !exists {
		tm.peerChunkMap[fileID] = make(map[string][]int)
	}

	// Store the chunks for this peer
	tm.peerChunkMap[fileID][peerID] = chunks
	tm.peerChunkMapMu.Unlock()

	// Then, update the transfer's available peers if it exists
	tm.transfersMu.Lock()
	defer tm.transfersMu.Unlock()

	if stats, exists := tm.activeTransfers[fileID]; exists {
		if stats.AvailablePeers == nil {
			stats.AvailablePeers = make(map[string][]int)
		}
		stats.AvailablePeers[peerID] = chunks
	}
}

// GetAvailablePeersForFile returns a map of peer IDs to the chunks they have available
func (tm *TransferManager) GetAvailablePeersForFile(fileID string) map[string][]int {
	tm.peerChunkMapMu.RLock()
	defer tm.peerChunkMapMu.RUnlock()

	if fileMap, exists := tm.peerChunkMap[fileID]; exists {
		// Create a copy of the map
		result := make(map[string][]int, len(fileMap))
		for peerID, chunks := range fileMap {
			chunksCopy := make([]int, len(chunks))
			copy(chunksCopy, chunks)
			result[peerID] = chunksCopy
		}
		return result
	}

	return make(map[string][]int)
}

// AddPeerForTransfer adds a peer as a source for a file transfer
func (tm *TransferManager) AddPeerForTransfer(fileID string, peerID string, availableChunks []int) error {
	tm.transfersMu.Lock()
	defer tm.transfersMu.Unlock()

	// Check if the transfer exists
	stats, exists := tm.activeTransfers[fileID]
	if !exists {
		return fmt.Errorf("no active transfer for file ID %s", fileID)
	}

	// Register the peer's chunks
	tm.registerPeerChunks(fileID, peerID, availableChunks)

	// Update the transfer's available peers
	if stats.AvailablePeers == nil {
		stats.AvailablePeers = make(map[string][]int)
	}
	stats.AvailablePeers[peerID] = availableChunks

	tm.logger.Info("Added peer for transfer",
		zap.String("file_id", fileID),
		zap.String("peer_id", peerID),
		zap.Int("available_chunks", len(availableChunks)))

	// If the transfer is in progress, check if we need to request more chunks
	if stats.Status == TransferStatusInProgress {
		go tm.checkAndRequestMoreChunks(fileID)
	}

	return nil
}

// SetChunkSelectionStrategy sets the strategy used for selecting chunks from peers
func (tm *TransferManager) SetChunkSelectionStrategy(strategy ChunkSelectionStrategy) {
	tm.chunkSelectionStrategy = strategy
}

// GetRarestFirstStrategy returns a new instance of the RarestFirstStrategy
func GetRarestFirstStrategy() ChunkSelectionStrategy {
	return &RarestFirstStrategy{}
}

// GetRoundRobinStrategy returns a new instance of the RoundRobinStrategy
func GetRoundRobinStrategy() ChunkSelectionStrategy {
	return &RoundRobinStrategy{}
}
