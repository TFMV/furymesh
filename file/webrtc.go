package file

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/pion/webrtc/v3"
	"go.uber.org/zap"
)

// WebRTCTransport handles WebRTC connections for file transfers
type WebRTCTransport struct {
	logger             *zap.Logger
	transferManager    *TransferManager
	peerConnections    map[string]*webrtc.PeerConnection
	dataChannels       map[string]*webrtc.DataChannel
	mu                 sync.RWMutex
	config             webrtc.Configuration
	onPeerConnected    func(peerID string)
	onPeerDisconnected func(peerID string)

	// Storage manager for persisting chunks
	storageManager *StorageManager

	// Active transfers
	activeTransfers map[string]bool
	transfersMu     sync.RWMutex
}

// WebRTCConfig contains configuration for WebRTC connections
type WebRTCConfig struct {
	ICEServers []string
}

// NewWebRTCTransport creates a new WebRTCTransport
func NewWebRTCTransport(
	logger *zap.Logger,
	transferManager *TransferManager,
	storageManager *StorageManager,
	config WebRTCConfig,
) *WebRTCTransport {
	// Convert ICE server strings to webrtc.ICEServer objects
	var iceServers []webrtc.ICEServer
	for _, server := range config.ICEServers {
		iceServers = append(iceServers, webrtc.ICEServer{
			URLs: []string{server},
		})
	}

	return &WebRTCTransport{
		logger:          logger,
		transferManager: transferManager,
		storageManager:  storageManager,
		peerConnections: make(map[string]*webrtc.PeerConnection),
		dataChannels:    make(map[string]*webrtc.DataChannel),
		activeTransfers: make(map[string]bool),
		config: webrtc.Configuration{
			ICEServers: iceServers,
		},
	}
}

// SetPeerCallbacks sets callbacks for peer connection events
func (w *WebRTCTransport) SetPeerCallbacks(
	onConnected func(peerID string),
	onDisconnected func(peerID string),
) {
	w.onPeerConnected = onConnected
	w.onPeerDisconnected = onDisconnected
}

// CreatePeerConnection creates a new WebRTC peer connection
func (w *WebRTCTransport) CreatePeerConnection(peerID string) (*webrtc.PeerConnection, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Check if a connection already exists
	if pc, exists := w.peerConnections[peerID]; exists {
		return pc, nil
	}

	// Create a new peer connection
	peerConnection, err := webrtc.NewPeerConnection(w.config)
	if err != nil {
		return nil, fmt.Errorf("failed to create peer connection: %w", err)
	}

	// Set up ICE connection state handler
	peerConnection.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		w.logger.Info("ICE connection state changed",
			zap.String("peer_id", peerID),
			zap.String("state", state.String()))

		switch state {
		case webrtc.ICEConnectionStateConnected:
			if w.onPeerConnected != nil {
				w.onPeerConnected(peerID)
			}
		case webrtc.ICEConnectionStateDisconnected, webrtc.ICEConnectionStateFailed, webrtc.ICEConnectionStateClosed:
			if w.onPeerDisconnected != nil {
				w.onPeerDisconnected(peerID)
			}

			// Clean up the connection
			w.mu.Lock()
			delete(w.peerConnections, peerID)
			delete(w.dataChannels, peerID)
			w.mu.Unlock()

			// Cancel any active transfers with this peer
			w.cancelPeerTransfers(peerID)
		}
	})

	// Create a data channel for file transfers
	dataChannel, err := peerConnection.CreateDataChannel("fury-transfer", nil)
	if err != nil {
		peerConnection.Close()
		return nil, fmt.Errorf("failed to create data channel: %w", err)
	}

	// Set up data channel handlers
	dataChannel.OnOpen(func() {
		w.logger.Info("Data channel opened", zap.String("peer_id", peerID))
	})

	dataChannel.OnClose(func() {
		w.logger.Info("Data channel closed", zap.String("peer_id", peerID))
	})

	dataChannel.OnMessage(func(msg webrtc.DataChannelMessage) {
		w.handleDataChannelMessage(peerID, msg)
	})

	// Store the peer connection and data channel
	w.peerConnections[peerID] = peerConnection
	w.dataChannels[peerID] = dataChannel

	return peerConnection, nil
}

