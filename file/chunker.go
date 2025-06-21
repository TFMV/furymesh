package file

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sync"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

const (
	// DefaultChunkSize is the default size of each chunk in bytes (1MB)
	DefaultChunkSize = 1024 * 1024
	// MaxConcurrentChunks is the maximum number of chunks to process concurrently
	MaxConcurrentChunks = 10
)

// ChunkMetadata contains metadata about a file's chunks
type ChunkMetadata struct {
	FileID      string   `json:"file_id"`
	FileName    string   `json:"file_name"`
	FilePath    string   `json:"file_path"`
	FileSize    int64    `json:"file_size"`
	ChunkSize   int      `json:"chunk_size"`
	TotalChunks int      `json:"total_chunks"`
	ChunkHashes []string `json:"chunk_hashes"`
	FileHash    string   `json:"file_hash"`
}

// Chunker handles splitting files into chunks and reassembling them
type Chunker struct {
	logger    *zap.Logger
	chunkSize int
	workDir   string
	mu        sync.RWMutex
	metadata  map[string]*ChunkMetadata // Map of fileID to metadata
}

// NewChunker creates a new Chunker instance
func NewChunker(logger *zap.Logger, workDir string, chunkSize int) (*Chunker, error) {
	if chunkSize <= 0 {
		chunkSize = DefaultChunkSize
	}

	// Ensure the working directory exists
	if err := os.MkdirAll(workDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create working directory: %w", err)
	}

	return &Chunker{
		logger:    logger,
		chunkSize: chunkSize,
		workDir:   workDir,
		metadata:  make(map[string]*ChunkMetadata),
	}, nil
}

// ChunkFile splits a file into chunks and returns metadata about the chunks
func (c *Chunker) ChunkFile(filePath string) (*ChunkMetadata, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to get file info: %w", err)
	}

	fileSize := fileInfo.Size()
	totalChunks := int(math.Ceil(float64(fileSize) / float64(c.chunkSize)))

	// Generate a unique ID for this file
	fileID := uuid.New().String()

	// Create metadata
	metadata := &ChunkMetadata{
		FileID:      fileID,
		FileName:    filepath.Base(filePath),
		FilePath:    filePath,
		FileSize:    fileSize,
		ChunkSize:   c.chunkSize,
		TotalChunks: totalChunks,
		ChunkHashes: make([]string, totalChunks),
	}

	// Create a directory for this file's chunks
	chunkDir := filepath.Join(c.workDir, fileID)
	if err := os.MkdirAll(chunkDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create chunk directory: %w", err)
	}

	// Process the file sequentially while computing the overall file hash.
	// This avoids reading the file twice and keeps memory usage predictable.
	fileHasher := sha256.New()

	for i := 0; i < totalChunks; i++ {
		offset := int64(i * c.chunkSize)
		size := c.chunkSize
		if offset+int64(size) > fileSize {
			size = int(fileSize - offset)
		}

		buffer := make([]byte, size)
		if _, err := file.ReadAt(buffer, offset); err != nil && err != io.EOF {
			return nil, fmt.Errorf("failed to read chunk %d: %w", i, err)
		}

		fileHasher.Write(buffer)

		chunkHash := sha256.Sum256(buffer)
		metadata.ChunkHashes[i] = hex.EncodeToString(chunkHash[:])

		chunkPath := filepath.Join(chunkDir, fmt.Sprintf("%d.chunk", i))
		if err := os.WriteFile(chunkPath, buffer, 0644); err != nil {
			return nil, fmt.Errorf("failed to write chunk %d: %w", i, err)
		}

		c.logger.Debug("Chunk created",
			zap.String("file_id", fileID),
			zap.Int("chunk_index", i),
			zap.Int("chunk_size", size),
			zap.String("chunk_hash", metadata.ChunkHashes[i]))
	}

	metadata.FileHash = hex.EncodeToString(fileHasher.Sum(nil))

	// Store metadata
	c.mu.Lock()
	c.metadata[fileID] = metadata
	c.mu.Unlock()

	c.logger.Info("File chunked successfully",
		zap.String("file_id", fileID),
		zap.String("file_name", metadata.FileName),
		zap.Int64("file_size", metadata.FileSize),
		zap.Int("total_chunks", metadata.TotalChunks))

	return metadata, nil
}

