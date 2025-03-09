package node

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/TFMV/furymesh/crypto"
	"github.com/TFMV/furymesh/file"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

// FileManager integrates the file transfer system with the node
type FileManager struct {
	logger          *zap.Logger
	chunker         *file.Chunker
	storageManager  *file.StorageManager
	transferManager *file.TransferManager
	webrtcTransport *file.WebRTCTransport

	// Map of peer IDs to their available files
	peerFiles   map[string][]string
	peerFilesMu sync.RWMutex

	// Context for cancellation
	ctx    context.Context
	cancel context.CancelFunc
}

// NewFileManager creates a new FileManager
func NewFileManager(logger *zap.Logger) (*FileManager, error) {
	// Get configuration values
	workDir := viper.GetString("storage.work_dir")
	if workDir == "" {
		homeDir, err := filepath.Abs(".")
		if err != nil {
			return nil, fmt.Errorf("failed to get current directory: %w", err)
		}
		workDir = filepath.Join(homeDir, ".furymesh", "work")
	}

	baseDir := viper.GetString("storage.base_dir")
	if baseDir == "" {
		homeDir, err := filepath.Abs(".")
		if err != nil {
			return nil, fmt.Errorf("failed to get current directory: %w", err)
		}
		baseDir = filepath.Join(homeDir, ".furymesh")
	}

	chunkSize := viper.GetInt("storage.chunk_size")
	if chunkSize <= 0 {
		chunkSize = file.DefaultChunkSize
	}

	requestTimeout := viper.GetDuration("transfer.request_timeout")
	if requestTimeout <= 0 {
		requestTimeout = file.DefaultRequestTimeout
	}

	maxRetries := viper.GetInt("transfer.max_retries")
	if maxRetries < 0 {
		maxRetries = file.DefaultMaxRetries
	}

	concurrentTransfers := viper.GetInt("transfer.concurrent_transfers")
	if concurrentTransfers <= 0 {
		concurrentTransfers = file.DefaultConcurrentTransfers
	}

	// Create chunker
	chunker, err := file.NewChunker(logger, workDir, chunkSize)
	if err != nil {
		return nil, fmt.Errorf("failed to create chunker: %w", err)
	}

	// Create storage manager
	storageConfig := file.StorageConfig{
		BaseDir: baseDir,
	}
	storageManager, err := file.NewStorageManager(logger, storageConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create storage manager: %w", err)
	}

	// Initialize encryption manager if enabled
	var encryptionMgr *crypto.EncryptionManager
	if viper.GetBool("encryption.enabled") {
		keysDir := viper.GetString("encryption.keys_dir")
		if keysDir == "" {
			homeDir, err := filepath.Abs(".")
			if err != nil {
				return nil, fmt.Errorf("failed to get current directory: %w", err)
			}
			keysDir = filepath.Join(homeDir, ".furymesh", "keys")
		}

		var err error
		encryptionMgr, err = crypto.NewEncryptionManager(logger, keysDir)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize encryption manager: %w", err)
		}

		logger.Info("Encryption enabled", zap.String("keys_dir", keysDir))
	}

	// Create transfer manager
	transferManager := file.NewTransferManager(
		logger,
		chunker,
		workDir,
		requestTimeout,
		maxRetries,
		concurrentTransfers,
		encryptionMgr, // Pass the encryption manager (can be nil if encryption is disabled)
	)

	// Create WebRTC transport
	stunServers := viper.GetStringSlice("webrtc.stun_servers")
	if len(stunServers) == 0 {
		stunServers = []string{"stun:stun.l.google.com:19302"}
	}

	webrtcConfig := file.WebRTCConfig{
		ICEServers: stunServers,
	}

	webrtcTransport := file.NewWebRTCTransport(
		logger,
		transferManager,
		storageManager,
		webrtcConfig,
	)

	ctx, cancel := context.WithCancel(context.Background())

	fm := &FileManager{
		logger:          logger,
		chunker:         chunker,
		storageManager:  storageManager,
		transferManager: transferManager,
		webrtcTransport: webrtcTransport,
		peerFiles:       make(map[string][]string),
		ctx:             ctx,
		cancel:          cancel,
	}

	// Set up callbacks
	webrtcTransport.SetPeerCallbacks(
		fm.handlePeerConnected,
		fm.handlePeerDisconnected,
	)

	return fm, nil
}

