package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/TFMV/furymesh/file"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

var (
	// Flags for file commands
	chunkSize    int
	outputPath   string
	workDir      string
	storageDir   string
	maxAge       string
	peerID       string
	concurrency  int
	showProgress bool
)

// fileCmd represents the file command
var fileCmd = &cobra.Command{
	Use:   "file",
	Short: "Manage files in the FuryMesh network",
	Long:  `Commands for managing files, including chunking, sharing, and downloading.`,
}

// chunkCmd represents the chunk command
var chunkCmd = &cobra.Command{
	Use:   "chunk [file_path]",
	Short: "Chunk a file for sharing",
	Long:  `Split a file into chunks for sharing over the FuryMesh network.`,
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		logger, _ := zap.NewProduction()
		defer logger.Sync()

		filePath := args[0]

		// Validate file path
		fileInfo, err := os.Stat(filePath)
		if err != nil {
			logger.Fatal("Failed to access file", zap.String("path", filePath), zap.Error(err))
		}

		if fileInfo.IsDir() {
			logger.Fatal("Path is a directory, not a file", zap.String("path", filePath))
		}

		// Get work directory
		if workDir == "" {
			workDir = viper.GetString("storage.work_dir")
			if workDir == "" {
				homeDir, err := os.UserHomeDir()
				if err != nil {
					logger.Fatal("Failed to get user home directory", zap.Error(err))
				}
				workDir = filepath.Join(homeDir, ".furymesh", "work")
			}
		}

		// Create chunker
		chunker, err := file.NewChunker(logger, workDir, chunkSize)
		if err != nil {
			logger.Fatal("Failed to create chunker", zap.Error(err))
		}

		// Create storage manager
		storageConfig := file.StorageConfig{
			BaseDir: storageDir,
		}
		storage, err := file.NewStorageManager(logger, storageConfig)
		if err != nil {
			logger.Fatal("Failed to create storage manager", zap.Error(err))
		}

		// Chunk the file
		fmt.Printf("Chunking file: %s\n", filePath)
		fmt.Printf("Chunk size: %d bytes\n", chunkSize)

		startTime := time.Now()
		metadata, err := chunker.ChunkFile(filePath)
		if err != nil {
			logger.Fatal("Failed to chunk file", zap.Error(err))
		}
		duration := time.Since(startTime)

		// Save metadata
		if err := storage.SaveMetadata(metadata); err != nil {
			logger.Fatal("Failed to save metadata", zap.Error(err))
		}

		// Save chunks
		for i := 0; i < metadata.TotalChunks; i++ {
			chunk, err := chunker.GetChunk(metadata.FileID, i)
			if err != nil {
				logger.Fatal("Failed to get chunk", zap.Int("chunk_index", i), zap.Error(err))
			}

			if err := storage.SaveChunk(metadata.FileID, i, chunk.Data); err != nil {
				logger.Fatal("Failed to save chunk", zap.Int("chunk_index", i), zap.Error(err))
			}
		}

		// Print results
		fmt.Printf("File chunked successfully:\n")
		fmt.Printf("  File ID: %s\n", metadata.FileID)
		fmt.Printf("  File name: %s\n", metadata.FileName)
		fmt.Printf("  File size: %d bytes\n", metadata.FileSize)
		fmt.Printf("  Total chunks: %d\n", metadata.TotalChunks)
		fmt.Printf("  Chunk size: %d bytes\n", metadata.ChunkSize)
		fmt.Printf("  Processing time: %v\n", duration)
		fmt.Printf("  Storage location: %s\n", storageDir)
	},
}