// GetChunk retrieves a specific chunk of a file
func (c *Chunker) GetChunk(fileID string, chunkIndex int) (*FileChunkData, error) {
	c.mu.RLock()
	metadata, exists := c.metadata[fileID]
	c.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("file with ID %s not found", fileID)
	}

	if chunkIndex < 0 || chunkIndex >= metadata.TotalChunks {
		return nil, fmt.Errorf("invalid chunk index: %d", chunkIndex)
	}

	chunkPath := filepath.Join(c.workDir, fileID, fmt.Sprintf("%d.chunk", chunkIndex))
	data, err := os.ReadFile(chunkPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read chunk file: %w", err)
	}

	// Create a FileChunkData instead of using the generated FlatBuffers type directly
	chunk := &FileChunkData{
		ID:          fileID,
		Index:       int32(chunkIndex),
		TotalChunks: int32(metadata.TotalChunks),
		Data:        data,
	}

	return chunk, nil
}

// FileChunkData represents a chunk of a file
type FileChunkData struct {
	ID          string
	Index       int32
	TotalChunks int32
	Data        []byte
}

// ReassembleFile reassembles a file from its chunks
func (c *Chunker) ReassembleFile(fileID string, outputPath string) error {
	c.mu.RLock()
	metadata, exists := c.metadata[fileID]
	c.mu.RUnlock()

	if !exists {
		return fmt.Errorf("file with ID %s not found", fileID)
	}

	// Create output directory if it doesn't exist
	outputDir := filepath.Dir(outputPath)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Create output file
	outputFile, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer outputFile.Close()

	// Create a hasher to verify the reassembled file
	hasher := sha256.New()

	// Process chunks in order
	for i := 0; i < metadata.TotalChunks; i++ {
		chunkPath := filepath.Join(c.workDir, fileID, fmt.Sprintf("%d.chunk", i))
		chunkData, err := os.ReadFile(chunkPath)
		if err != nil {
			return fmt.Errorf("failed to read chunk %d: %w", i, err)
		}

		// Verify chunk hash
		chunkHasher := sha256.New()
		chunkHasher.Write(chunkData)
		chunkHash := hex.EncodeToString(chunkHasher.Sum(nil))

		if chunkHash != metadata.ChunkHashes[i] {
			return fmt.Errorf("chunk %d hash mismatch", i)
		}

		// Write chunk to output file
		if _, err := outputFile.Write(chunkData); err != nil {
			return fmt.Errorf("failed to write chunk %d to output file: %w", i, err)
		}

		// Update file hash
		hasher.Write(chunkData)

		c.logger.Debug("Chunk processed for reassembly",
			zap.String("file_id", fileID),
			zap.Int("chunk_index", i))
	}

	// Verify file hash
	fileHash := hex.EncodeToString(hasher.Sum(nil))
	if fileHash != metadata.FileHash {
		return fmt.Errorf("reassembled file hash mismatch")
	}

	c.logger.Info("File reassembled successfully",
		zap.String("file_id", fileID),
		zap.String("output_path", outputPath),
		zap.Int64("file_size", metadata.FileSize))

	return nil
}

// GetFileMetadata returns metadata for a file
func (c *Chunker) GetFileMetadata(fileID string) (*ChunkMetadata, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	metadata, exists := c.metadata[fileID]
	if !exists {
		return nil, fmt.Errorf("file with ID %s not found", fileID)
	}

	return metadata, nil
}

// ListFiles returns a list of all files managed by the chunker
func (c *Chunker) ListFiles() []*ChunkMetadata {
	c.mu.RLock()
	defer c.mu.RUnlock()

	files := make([]*ChunkMetadata, 0, len(c.metadata))
	for _, metadata := range c.metadata {
		files = append(files, metadata)
	}

	return files
}

// DeleteFile removes a file and its chunks
func (c *Chunker) DeleteFile(fileID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	metadata, exists := c.metadata[fileID]
	if !exists {
		return fmt.Errorf("file with ID %s not found", fileID)
	}

	// Remove chunk directory
	chunkDir := filepath.Join(c.workDir, fileID)
	if err := os.RemoveAll(chunkDir); err != nil {
		return fmt.Errorf("failed to remove chunk directory: %w", err)
	}

	// Remove metadata
	delete(c.metadata, fileID)

	c.logger.Info("File deleted",
		zap.String("file_id", fileID),
		zap.String("file_name", metadata.FileName))

	return nil
}

// GetWorkDir returns the working directory for chunks
func (c *Chunker) GetWorkDir() string {
	return c.workDir
}