// cancelPeerTransfers cancels all active transfers with a peer
func (w *WebRTCTransport) cancelPeerTransfers(peerID string) {
	w.transfersMu.Lock()
	defer w.transfersMu.Unlock()

	// Find all transfers associated with this peer
	for transferID, active := range w.activeTransfers {
		if active && transferID[:36] == peerID {
			fileID := transferID[37:]
			w.transferManager.CancelTransfer(fileID)
			delete(w.activeTransfers, transferID)
		}
	}
}

// handleDataChannelMessage processes messages received on the data channel
func (w *WebRTCTransport) handleDataChannelMessage(peerID string, msg webrtc.DataChannelMessage) {
	// Check if this is a binary message (FlatBuffers)
	if msg.IsString {
		// Handle JSON message
		var message map[string]interface{}
		if err := json.Unmarshal(msg.Data, &message); err != nil {
			w.logger.Error("Failed to parse data channel message",
				zap.String("peer_id", peerID),
				zap.Error(err))
			return
		}

		// Extract the message type
		msgType, ok := message["type"].(string)
		if !ok {
			w.logger.Error("Missing message type in data channel message",
				zap.String("peer_id", peerID))
			return
		}

		// Handle different message types
		switch msgType {
		case "file_request":
			w.handleFileRequest(peerID, message)
		case "chunk_request":
			w.handleChunkRequest(peerID, message)
		case "chunk_response":
			w.handleChunkResponse(peerID, message)
		case "transfer_cancel":
			w.handleTransferCancel(peerID, message)
		case "transfer_complete":
			w.handleTransferComplete(peerID, message)
		default:
			w.logger.Warn("Unknown message type in data channel message",
				zap.String("peer_id", peerID),
				zap.String("type", msgType))
		}
	} else {
		// Handle binary message (FlatBuffers)
		w.handleBinaryMessage(peerID, msg.Data)
	}
}

// handleBinaryMessage processes binary messages (FlatBuffers)
func (w *WebRTCTransport) handleBinaryMessage(peerID string, data []byte) {
	// TODO: Implement FlatBuffers deserialization
	// For now, we'll just log that we received a binary message
	w.logger.Debug("Received binary message",
		zap.String("peer_id", peerID),
		zap.Int("data_size", len(data)))
}

// handleFileRequest processes a request for a file
func (w *WebRTCTransport) handleFileRequest(peerID string, message map[string]interface{}) {
	// Extract the file ID
	fileID, ok := message["file_id"].(string)
	if !ok {
		w.logger.Error("Missing file ID in file request",
			zap.String("peer_id", peerID))
		return
	}

	// Get the file metadata
	metadata, err := w.storageManager.GetMetadata(fileID)
	if err != nil {
		w.logger.Error("Failed to get file metadata",
			zap.String("peer_id", peerID),
			zap.String("file_id", fileID),
			zap.Error(err))

		// Send an error response
		w.sendErrorResponse(peerID, "file_request_error", fileID, err.Error())
		return
	}

	// Register the transfer
	transferID := fmt.Sprintf("%s:%s", peerID, fileID)
	w.transfersMu.Lock()
	w.activeTransfers[transferID] = true
	w.transfersMu.Unlock()

	// Send a file response
	response := map[string]interface{}{
		"type":         "file_response",
		"file_id":      fileID,
		"file_name":    metadata.FileName,
		"file_size":    metadata.FileSize,
		"total_chunks": metadata.TotalChunks,
		"chunk_size":   metadata.ChunkSize,
		"file_hash":    metadata.FileHash,
	}

	w.sendDataChannelMessage(peerID, response)

	w.logger.Info("File request handled",
		zap.String("peer_id", peerID),
		zap.String("file_id", fileID),
		zap.String("file_name", metadata.FileName))
}