// Start starts the file manager
func (fm *FileManager) Start() {
	fm.logger.Info("Starting file manager")

	// Start the transfer manager
	fm.transferManager.Start()

	// Start a goroutine to clean up completed transfers
	go fm.cleanupCompletedTransfers()
}

// Stop stops the file manager
func (fm *FileManager) Stop() {
	fm.logger.Info("Stopping file manager")

	// Cancel the context
	fm.cancel()

	// Stop the transfer manager
	fm.transferManager.Stop()

	// Close the WebRTC transport
	fm.webrtcTransport.Close()
}

// cleanupCompletedTransfers periodically cleans up completed transfers
func (fm *FileManager) cleanupCompletedTransfers() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-fm.ctx.Done():
			return
		case <-ticker.C:
			fm.transferManager.CleanupCompletedTransfers()
		}
	}
}

// handlePeerConnected is called when a peer connects
func (fm *FileManager) handlePeerConnected(peerID string) {
	fm.logger.Info("Peer connected", zap.String("peer_id", peerID))

	// Initialize peer files map
	fm.peerFilesMu.Lock()
	fm.peerFiles[peerID] = []string{}
	fm.peerFilesMu.Unlock()

	// Send our available files to the peer
	go fm.sendAvailableFiles(peerID)
}

// handlePeerDisconnected is called when a peer disconnects
func (fm *FileManager) handlePeerDisconnected(peerID string) {
	fm.logger.Info("Peer disconnected", zap.String("peer_id", peerID))

	// Remove the peer's files
	fm.peerFilesMu.Lock()
	delete(fm.peerFiles, peerID)
	fm.peerFilesMu.Unlock()
}

// sendAvailableFiles sends our available files to a peer
func (fm *FileManager) sendAvailableFiles(peerID string) {
	// Get our available files
	files := fm.storageManager.ListMetadata()

	// Create a list of file IDs
	fileIDs := make([]string, 0, len(files))
	for _, metadata := range files {
		fileIDs = append(fileIDs, metadata.FileID)
	}

	// Send the list to the peer
	message := map[string]interface{}{
		"type":      "available_files",
		"files":     fileIDs,
		"timestamp": time.Now().Unix(),
	}

	// Use the WebRTC transport to send the message
	if err := fm.webrtcTransport.SendDataChannelMessage(peerID, message); err != nil {
		fm.logger.Error("Failed to send available files",
			zap.String("peer_id", peerID),
			zap.Error(err))
	}
}

// updatePeerFiles updates the list of files available from a peer
func (fm *FileManager) updatePeerFiles(peerID string, files []string) {
	fm.peerFilesMu.Lock()
	fm.peerFiles[peerID] = files
	fm.peerFilesMu.Unlock()

	fm.logger.Info("Updated peer files",
		zap.String("peer_id", peerID),
		zap.Int("file_count", len(files)))
}

// GetPeerFiles returns the list of files available from a peer
func (fm *FileManager) GetPeerFiles(peerID string) []string {
	fm.peerFilesMu.RLock()
	defer fm.peerFilesMu.RUnlock()

	files, exists := fm.peerFiles[peerID]
	if !exists {
		return []string{}
	}

	return files
}

// GetAllPeerFiles returns a map of peer IDs to their available files
func (fm *FileManager) GetAllPeerFiles() map[string][]string {
	fm.peerFilesMu.RLock()
	defer fm.peerFilesMu.RUnlock()

	// Create a copy of the map
	result := make(map[string][]string, len(fm.peerFiles))
	for peerID, files := range fm.peerFiles {
		filesCopy := make([]string, len(files))
		copy(filesCopy, files)
		result[peerID] = filesCopy
	}

	return result
}

// ChunkFile chunks a file and stores it
func (fm *FileManager) ChunkFile(filePath string) (*file.ChunkMetadata, error) {
	// Chunk the file
	metadata, err := fm.chunker.ChunkFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to chunk file: %w", err)
	}

	// Save metadata
	if err := fm.storageManager.SaveMetadata(metadata); err != nil {
		return nil, fmt.Errorf("failed to save metadata: %w", err)
	}

	// Save chunks
	for i := 0; i < metadata.TotalChunks; i++ {
		chunk, err := fm.chunker.GetChunk(metadata.FileID, i)
		if err != nil {
			return nil, fmt.Errorf("failed to get chunk %d: %w", i, err)
		}

		if err := fm.storageManager.SaveChunk(metadata.FileID, i, chunk.Data); err != nil {
			return nil, fmt.Errorf("failed to save chunk %d: %w", i, err)
		}
	}

	fm.logger.Info("File chunked and stored",
		zap.String("file_id", metadata.FileID),
		zap.String("file_name", metadata.FileName),
		zap.Int64("file_size", metadata.FileSize),
		zap.Int("total_chunks", metadata.TotalChunks))

	return metadata, nil
}

