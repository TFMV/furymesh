package file

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.uber.org/zap"
)

// StorageManager handles persistent storage of file metadata and chunks
type StorageManager struct {
	logger        *zap.Logger
	baseDir       string
	metadataDir   string
	chunksDir     string
	metadataMu    sync.RWMutex
	metadataCache map[string]*ChunkMetadata
}

// StorageConfig contains configuration for the storage manager
type StorageConfig struct {
	BaseDir string
}

// NewStorageManager creates a new StorageManager
func NewStorageManager(logger *zap.Logger, config StorageConfig) (*StorageManager, error) {
	baseDir := config.BaseDir
	if baseDir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get user home directory: %w", err)
		}
		baseDir = filepath.Join(homeDir, ".furymesh")
	}

	metadataDir := filepath.Join(baseDir, "metadata")
	chunksDir := filepath.Join(baseDir, "chunks")

	// Create directories if they don't exist
	for _, dir := range []string{baseDir, metadataDir, chunksDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	sm := &StorageManager{
		logger:        logger,
		baseDir:       baseDir,
		metadataDir:   metadataDir,
		chunksDir:     chunksDir,
		metadataCache: make(map[string]*ChunkMetadata),
	}

	// Load existing metadata
	if err := sm.loadMetadata(); err != nil {
		logger.Warn("Failed to load existing metadata", zap.Error(err))
	}

	return sm, nil
}

// loadMetadata loads existing metadata from disk
func (sm *StorageManager) loadMetadata() error {
	sm.metadataMu.Lock()
	defer sm.metadataMu.Unlock()

	// Read metadata directory
	entries, err := os.ReadDir(sm.metadataDir)
	if err != nil {
		return fmt.Errorf("failed to read metadata directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		// Read metadata file
		metadataPath := filepath.Join(sm.metadataDir, entry.Name())
		data, err := os.ReadFile(metadataPath)
		if err != nil {
			sm.logger.Warn("Failed to read metadata file",
				zap.String("path", metadataPath),
				zap.Error(err))
			continue
		}

		// Parse metadata
		var metadata ChunkMetadata
		if err := json.Unmarshal(data, &metadata); err != nil {
			sm.logger.Warn("Failed to parse metadata file",
				zap.String("path", metadataPath),
				zap.Error(err))
			continue
		}

		// Add to cache
		sm.metadataCache[metadata.FileID] = &metadata
		sm.logger.Debug("Loaded metadata",
			zap.String("file_id", metadata.FileID),
			zap.String("file_name", metadata.FileName))
	}

	sm.logger.Info("Loaded metadata from disk",
		zap.Int("file_count", len(sm.metadataCache)))

	return nil
}

// SaveMetadata saves file metadata to disk
func (sm *StorageManager) SaveMetadata(metadata *ChunkMetadata) error {
	sm.metadataMu.Lock()
	defer sm.metadataMu.Unlock()

	// Add to cache
	sm.metadataCache[metadata.FileID] = metadata

	// Serialize metadata
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize metadata: %w", err)
	}

	// Write to file
	metadataPath := filepath.Join(sm.metadataDir, fmt.Sprintf("%s.json", metadata.FileID))
	if err := os.WriteFile(metadataPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write metadata file: %w", err)
	}

	sm.logger.Debug("Saved metadata",
		zap.String("file_id", metadata.FileID),
		zap.String("file_name", metadata.FileName),
		zap.String("path", metadataPath))

	return nil
}

// GetMetadata retrieves file metadata from cache or disk
func (sm *StorageManager) GetMetadata(fileID string) (*ChunkMetadata, error) {
	sm.metadataMu.RLock()
	metadata, exists := sm.metadataCache[fileID]
	sm.metadataMu.RUnlock()

	if exists {
		return metadata, nil
	}

	// Try to load from disk
	metadataPath := filepath.Join(sm.metadataDir, fmt.Sprintf("%s.json", fileID))
	data, err := os.ReadFile(metadataPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read metadata file: %w", err)
	}

	// Parse metadata
	var loadedMetadata ChunkMetadata
	if err := json.Unmarshal(data, &loadedMetadata); err != nil {
		return nil, fmt.Errorf("failed to parse metadata file: %w", err)
	}

	// Add to cache
	sm.metadataMu.Lock()
	sm.metadataCache[fileID] = &loadedMetadata
	sm.metadataMu.Unlock()

	return &loadedMetadata, nil
}

// DeleteMetadata deletes file metadata from disk and cache
func (sm *StorageManager) DeleteMetadata(fileID string) error {
	sm.metadataMu.Lock()
	defer sm.metadataMu.Unlock()

	// Remove from cache
	delete(sm.metadataCache, fileID)

	// Remove from disk
	metadataPath := filepath.Join(sm.metadataDir, fmt.Sprintf("%s.json", fileID))
	if err := os.Remove(metadataPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete metadata file: %w", err)
	}

	sm.logger.Debug("Deleted metadata",
		zap.String("file_id", fileID),
		zap.String("path", metadataPath))

	return nil
}

