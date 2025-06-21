package file

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	flatbuffers "github.com/google/flatbuffers/go"
	"go.uber.org/zap"

	"github.com/TFMV/furymesh/fury/fury"
)

// Error definitions
var (
	ErrInvalidMessageType = errors.New("invalid message type")
)

// FlatBuffersSerializer handles serialization and deserialization of messages using FlatBuffers
type FlatBuffersSerializer struct {
	logger      *zap.Logger
	nodeID      string
	builderPool sync.Pool
}

// NewFlatBuffersSerializer creates a new FlatBuffers serializer
func NewFlatBuffersSerializer(logger *zap.Logger, nodeID string) *FlatBuffersSerializer {
	s := &FlatBuffersSerializer{
		logger: logger,
		nodeID: nodeID,
	}
	s.builderPool.New = func() interface{} {
		return flatbuffers.NewBuilder(1024)
	}
	return s
}

// SerializeFileMetadata serializes file metadata into a FlatBuffers message
func (f *FlatBuffersSerializer) SerializeFileMetadata(metadata *ChunkMetadata) []byte {
	builder := f.builderPool.Get().(*flatbuffers.Builder)
	defer func() {
		builder.Reset()
		f.builderPool.Put(builder)
	}()

	// Create chunk hashes array
	chunkHashOffsets := make([]flatbuffers.UOffsetT, len(metadata.ChunkHashes))
	for i, hash := range metadata.ChunkHashes {
		chunkHashOffsets[i] = builder.CreateString(hash)
	}

	// Start chunk hashes vector
	fury.FileMetadataStartChunkHashesVector(builder, len(chunkHashOffsets))
	for i := len(chunkHashOffsets) - 1; i >= 0; i-- {
		builder.PrependUOffsetT(chunkHashOffsets[i])
	}
	chunkHashesVector := builder.EndVector(len(chunkHashOffsets))

	// Create strings
	fileIDOffset := builder.CreateString(metadata.FileID)
	fileNameOffset := builder.CreateString(metadata.FileName)
	hashOffset := builder.CreateString(metadata.FileHash)
	mimeTypeOffset := builder.CreateString("application/octet-stream") // Default MIME type
	compressionOffset := builder.CreateString("none")                  // Default compression

	// Create FileMetadata
	fury.FileMetadataStart(builder)
	fury.FileMetadataAddFileId(builder, fileIDOffset)
	fury.FileMetadataAddFileName(builder, fileNameOffset)
	fury.FileMetadataAddFileSize(builder, uint64(metadata.FileSize))
	fury.FileMetadataAddChunkSize(builder, uint32(metadata.ChunkSize))
	fury.FileMetadataAddTotalChunks(builder, uint32(metadata.TotalChunks))
	fury.FileMetadataAddMimeType(builder, mimeTypeOffset)
	fury.FileMetadataAddCreatedAt(builder, time.Now().UnixNano())
	fury.FileMetadataAddHash(builder, hashOffset)
	fury.FileMetadataAddChunkHashes(builder, chunkHashesVector)
	fury.FileMetadataAddEncrypted(builder, false) // Default encryption status
	fury.FileMetadataAddCompression(builder, compressionOffset)
	fileMetadataOffset := fury.FileMetadataEnd(builder)

	// Create sender ID
	senderIDOffset := builder.CreateString(f.nodeID)

	// Create Message
	fury.MessageStart(builder)
	fury.MessageAddType(builder, fury.MessageTypeFileMetadata)
	fury.MessageAddSenderId(builder, senderIDOffset)
	fury.MessageAddTimestamp(builder, time.Now().UnixNano())
	fury.MessageAddFileMetadata(builder, fileMetadataOffset)
	messageOffset := fury.MessageEnd(builder)

	builder.Finish(messageOffset)
	return builder.FinishedBytes()
}