// reassembleCmd represents the reassemble command
var reassembleCmd = &cobra.Command{
	Use:   "reassemble [file_id]",
	Short: "Reassemble a chunked file",
	Long:  `Reassemble a file from its chunks.`,
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		logger, _ := zap.NewProduction()
		defer logger.Sync()

		fileID := args[0]

		// Get work directory
		if workDir == "" {
			workDir = viper.GetString("storage.work_dir")
			if workDir == "" {
				homeDir, err := os.UserHomeDir()
				if err != nil {
					logger.Fatal("Failed to get user home directory", zap.Error(err))
				}
				workDir = filepath.Join(homeDir, ".furymesh", "work")
			}
		}

		// Create storage manager
		storageConfig := file.StorageConfig{
			BaseDir: storageDir,
		}
		storage, err := file.NewStorageManager(logger, storageConfig)
		if err != nil {
			logger.Fatal("Failed to create storage manager", zap.Error(err))
		}

		// Get metadata
		metadata, err := storage.GetMetadata(fileID)
		if err != nil {
			logger.Fatal("Failed to get metadata", zap.String("file_id", fileID), zap.Error(err))
		}

		// Create chunker
		chunker, err := file.NewChunker(logger, workDir, metadata.ChunkSize)
		if err != nil {
			logger.Fatal("Failed to create chunker", zap.Error(err))
		}

		// Determine output path
		if outputPath == "" {
			outputPath = metadata.FileName
		}

		// Load chunks into chunker
		for i := 0; i < metadata.TotalChunks; i++ {
			data, err := storage.GetChunk(fileID, i)
			if err != nil {
				logger.Fatal("Failed to get chunk", zap.Int("chunk_index", i), zap.Error(err))
			}

			// Create chunk directory
			chunkDir := filepath.Join(workDir, fileID)
			if err := os.MkdirAll(chunkDir, 0755); err != nil {
				logger.Fatal("Failed to create chunk directory", zap.Error(err))
			}

			// Write chunk to file
			chunkPath := filepath.Join(chunkDir, fmt.Sprintf("%d.chunk", i))
			if err := os.WriteFile(chunkPath, data, 0644); err != nil {
				logger.Fatal("Failed to write chunk file", zap.Error(err))
			}
		}

		// Reassemble the file
		fmt.Printf("Reassembling file: %s\n", metadata.FileName)
		fmt.Printf("File ID: %s\n", fileID)

		startTime := time.Now()
		if err := chunker.ReassembleFile(fileID, outputPath); err != nil {
			logger.Fatal("Failed to reassemble file", zap.Error(err))
		}
		duration := time.Since(startTime)

		// Print results
		fmt.Printf("File reassembled successfully:\n")
		fmt.Printf("  Output path: %s\n", outputPath)
		fmt.Printf("  File size: %d bytes\n", metadata.FileSize)
		fmt.Printf("  Processing time: %v\n", duration)
	},
}

// listCmd represents the list command
var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List available files",
	Long:  `List files available in the local storage.`,
	Run: func(cmd *cobra.Command, args []string) {
		logger, _ := zap.NewProduction()
		defer logger.Sync()

		// Create storage manager
		storageConfig := file.StorageConfig{
			BaseDir: storageDir,
		}
		storage, err := file.NewStorageManager(logger, storageConfig)
		if err != nil {
			logger.Fatal("Failed to create storage manager", zap.Error(err))
		}

		// Get file list
		files := storage.ListMetadata()

		// Print results
		fmt.Printf("Available files (%d):\n", len(files))
		fmt.Printf("%-36s %-30s %-15s %s\n", "File ID", "File Name", "Size", "Chunks")
		fmt.Printf("%-36s %-30s %-15s %s\n", "--------------------------------------", "------------------------------", "---------------", "------")

		for _, metadata := range files {
			sizeStr := formatSize(metadata.FileSize)
			fmt.Printf("%-36s %-30s %-15s %d\n", metadata.FileID, metadata.FileName, sizeStr, metadata.TotalChunks)
		}

		// Get storage stats
		stats, err := storage.GetStorageStats()
		if err != nil {
			logger.Warn("Failed to get storage stats", zap.Error(err))
		} else {
			fmt.Printf("\nStorage statistics:\n")
			fmt.Printf("  Total files: %d\n", stats["file_count"])
			fmt.Printf("  Total size: %s\n", formatSize(stats["total_size"].(int64)))
			fmt.Printf("  Total chunks: %d\n", stats["total_chunks"])
			fmt.Printf("  Disk usage: %s\n", formatSize(stats["disk_usage"].(int64)))
			fmt.Printf("  Storage location: %s\n", stats["base_dir"])
		}
	},
}