// ReassembleFile reassembles a file from chunks
func (fm *FileManager) ReassembleFile(fileID, outputPath string) error {
	// Get metadata
	metadata, err := fm.storageManager.GetMetadata(fileID)
	if err != nil {
		return fmt.Errorf("failed to get metadata: %w", err)
	}

	// Load chunks into chunker
	for i := 0; i < metadata.TotalChunks; i++ {
		data, err := fm.storageManager.GetChunk(fileID, i)
		if err != nil {
			return fmt.Errorf("failed to get chunk %d: %w", i, err)
		}

		// Create chunk directory
		chunkDir := filepath.Join(fm.chunker.GetWorkDir(), fileID)
		if err := os.MkdirAll(chunkDir, 0755); err != nil {
			return fmt.Errorf("failed to create chunk directory: %w", err)
		}

		// Write chunk to file
		chunkPath := filepath.Join(chunkDir, fmt.Sprintf("%d.chunk", i))
		if err := os.WriteFile(chunkPath, data, 0644); err != nil {
			return fmt.Errorf("failed to write chunk file: %w", err)
		}
	}

	// Reassemble the file
	if err := fm.chunker.ReassembleFile(fileID, outputPath); err != nil {
		return fmt.Errorf("failed to reassemble file: %w", err)
	}

	fm.logger.Info("File reassembled",
		zap.String("file_id", fileID),
		zap.String("output_path", outputPath),
		zap.Int64("file_size", metadata.FileSize))

	return nil
}

// RequestFileFromPeer requests a file from a peer
func (fm *FileManager) RequestFileFromPeer(ctx context.Context, peerID, fileID string) error {
	// Check if we already have the file
	_, err := fm.storageManager.GetMetadata(fileID)
	if err == nil {
		return fmt.Errorf("file already exists locally")
	}

	// Check if the peer exists
	fm.peerFilesMu.RLock()
	peerFiles, peerExists := fm.peerFiles[peerID]
	fm.peerFilesMu.RUnlock()
	if !peerExists {
		return fmt.Errorf("peer not connected: %s", peerID)
	}

	// Check if the peer has the file
	fileExists := false
	for _, f := range peerFiles {
		if f == fileID {
			fileExists = true
			break
		}
	}
	if !fileExists {
		return fmt.Errorf("file not available from peer: %s", fileID)
	}

	// Request the file
	if err := fm.webrtcTransport.RequestFile(ctx, peerID, fileID); err != nil {
		return fmt.Errorf("failed to request file: %w", err)
	}

	fm.logger.Info("Requested file from peer",
		zap.String("peer_id", peerID),
		zap.String("file_id", fileID))

	return nil
}

// GetTransferStats returns statistics for a transfer
func (fm *FileManager) GetTransferStats(fileID string) (*file.TransferStats, error) {
	return fm.transferManager.GetTransferStats(fileID)
}

// ListAvailableFiles returns a list of files available locally
func (fm *FileManager) ListAvailableFiles() []*file.ChunkMetadata {
	return fm.storageManager.ListMetadata()
}

// DeleteFile deletes a file and its chunks
func (fm *FileManager) DeleteFile(fileID string) error {
	// Delete chunks
	if err := fm.storageManager.DeleteAllChunks(fileID); err != nil {
		return fmt.Errorf("failed to delete chunks: %w", err)
	}

	// Delete metadata
	if err := fm.storageManager.DeleteMetadata(fileID); err != nil {
		return fmt.Errorf("failed to delete metadata: %w", err)
	}

	fm.logger.Info("File deleted", zap.String("file_id", fileID))

	return nil
}

// GetStorageStats returns statistics about storage usage
func (fm *FileManager) GetStorageStats() (map[string]interface{}, error) {
	return fm.storageManager.GetStorageStats()
}