// DeserializeFileMetadata deserializes a FlatBuffers message into file metadata
func (f *FlatBuffersSerializer) DeserializeFileMetadata(data []byte) (*ChunkMetadata, error) {
	message := fury.GetRootAsMessage(data, 0)

	if message.Type() != fury.MessageTypeFileMetadata {
		return nil, ErrInvalidMessageType
	}

	fileMetadata := new(fury.FileMetadata)
	message.FileMetadata(fileMetadata)

	// Extract chunk hashes
	chunkHashes := make([]string, fileMetadata.ChunkHashesLength())
	for i := 0; i < fileMetadata.ChunkHashesLength(); i++ {
		chunkHashes[i] = string(fileMetadata.ChunkHashes(i))
	}

	return &ChunkMetadata{
		FileID:      string(fileMetadata.FileId()),
		FileName:    string(fileMetadata.FileName()),
		FileSize:    int64(fileMetadata.FileSize()),
		ChunkSize:   int(fileMetadata.ChunkSize()),
		TotalChunks: int(fileMetadata.TotalChunks()),
		ChunkHashes: chunkHashes,
		FileHash:    string(fileMetadata.Hash()),
	}, nil
}

// SerializeFileChunk serializes a file chunk into a FlatBuffers message
func (f *FlatBuffersSerializer) SerializeFileChunk(fileID string, chunkIndex int, data []byte, encrypted bool, compression string) []byte {
	builder := f.builderPool.Get().(*flatbuffers.Builder)
	defer func() {
		builder.Reset()
		f.builderPool.Put(builder)
	}()

	// Calculate hash of the chunk data
	hash := sha256.Sum256(data)
	hashHex := hex.EncodeToString(hash[:])

	// Create strings
	fileIDOffset := builder.CreateString(fileID)
	hashOffset := builder.CreateString(hashHex)
	compressionOffset := builder.CreateString(compression)

	// Create data vector
	fury.FileChunkStartDataVector(builder, len(data))
	for i := len(data) - 1; i >= 0; i-- {
		builder.PrependByte(data[i])
	}
	dataVector := builder.EndVector(len(data))

	// Create FileChunk
	fury.FileChunkStart(builder)
	fury.FileChunkAddFileId(builder, fileIDOffset)
	fury.FileChunkAddChunkIndex(builder, uint32(chunkIndex))
	fury.FileChunkAddData(builder, dataVector)
	fury.FileChunkAddHash(builder, hashOffset)
	fury.FileChunkAddEncrypted(builder, encrypted)
	fury.FileChunkAddCompression(builder, compressionOffset)
	fileChunkOffset := fury.FileChunkEnd(builder)

	// Create sender ID
	senderIDOffset := builder.CreateString(f.nodeID)

	// Create Message
	fury.MessageStart(builder)
	fury.MessageAddType(builder, fury.MessageTypeFileChunk)
	fury.MessageAddSenderId(builder, senderIDOffset)
	fury.MessageAddTimestamp(builder, time.Now().UnixNano())
	fury.MessageAddFileChunk(builder, fileChunkOffset)
	messageOffset := fury.MessageEnd(builder)

	builder.Finish(messageOffset)
	return builder.FinishedBytes()
}

// DeserializeFileChunk deserializes a FlatBuffers message into a file chunk
func (f *FlatBuffersSerializer) DeserializeFileChunk(data []byte) (*FileChunkData, error) {
	message := fury.GetRootAsMessage(data, 0)

	if message.Type() != fury.MessageTypeFileChunk {
		return nil, ErrInvalidMessageType
	}

	fileChunk := new(fury.FileChunk)
	message.FileChunk(fileChunk)

	// Extract chunk data
	chunkData := make([]byte, fileChunk.DataLength())
	for i := 0; i < fileChunk.DataLength(); i++ {
		chunkData[i] = fileChunk.Data(i)
	}

	return &FileChunkData{
		ID:    string(fileChunk.FileId()),
		Index: int32(fileChunk.ChunkIndex()),
		Data:  chunkData,
	}, nil
}