// ListMetadata returns a list of all file metadata
func (sm *StorageManager) ListMetadata() []*ChunkMetadata {
	sm.metadataMu.RLock()
	defer sm.metadataMu.RUnlock()

	metadata := make([]*ChunkMetadata, 0, len(sm.metadataCache))
	for _, m := range sm.metadataCache {
		metadata = append(metadata, m)
	}

	return metadata
}

// SaveChunk saves a chunk to disk
func (sm *StorageManager) SaveChunk(fileID string, chunkIndex int, data []byte) error {
	// Create directory for file chunks if it doesn't exist
	chunkDir := filepath.Join(sm.chunksDir, fileID)
	if err := os.MkdirAll(chunkDir, 0755); err != nil {
		return fmt.Errorf("failed to create chunk directory: %w", err)
	}

	// Write chunk to file
	chunkPath := filepath.Join(chunkDir, fmt.Sprintf("%d.chunk", chunkIndex))
	if err := os.WriteFile(chunkPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write chunk file: %w", err)
	}

	sm.logger.Debug("Saved chunk",
		zap.String("file_id", fileID),
		zap.Int("chunk_index", chunkIndex),
		zap.Int("data_size", len(data)),
		zap.String("path", chunkPath))

	return nil
}

// GetChunk retrieves a chunk from disk
func (sm *StorageManager) GetChunk(fileID string, chunkIndex int) ([]byte, error) {
	chunkPath := filepath.Join(sm.chunksDir, fileID, fmt.Sprintf("%d.chunk", chunkIndex))
	data, err := os.ReadFile(chunkPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read chunk file: %w", err)
	}

	sm.logger.Debug("Retrieved chunk",
		zap.String("file_id", fileID),
		zap.Int("chunk_index", chunkIndex),
		zap.Int("data_size", len(data)),
		zap.String("path", chunkPath))

	return data, nil
}

// DeleteChunk deletes a chunk from disk
func (sm *StorageManager) DeleteChunk(fileID string, chunkIndex int) error {
	chunkPath := filepath.Join(sm.chunksDir, fileID, fmt.Sprintf("%d.chunk", chunkIndex))
	if err := os.Remove(chunkPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete chunk file: %w", err)
	}

	sm.logger.Debug("Deleted chunk",
		zap.String("file_id", fileID),
		zap.Int("chunk_index", chunkIndex),
		zap.String("path", chunkPath))

	return nil
}

// DeleteAllChunks deletes all chunks for a file
func (sm *StorageManager) DeleteAllChunks(fileID string) error {
	chunkDir := filepath.Join(sm.chunksDir, fileID)
	if err := os.RemoveAll(chunkDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete chunk directory: %w", err)
	}

	sm.logger.Debug("Deleted all chunks",
		zap.String("file_id", fileID),
		zap.String("path", chunkDir))

	return nil
}

// CleanupExpiredFiles removes files that have expired
func (sm *StorageManager) CleanupExpiredFiles(maxAge time.Duration) error {
	sm.metadataMu.Lock()
	defer sm.metadataMu.Unlock()

	now := time.Now()
	expiredCount := 0

	for fileID, metadata := range sm.metadataCache {
		// Check if file has expired
		fileAge := now.Sub(time.Unix(0, 0)) // TODO: Add creation time to metadata
		if fileAge > maxAge {
			// Delete chunks
			chunkDir := filepath.Join(sm.chunksDir, fileID)
			if err := os.RemoveAll(chunkDir); err != nil && !os.IsNotExist(err) {
				sm.logger.Warn("Failed to delete chunk directory",
					zap.String("file_id", fileID),
					zap.String("path", chunkDir),
					zap.Error(err))
			}

			// Delete metadata file
			metadataPath := filepath.Join(sm.metadataDir, fmt.Sprintf("%s.json", fileID))
			if err := os.Remove(metadataPath); err != nil && !os.IsNotExist(err) {
				sm.logger.Warn("Failed to delete metadata file",
					zap.String("file_id", fileID),
					zap.String("path", metadataPath),
					zap.Error(err))
			}

			// Remove from cache
			delete(sm.metadataCache, fileID)
			expiredCount++

			sm.logger.Info("Deleted expired file",
				zap.String("file_id", fileID),
				zap.String("file_name", metadata.FileName))
		}
	}

	sm.logger.Info("Cleaned up expired files",
		zap.Int("expired_count", expiredCount),
		zap.Duration("max_age", maxAge))

	return nil
}

// GetStorageStats returns statistics about storage usage
func (sm *StorageManager) GetStorageStats() (map[string]interface{}, error) {
	sm.metadataMu.RLock()
	defer sm.metadataMu.RUnlock()

	var totalSize int64
	var totalChunks int
	fileCount := len(sm.metadataCache)

	for _, metadata := range sm.metadataCache {
		totalSize += metadata.FileSize
		totalChunks += metadata.TotalChunks
	}

	// Get disk usage
	var diskUsage int64
	err := filepath.Walk(sm.baseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			diskUsage += info.Size()
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to calculate disk usage: %w", err)
	}

	return map[string]interface{}{
		"file_count":   fileCount,
		"total_size":   totalSize,
		"total_chunks": totalChunks,
		"disk_usage":   diskUsage,
		"base_dir":     sm.baseDir,
	}, nil
}