// handleChunkRequest processes a request for a chunk
func (w *WebRTCTransport) handleChunkRequest(peerID string, message map[string]interface{}) {
	// Extract the file ID and chunk index
	fileID, ok := message["file_id"].(string)
	if !ok {
		w.logger.Error("Missing file ID in chunk request",
			zap.String("peer_id", peerID))
		return
	}

	chunkIndex, ok := message["chunk_index"].(float64)
	if !ok {
		w.logger.Error("Missing or invalid chunk index in chunk request",
			zap.String("peer_id", peerID),
			zap.String("file_id", fileID))
		return
	}

	// Check if this is an active transfer
	transferID := fmt.Sprintf("%s:%s", peerID, fileID)
	w.transfersMu.RLock()
	active := w.activeTransfers[transferID]
	w.transfersMu.RUnlock()

	if !active {
		w.logger.Warn("Received chunk request for inactive transfer",
			zap.String("peer_id", peerID),
			zap.String("file_id", fileID),
			zap.Float64("chunk_index", chunkIndex))

		// Send an error response
		w.sendErrorResponse(peerID, "chunk_request_error", fileID, "transfer not active")
		return
	}

	// Get the chunk data from storage
	chunkData, err := w.storageManager.GetChunk(fileID, int(chunkIndex))
	if err != nil {
		w.logger.Error("Failed to get chunk",
			zap.String("peer_id", peerID),
			zap.String("file_id", fileID),
			zap.Float64("chunk_index", chunkIndex),
			zap.Error(err))

		// Send an error response
		w.sendErrorResponse(peerID, "chunk_request_error", fileID, err.Error())
		return
	}

	// Get metadata for total chunks
	metadata, err := w.storageManager.GetMetadata(fileID)
	if err != nil {
		w.logger.Error("Failed to get metadata",
			zap.String("peer_id", peerID),
			zap.String("file_id", fileID),
			zap.Error(err))
		return
	}

	// Create a FileChunkData
	chunk := &FileChunkData{
		ID:          fileID,
		Index:       int32(chunkIndex),
		TotalChunks: int32(metadata.TotalChunks),
		Data:        chunkData,
	}

	// Send a chunk response
	response := map[string]interface{}{
		"type":         "chunk_response",
		"file_id":      fileID,
		"chunk_index":  int(chunkIndex),
		"data":         chunk.Data,
		"total_chunks": metadata.TotalChunks,
	}

	w.sendDataChannelMessage(peerID, response)

	w.logger.Debug("Chunk request handled",
		zap.String("peer_id", peerID),
		zap.String("file_id", fileID),
		zap.Float64("chunk_index", chunkIndex),
		zap.Int("data_size", len(chunkData)))
}

// handleChunkResponse processes a response containing a chunk
func (w *WebRTCTransport) handleChunkResponse(peerID string, message map[string]interface{}) {
	// Extract the file ID and chunk index
	fileID, ok := message["file_id"].(string)
	if !ok {
		w.logger.Error("Missing file ID in chunk response",
			zap.String("peer_id", peerID))
		return
	}

	chunkIndex, ok := message["chunk_index"].(float64)
	if !ok {
		w.logger.Error("Missing or invalid chunk index in chunk response",
			zap.String("peer_id", peerID),
			zap.String("file_id", fileID))
		return
	}

	data, ok := message["data"].([]byte)
	if !ok {
		w.logger.Error("Missing or invalid data in chunk response",
			zap.String("peer_id", peerID),
			zap.String("file_id", fileID),
			zap.Float64("chunk_index", chunkIndex))
		return
	}

	totalChunks, ok := message["total_chunks"].(float64)
	if !ok {
		w.logger.Error("Missing or invalid total_chunks in chunk response",
			zap.String("peer_id", peerID),
			zap.String("file_id", fileID))
		return
	}

	// Check if this is an active transfer
	transferID := fmt.Sprintf("%s:%s", peerID, fileID)
	w.transfersMu.RLock()
	active := w.activeTransfers[transferID]
	w.transfersMu.RUnlock()

	if !active {
		w.logger.Warn("Received chunk response for inactive transfer",
			zap.String("peer_id", peerID),
			zap.String("file_id", fileID),
			zap.Float64("chunk_index", chunkIndex))
		return
	}

	// Save the chunk to storage
	if err := w.storageManager.SaveChunk(fileID, int(chunkIndex), data); err != nil {
		w.logger.Error("Failed to save chunk",
			zap.String("peer_id", peerID),
			zap.String("file_id", fileID),
			zap.Float64("chunk_index", chunkIndex),
			zap.Error(err))
		return
	}

	// Create a DataResponse to pass to the transfer manager
	chunk := &FileChunkData{
		ID:          fileID,
		Index:       int32(chunkIndex),
		TotalChunks: int32(totalChunks),
		Data:        data,
	}

	response := &DataResponse{
		RequestID: fmt.Sprintf("%s-%s-%d", peerID, fileID, int(chunkIndex)),
		FileID:    fileID,
		Chunk:     chunk,
	}

	// Pass the response to the transfer manager
	select {
	case w.transferManager.dataResponseCh <- response:
		w.logger.Debug("Forwarded chunk response to transfer manager",
			zap.String("peer_id", peerID),
			zap.String("file_id", fileID),
			zap.Float64("chunk_index", chunkIndex))
	default:
		w.logger.Warn("Failed to forward chunk response to transfer manager (channel full)",
			zap.String("peer_id", peerID),
			zap.String("file_id", fileID),
			zap.Float64("chunk_index", chunkIndex))
	}
}