// deleteCmd represents the delete command
var deleteCmd = &cobra.Command{
	Use:   "delete [file_id]",
	Short: "Delete a file",
	Long:  `Delete a file and its chunks from local storage.`,
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		logger, _ := zap.NewProduction()
		defer logger.Sync()

		fileID := args[0]

		// Create storage manager
		storageConfig := file.StorageConfig{
			BaseDir: storageDir,
		}
		storage, err := file.NewStorageManager(logger, storageConfig)
		if err != nil {
			logger.Fatal("Failed to create storage manager", zap.Error(err))
		}

		// Get metadata
		metadata, err := storage.GetMetadata(fileID)
		if err != nil {
			logger.Fatal("Failed to get metadata", zap.String("file_id", fileID), zap.Error(err))
		}

		// Delete chunks
		if err := storage.DeleteAllChunks(fileID); err != nil {
			logger.Fatal("Failed to delete chunks", zap.Error(err))
		}

		// Delete metadata
		if err := storage.DeleteMetadata(fileID); err != nil {
			logger.Fatal("Failed to delete metadata", zap.Error(err))
		}

		fmt.Printf("File deleted successfully:\n")
		fmt.Printf("  File ID: %s\n", fileID)
		fmt.Printf("  File name: %s\n", metadata.FileName)
	},
}

// cleanupCmd represents the cleanup command
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Clean up expired files",
	Long:  `Remove files that have expired based on their age.`,
	Run: func(cmd *cobra.Command, args []string) {
		logger, _ := zap.NewProduction()
		defer logger.Sync()

		// Parse max age
		duration, err := parseDuration(maxAge)
		if err != nil {
			logger.Fatal("Invalid max age", zap.String("max_age", maxAge), zap.Error(err))
		}

		// Create storage manager
		storageConfig := file.StorageConfig{
			BaseDir: storageDir,
		}
		storage, err := file.NewStorageManager(logger, storageConfig)
		if err != nil {
			logger.Fatal("Failed to create storage manager", zap.Error(err))
		}

		// Clean up expired files
		if err := storage.CleanupExpiredFiles(duration); err != nil {
			logger.Fatal("Failed to clean up expired files", zap.Error(err))
		}

		fmt.Printf("Expired files cleaned up successfully.\n")
	},
}

// Helper function to format file size
func formatSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(size)/float64(div), "KMGTPE"[exp])
}

// Helper function to parse duration string
func parseDuration(s string) (time.Duration, error) {
	// Try standard duration parsing
	d, err := time.ParseDuration(s)
	if err == nil {
		return d, nil
	}

	// Try parsing as days
	if len(s) > 1 && s[len(s)-1] == 'd' {
		days, err := strconv.Atoi(s[:len(s)-1])
		if err != nil {
			return 0, err
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}

	return 0, fmt.Errorf("invalid duration format: %s", s)
}

func init() {
	rootCmd.AddCommand(fileCmd)
	fileCmd.AddCommand(chunkCmd)
	fileCmd.AddCommand(reassembleCmd)
	fileCmd.AddCommand(listCmd)
	fileCmd.AddCommand(deleteCmd)
	fileCmd.AddCommand(cleanupCmd)

	// Flags for chunk command
	chunkCmd.Flags().IntVarP(&chunkSize, "chunk-size", "c", file.DefaultChunkSize, "Size of each chunk in bytes")
	chunkCmd.Flags().StringVarP(&workDir, "work-dir", "w", "", "Working directory for temporary files")
	chunkCmd.Flags().StringVarP(&storageDir, "storage-dir", "s", "", "Storage directory for chunks and metadata")

	// Flags for reassemble command
	reassembleCmd.Flags().StringVarP(&outputPath, "output", "o", "", "Output path for the reassembled file")
	reassembleCmd.Flags().StringVarP(&workDir, "work-dir", "w", "", "Working directory for temporary files")
	reassembleCmd.Flags().StringVarP(&storageDir, "storage-dir", "s", "", "Storage directory for chunks and metadata")

	// Flags for list command
	listCmd.Flags().StringVarP(&storageDir, "storage-dir", "s", "", "Storage directory for chunks and metadata")

	// Flags for delete command
	deleteCmd.Flags().StringVarP(&storageDir, "storage-dir", "s", "", "Storage directory for chunks and metadata")

	// Flags for cleanup command
	cleanupCmd.Flags().StringVarP(&maxAge, "max-age", "m", "30d", "Maximum age of files to keep (e.g., 30d, 24h)")
	cleanupCmd.Flags().StringVarP(&storageDir, "storage-dir", "s", "", "Storage directory for chunks and metadata")
}
