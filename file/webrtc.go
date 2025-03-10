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

	// Chunk selection strategy
	chunkStrategy ChunkSelectionStrategy

	// Available peers for each file
	availablePeers   map[string]map[string][]int // fileID -> peerID -> []chunkIndex
	availablePeersMu sync.RWMutex

	// Context for cancellation
	ctx    context.Context
	cancel context.CancelFunc
}

// WebRTCConfig contains configuration for WebRTC connections
type WebRTCConfig struct {
	ICEServers []string
	// Add new configuration options
	MaxConcurrentChunks int
	RetryInterval       time.Duration
	MaxRetries          int
	IdleTimeout         time.Duration
	BufferSize          int
}

// DefaultWebRTCConfig returns a default WebRTC configuration
func DefaultWebRTCConfig() WebRTCConfig {
	return WebRTCConfig{
		ICEServers:          []string{"stun:stun.l.google.com:19302", "stun:stun1.l.google.com:19302"},
		MaxConcurrentChunks: 5,
		RetryInterval:       5 * time.Second,
		MaxRetries:          3,
		IdleTimeout:         30 * time.Second,
		BufferSize:          10, // Buffer 10 chunks ahead
	}
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

	// Create WebRTC configuration
	webrtcConfig := webrtc.Configuration{
		ICEServers: iceServers,
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &WebRTCTransport{
		logger:          logger,
		transferManager: transferManager,
		storageManager:  storageManager,
		peerConnections: make(map[string]*webrtc.PeerConnection),
		dataChannels:    make(map[string]*webrtc.DataChannel),
		config:          webrtcConfig,
		activeTransfers: make(map[string]bool),
		chunkStrategy:   GetRarestFirstStrategy(), // Default to rarest first
		availablePeers:  make(map[string]map[string][]int),
		ctx:             ctx,
		cancel:          cancel,
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

// Close closes the WebRTC transport
func (w *WebRTCTransport) Close() {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Cancel the context
	if w.cancel != nil {
		w.cancel()
	}

	// Close all peer connections
	for peerID, pc := range w.peerConnections {
		if err := pc.Close(); err != nil {
			w.logger.Error("Failed to close peer connection", zap.String("peerID", peerID), zap.Error(err))
		}
	}

	// Clear maps
	w.peerConnections = make(map[string]*webrtc.PeerConnection)
	w.dataChannels = make(map[string]*webrtc.DataChannel)
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

// RequestFileFromMultiplePeers requests a file from multiple peers
func (w *WebRTCTransport) RequestFileFromMultiplePeers(ctx context.Context, fileID string, peerIDs []string) error {
	w.logger.Info("Requesting file from multiple peers",
		zap.String("fileID", fileID),
		zap.Strings("peerIDs", peerIDs))

	if len(peerIDs) == 0 {
		return fmt.Errorf("no peers specified")
	}

	// Mark transfer as active
	w.transfersMu.Lock()
	w.activeTransfers[fileID] = true
	w.transfersMu.Unlock()

	// Create a context with cancellation
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Create a wait group to wait for all requests to complete
	var wg sync.WaitGroup
	errChan := make(chan error, len(peerIDs))

	// Request file metadata from all peers
	for _, peerID := range peerIDs {
		wg.Add(1)
		go func(pid string) {
			defer wg.Done()

			// Send file request message
			message := map[string]interface{}{
				"type":    "file_request",
				"file_id": fileID,
			}

			if err := w.SendDataChannelMessage(pid, message); err != nil {
				errChan <- fmt.Errorf("failed to send file request to peer %s: %w", pid, err)
				return
			}

			// Add peer to available peers for this file
			w.availablePeersMu.Lock()
			if _, exists := w.availablePeers[fileID]; !exists {
				w.availablePeers[fileID] = make(map[string][]int)
			}
			// We don't know which chunks this peer has yet, so leave it empty
			w.availablePeers[fileID][pid] = []int{}
			w.availablePeersMu.Unlock()
		}(peerID)
	}

	// Wait for all requests to complete or context to be cancelled
	go func() {
		wg.Wait()
		close(errChan)
	}()

	// Collect errors
	var errs []error
	for err := range errChan {
		errs = append(errs, err)
	}

	if len(errs) == len(peerIDs) {
		// All requests failed
		return fmt.Errorf("all file requests failed: %v", errs)
	}

	return nil
}

// AddPeerForTransfer adds a peer for a file transfer
func (w *WebRTCTransport) AddPeerForTransfer(fileID string, peerID string, availableChunks []int) error {
	w.logger.Info("Adding peer for transfer",
		zap.String("fileID", fileID),
		zap.String("peerID", peerID),
		zap.Ints("availableChunks", availableChunks))

	// Add peer to available peers for this file
	w.availablePeersMu.Lock()
	defer w.availablePeersMu.Unlock()

	if _, exists := w.availablePeers[fileID]; !exists {
		w.availablePeers[fileID] = make(map[string][]int)
	}
	w.availablePeers[fileID][peerID] = availableChunks

	return nil
}

// SetChunkSelectionStrategy sets the chunk selection strategy
func (w *WebRTCTransport) SetChunkSelectionStrategy(strategy ChunkSelectionStrategy) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.chunkStrategy = strategy
}

// GetAvailablePeersForFile gets the available peers for a file
func (w *WebRTCTransport) GetAvailablePeersForFile(fileID string) map[string][]int {
	w.availablePeersMu.RLock()
	defer w.availablePeersMu.RUnlock()

	if peers, exists := w.availablePeers[fileID]; exists {
		// Make a copy to avoid concurrent map access
		result := make(map[string][]int)
		for peerID, chunks := range peers {
			chunksCopy := make([]int, len(chunks))
			copy(chunksCopy, chunks)
			result[peerID] = chunksCopy
		}
		return result
	}

	return make(map[string][]int)
}

// CleanupCompletedTransfers cleans up completed transfers
func (w *WebRTCTransport) CleanupCompletedTransfers() {
	w.transfersMu.Lock()
	defer w.transfersMu.Unlock()

	// Get active transfers from the transfer manager
	activeTransfers := w.transferManager.ListActiveTransfers()

	// Find completed transfers (those that are in our active list but not in the transfer manager's active list)
	for fileID := range w.activeTransfers {
		if _, exists := activeTransfers[fileID]; !exists {
			// This transfer is no longer active, remove it
			delete(w.activeTransfers, fileID)

			// Clean up available peers for this file
			w.availablePeersMu.Lock()
			delete(w.availablePeers, fileID)
			w.availablePeersMu.Unlock()
		}
	}
}