// handleTransferCancel processes a transfer cancellation
func (w *WebRTCTransport) handleTransferCancel(peerID string, message map[string]interface{}) {
	// Extract the file ID
	fileID, ok := message["file_id"].(string)
	if !ok {
		w.logger.Error("Missing file ID in transfer cancel",
			zap.String("peer_id", peerID))
		return
	}

	// Remove from active transfers
	transferID := fmt.Sprintf("%s:%s", peerID, fileID)
	w.transfersMu.Lock()
	delete(w.activeTransfers, transferID)
	w.transfersMu.Unlock()

	// Cancel the transfer in the transfer manager
	if err := w.transferManager.CancelTransfer(fileID); err != nil {
		w.logger.Warn("Failed to cancel transfer",
			zap.String("peer_id", peerID),
			zap.String("file_id", fileID),
			zap.Error(err))
	}

	w.logger.Info("Transfer cancelled by peer",
		zap.String("peer_id", peerID),
		zap.String("file_id", fileID))
}

// handleTransferComplete processes a transfer completion
func (w *WebRTCTransport) handleTransferComplete(peerID string, message map[string]interface{}) {
	// Extract the file ID
	fileID, ok := message["file_id"].(string)
	if !ok {
		w.logger.Error("Missing file ID in transfer complete",
			zap.String("peer_id", peerID))
		return
	}

	// Remove from active transfers
	transferID := fmt.Sprintf("%s:%s", peerID, fileID)
	w.transfersMu.Lock()
	delete(w.activeTransfers, transferID)
	w.transfersMu.Unlock()

	w.logger.Info("Transfer completed by peer",
		zap.String("peer_id", peerID),
		zap.String("file_id", fileID))
}

// sendErrorResponse sends an error response
func (w *WebRTCTransport) sendErrorResponse(peerID, errorType, fileID, errorMsg string) {
	response := map[string]interface{}{
		"type":      errorType,
		"file_id":   fileID,
		"error":     errorMsg,
		"timestamp": time.Now().Unix(),
	}

	w.sendDataChannelMessage(peerID, response)
}

// sendDataChannelMessage sends a message on the data channel
func (w *WebRTCTransport) sendDataChannelMessage(peerID string, message interface{}) {
	w.mu.RLock()
	dataChannel, exists := w.dataChannels[peerID]
	w.mu.RUnlock()

	if !exists {
		w.logger.Error("Data channel not found for peer",
			zap.String("peer_id", peerID))
		return
	}

	// Serialize the message
	data, err := json.Marshal(message)
	if err != nil {
		w.logger.Error("Failed to serialize message",
			zap.String("peer_id", peerID),
			zap.Error(err))
		return
	}

	// Send the message
	if err := dataChannel.Send(data); err != nil {
		w.logger.Error("Failed to send message on data channel",
			zap.String("peer_id", peerID),
			zap.Error(err))
		return
	}

	w.logger.Debug("Sent message on data channel",
		zap.String("peer_id", peerID),
		zap.String("message_type", message.(map[string]interface{})["type"].(string)))
}

// sendBinaryMessage sends a binary message on the data channel
func (w *WebRTCTransport) sendBinaryMessage(peerID string, data []byte) {
	w.mu.RLock()
	dataChannel, exists := w.dataChannels[peerID]
	w.mu.RUnlock()

	if !exists {
		w.logger.Error("Data channel not found for peer",
			zap.String("peer_id", peerID))
		return
	}

	// Send the message
	if err := dataChannel.SendText(string(data)); err != nil {
		w.logger.Error("Failed to send binary message on data channel",
			zap.String("peer_id", peerID),
			zap.Error(err))
		return
	}

	w.logger.Debug("Sent binary message on data channel",
		zap.String("peer_id", peerID),
		zap.Int("data_size", len(data)))
}