// SerializeChunkRequest serializes a chunk request into a FlatBuffers message
func (f *FlatBuffersSerializer) SerializeChunkRequest(fileID string, chunkIndex int, priority uint8) []byte {
	builder := f.builderPool.Get().(*flatbuffers.Builder)
	defer func() {
		builder.Reset()
		f.builderPool.Put(builder)
	}()

	// Create strings
	fileIDOffset := builder.CreateString(fileID)

	// Create ChunkRequest
	fury.ChunkRequestStart(builder)
	fury.ChunkRequestAddFileId(builder, fileIDOffset)
	fury.ChunkRequestAddChunkIndex(builder, uint32(chunkIndex))
	fury.ChunkRequestAddPriority(builder, priority)
	chunkRequestOffset := fury.ChunkRequestEnd(builder)

	// Create sender ID
	senderIDOffset := builder.CreateString(f.nodeID)

	// Create Message
	fury.MessageStart(builder)
	fury.MessageAddType(builder, fury.MessageTypeChunkRequest)
	fury.MessageAddSenderId(builder, senderIDOffset)
	fury.MessageAddTimestamp(builder, time.Now().UnixNano())
	fury.MessageAddChunkRequest(builder, chunkRequestOffset)
	messageOffset := fury.MessageEnd(builder)

	builder.Finish(messageOffset)
	return builder.FinishedBytes()
}

// DeserializeChunkRequest deserializes a FlatBuffers message into a chunk request
func (f *FlatBuffersSerializer) DeserializeChunkRequest(data []byte) (*DataRequest, error) {
	message := fury.GetRootAsMessage(data, 0)

	if message.Type() != fury.MessageTypeChunkRequest {
		return nil, ErrInvalidMessageType
	}

	chunkRequest := new(fury.ChunkRequest)
	message.ChunkRequest(chunkRequest)

	return &DataRequest{
		FileID:     string(chunkRequest.FileId()),
		ChunkIndex: int(chunkRequest.ChunkIndex()),
	}, nil
}

// SerializeTransferStatus serializes a transfer status into a FlatBuffers message
func (f *FlatBuffersSerializer) SerializeTransferStatus(transferID string, stats *TransferStats) []byte {
	builder := f.builderPool.Get().(*flatbuffers.Builder)
	defer func() {
		builder.Reset()
		f.builderPool.Put(builder)
	}()

	// Create strings
	fileIDOffset := builder.CreateString(transferID)
	statusOffset := builder.CreateString(string(stats.Status))

	// Create TransferStatus
	fury.TransferStatusStart(builder)
	fury.TransferStatusAddFileId(builder, fileIDOffset)
	fury.TransferStatusAddChunksReceived(builder, uint32(stats.ChunksTransferred))
	fury.TransferStatusAddTotalChunks(builder, uint32(stats.TotalChunks))
	fury.TransferStatusAddBytesReceived(builder, uint64(stats.BytesTransferred))
	fury.TransferStatusAddTotalBytes(builder, uint64(stats.TotalBytes))
	fury.TransferStatusAddTransferRate(builder, uint32(stats.TransferRate))
	fury.TransferStatusAddEtaSeconds(builder, uint32(0)) // Not available in TransferStats
	fury.TransferStatusAddStatus(builder, statusOffset)
	transferStatusOffset := fury.TransferStatusEnd(builder)

	// Create sender ID
	senderIDOffset := builder.CreateString(f.nodeID)

	// Create Message
	fury.MessageStart(builder)
	fury.MessageAddType(builder, fury.MessageTypeTransferStatus)
	fury.MessageAddSenderId(builder, senderIDOffset)
	fury.MessageAddTimestamp(builder, time.Now().UnixNano())
	fury.MessageAddTransferStatus(builder, transferStatusOffset)
	messageOffset := fury.MessageEnd(builder)

	builder.Finish(messageOffset)
	return builder.FinishedBytes()
}

// DeserializeTransferStatus deserializes a FlatBuffers message into a transfer status
func (f *FlatBuffersSerializer) DeserializeTransferStatus(data []byte) (*TransferStats, error) {
	message := fury.GetRootAsMessage(data, 0)

	if message.Type() != fury.MessageTypeTransferStatus {
		return nil, ErrInvalidMessageType
	}

	transferStatus := new(fury.TransferStatus)
	message.TransferStatus(transferStatus)

	status := TransferStatusPending
	switch string(transferStatus.Status()) {
	case "pending":
		status = TransferStatusPending
	case "in_progress":
		status = TransferStatusInProgress
	case "completed":
		status = TransferStatusCompleted
	case "failed":
		status = TransferStatusFailed
	case "cancelled":
		status = TransferStatusCancelled
	}

	return &TransferStats{
		BytesTransferred:  int64(transferStatus.BytesReceived()),
		TotalBytes:        int64(transferStatus.TotalBytes()),
		ChunksTransferred: int(transferStatus.ChunksReceived()),
		TotalChunks:       int(transferStatus.TotalChunks()),
		TransferRate:      float64(transferStatus.TransferRate()),
		Status:            status,
		StartTime:         time.Now(),
	}, nil
}

// SerializeErrorMessage serializes an error message into a FlatBuffers message
func (f *FlatBuffersSerializer) SerializeErrorMessage(code int32, message, context string) []byte {
	builder := f.builderPool.Get().(*flatbuffers.Builder)
	defer func() {
		builder.Reset()
		f.builderPool.Put(builder)
	}()

	// Create strings
	messageOffset := builder.CreateString(message)
	contextOffset := builder.CreateString(context)

	// Create ErrorMessage
	fury.ErrorMessageStart(builder)
	fury.ErrorMessageAddCode(builder, code)
	fury.ErrorMessageAddMessage(builder, messageOffset)
	fury.ErrorMessageAddContext(builder, contextOffset)
	errorMessageOffset := fury.ErrorMessageEnd(builder)

	// Create sender ID
	senderIDOffset := builder.CreateString(f.nodeID)

	// Create Message
	fury.MessageStart(builder)
	fury.MessageAddType(builder, fury.MessageTypeErrorMessage)
	fury.MessageAddSenderId(builder, senderIDOffset)
	fury.MessageAddTimestamp(builder, time.Now().UnixNano())
	fury.MessageAddErrorMessage(builder, errorMessageOffset)
	errorOffset := fury.MessageEnd(builder)

	builder.Finish(errorOffset)
	return builder.FinishedBytes()
}

// DeserializeErrorMessage deserializes a FlatBuffers message into an error message
type ErrorMessage struct {
	Code    int32
	Message string
	Context string
}

func (f *FlatBuffersSerializer) DeserializeErrorMessage(data []byte) (*ErrorMessage, error) {
	message := fury.GetRootAsMessage(data, 0)

	if message.Type() != fury.MessageTypeErrorMessage {
		return nil, ErrInvalidMessageType
	}

	errorMessage := new(fury.ErrorMessage)
	message.ErrorMessage(errorMessage)

	return &ErrorMessage{
		Code:    errorMessage.Code(),
		Message: string(errorMessage.Message()),
		Context: string(errorMessage.Context()),
	}, nil
}

// GetMessageType returns the type of the message
func (f *FlatBuffersSerializer) GetMessageType(data []byte) (fury.MessageType, error) {
	message := fury.GetRootAsMessage(data, 0)
	return message.Type(), nil
}

// GetSenderID returns the sender ID of the message
func (f *FlatBuffersSerializer) GetSenderID(data []byte) (string, error) {
	message := fury.GetRootAsMessage(data, 0)
	return string(message.SenderId()), nil
}

// GetTimestamp returns the timestamp of the message
func (f *FlatBuffersSerializer) GetTimestamp(data []byte) (int64, error) {
	message := fury.GetRootAsMessage(data, 0)
	return message.Timestamp(), nil
}