// RequestFile requests a file from a peer
func (w *WebRTCTransport) RequestFile(ctx context.Context, peerID, fileID string) error {
	// Register the transfer
	transferID := fmt.Sprintf("%s:%s", peerID, fileID)
	w.transfersMu.Lock()
	w.activeTransfers[transferID] = true
	w.transfersMu.Unlock()

	// Create a file request message
	request := map[string]interface{}{
		"type":      "file_request",
		"file_id":   fileID,
		"timestamp": time.Now().Unix(),
	}

	// Send the request
	w.sendDataChannelMessage(peerID, request)

	w.logger.Info("Requested file from peer",
		zap.String("peer_id", peerID),
		zap.String("file_id", fileID))

	return nil
}

// RequestChunk requests a chunk from a peer
func (w *WebRTCTransport) RequestChunk(ctx context.Context, peerID, fileID string, chunkIndex int) error {
	// Check if this is an active transfer
	transferID := fmt.Sprintf("%s:%s", peerID, fileID)
	w.transfersMu.RLock()
	active := w.activeTransfers[transferID]
	w.transfersMu.RUnlock()

	if !active {
		return fmt.Errorf("transfer not active")
	}

	// Create a chunk request message
	request := map[string]interface{}{
		"type":        "chunk_request",
		"file_id":     fileID,
		"chunk_index": chunkIndex,
		"timestamp":   time.Now().Unix(),
	}

	// Send the request
	w.sendDataChannelMessage(peerID, request)

	w.logger.Debug("Requested chunk from peer",
		zap.String("peer_id", peerID),
		zap.String("file_id", fileID),
		zap.Int("chunk_index", chunkIndex))

	return nil
}

// CancelTransfer cancels a transfer with a peer
func (w *WebRTCTransport) CancelTransfer(peerID, fileID string) error {
	// Remove from active transfers
	transferID := fmt.Sprintf("%s:%s", peerID, fileID)
	w.transfersMu.Lock()
	delete(w.activeTransfers, transferID)
	w.transfersMu.Unlock()

	// Send a cancel message
	request := map[string]interface{}{
		"type":      "transfer_cancel",
		"file_id":   fileID,
		"timestamp": time.Now().Unix(),
	}

	// Send the request
	w.sendDataChannelMessage(peerID, request)

	w.logger.Info("Cancelled transfer with peer",
		zap.String("peer_id", peerID),
		zap.String("file_id", fileID))

	return nil
}

// CompleteTransfer marks a transfer as complete
func (w *WebRTCTransport) CompleteTransfer(peerID, fileID string) error {
	// Remove from active transfers
	transferID := fmt.Sprintf("%s:%s", peerID, fileID)
	w.transfersMu.Lock()
	delete(w.activeTransfers, transferID)
	w.transfersMu.Unlock()

	// Send a complete message
	request := map[string]interface{}{
		"type":      "transfer_complete",
		"file_id":   fileID,
		"timestamp": time.Now().Unix(),
	}

	// Send the request
	w.sendDataChannelMessage(peerID, request)

	w.logger.Info("Completed transfer with peer",
		zap.String("peer_id", peerID),
		zap.String("file_id", fileID))

	return nil
}

// Close closes all peer connections
func (w *WebRTCTransport) Close() {
	w.mu.Lock()
	defer w.mu.Unlock()

	for peerID, pc := range w.peerConnections {
		w.logger.Info("Closing peer connection", zap.String("peer_id", peerID))
		pc.Close()
	}

	w.peerConnections = make(map[string]*webrtc.PeerConnection)
	w.dataChannels = make(map[string]*webrtc.DataChannel)

	// Clear active transfers
	w.transfersMu.Lock()
	w.activeTransfers = make(map[string]bool)
	w.transfersMu.Unlock()
}

// SendDataChannelMessage sends a message to a peer over the data channel
func (w *WebRTCTransport) SendDataChannelMessage(peerID string, message interface{}) error {
	w.mu.RLock()
	dataChannel, exists := w.dataChannels[peerID]
	w.mu.RUnlock()

	if !exists {
		return fmt.Errorf("data channel not found for peer %s", peerID)
	}

	// Serialize the message
	data, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to serialize message: %w", err)
	}

	// Send the message
	if err := dataChannel.Send(data); err != nil {
		return fmt.Errorf("failed to send message on data channel: %w", err)
	}

	w.logger.Debug("Sent message on data channel",
		zap.String("peer_id", peerID),
		zap.String("message_type", message.(map[string]interface{})["type"].(string)))

	return nil
}
