package node

import (
	"encoding/json"
	"errors"
	"sync"

	"github.com/pion/webrtc/v3"
	"go.uber.org/zap"

	"github.com/TFMV/furymesh/file"
	"github.com/TFMV/furymesh/fury/fury"
)

// MessageType defines the type of message being sent
type MessageType string

const (
	// Message types for control messages
	MessageTypeNodeInfo     MessageType = "node_info"
	MessageTypePeerRequest  MessageType = "peer_request"
	MessageTypePeerResponse MessageType = "peer_response"
	MessageTypeFileInfo     MessageType = "file_info"
	MessageTypeFileRequest  MessageType = "file_request"
	MessageTypeFileResponse MessageType = "file_response"
	MessageTypePing         MessageType = "ping"
	MessageTypePong         MessageType = "pong"
)

// ControlMessage represents a small control message sent over WebRTC data channels
type ControlMessage struct {
	Type      MessageType     `json:"type"`
	Timestamp int64           `json:"timestamp"`
	SenderID  string          `json:"sender_id"`
	Payload   json.RawMessage `json:"payload"`
}

// MessageHandler is a function that handles a message
type MessageHandler func(peerID string, messageType fury.MessageType, data []byte) error

// WebRTCMessaging handles messaging over WebRTC
type WebRTCMessaging struct {
	logger     *zap.Logger
	nodeID     string
	webrtcMgr  *WebRTCManager
	serializer *file.FlatBuffersSerializer
	handlers   map[fury.MessageType]MessageHandler
	handlersMu sync.RWMutex
	metrics    *WebRTCMetrics
}

// WebRTCMetrics tracks metrics for WebRTC messaging
type WebRTCMetrics struct {
	MessagesSent     int64
	MessagesReceived int64
	BytesSent        int64
	BytesReceived    int64
	ErrorCount       int64
	mu               sync.RWMutex
}

// NewWebRTCMessaging creates a new WebRTC messaging handler
func NewWebRTCMessaging(logger *zap.Logger, nodeID string, webrtcMgr *WebRTCManager) *WebRTCMessaging {
	messaging := &WebRTCMessaging{
		logger:     logger,
		nodeID:     nodeID,
		webrtcMgr:  webrtcMgr,
		serializer: file.NewFlatBuffersSerializer(logger, nodeID),
		handlers:   make(map[fury.MessageType]MessageHandler),
		metrics:    &WebRTCMetrics{},
	}

	// Set the messaging handler in the WebRTC manager
	webrtcMgr.SetMessaging(messaging)

	return messaging
}

// RegisterHandler registers a handler for a message type
func (w *WebRTCMessaging) RegisterHandler(messageType fury.MessageType, handler MessageHandler) {
	w.handlersMu.Lock()
	defer w.handlersMu.Unlock()

	w.handlers[messageType] = handler
}

// UnregisterHandler unregisters a handler for a message type
func (w *WebRTCMessaging) UnregisterHandler(messageType fury.MessageType) {
	w.handlersMu.Lock()
	defer w.handlersMu.Unlock()

	delete(w.handlers, messageType)
}

// SendFileMetadata sends file metadata to a peer
func (w *WebRTCMessaging) SendFileMetadata(peerID string, metadata *file.ChunkMetadata) error {
	// Serialize the metadata
	data := w.serializer.SerializeFileMetadata(metadata)

	// Send the data
	err := w.sendMessage(peerID, data)
	if err != nil {
		return err
	}

	return nil
}

// SendFileChunk sends a file chunk to a peer
func (w *WebRTCMessaging) SendFileChunk(peerID string, fileID string, chunkIndex int, data []byte, encrypted bool, compression string) error {
	// Serialize the chunk
	chunkData := w.serializer.SerializeFileChunk(fileID, chunkIndex, data, encrypted, compression)

	// Send the data
	err := w.sendMessage(peerID, chunkData)
	if err != nil {
		return err
	}

	return nil
}

// SendChunkRequest sends a chunk request to a peer
func (w *WebRTCMessaging) SendChunkRequest(peerID string, fileID string, chunkIndex int, priority uint8) error {
	// Serialize the request
	requestData := w.serializer.SerializeChunkRequest(fileID, chunkIndex, priority)

	// Send the data
	err := w.sendMessage(peerID, requestData)
	if err != nil {
		return err
	}

	return nil
}

// SendTransferStatus sends transfer status to a peer
func (w *WebRTCMessaging) SendTransferStatus(peerID string, transferID string, stats *file.TransferStats) error {
	// Serialize the status
	statusData := w.serializer.SerializeTransferStatus(transferID, stats)

	// Send the data
	err := w.sendMessage(peerID, statusData)
	if err != nil {
		return err
	}

	return nil
}

// SendErrorMessage sends an error message to a peer
func (w *WebRTCMessaging) SendErrorMessage(peerID string, code int32, message, context string) error {
	// Serialize the error
	errorData := w.serializer.SerializeErrorMessage(code, message, context)

	// Send the data
	err := w.sendMessage(peerID, errorData)
	if err != nil {
		return err
	}

	return nil
}

// sendMessage sends a message to a peer
func (w *WebRTCMessaging) sendMessage(peerID string, data []byte) error {
	// Get or create the control data channel
	dataChannel, err := w.webrtcMgr.CreateDataChannel(peerID, "control", true, nil)
	if err != nil {
		return err
	}

	// Check if the data channel is open
	if dataChannel.ReadyState() != webrtc.DataChannelStateOpen {
		return errors.New("data channel is not open")
	}

	// Send the data
	err = dataChannel.Send(data)
	if err != nil {
		return err
	}

	// Update metrics
	w.metrics.mu.Lock()
	w.metrics.MessagesSent++
	w.metrics.BytesSent += int64(len(data))
	w.metrics.mu.Unlock()

	return nil
}

// handleIncomingMessage handles an incoming message
func (w *WebRTCMessaging) handleIncomingMessage(peerID string, data []byte) {
	// Update metrics
	w.metrics.mu.Lock()
	w.metrics.MessagesReceived++
	w.metrics.BytesReceived += int64(len(data))
	w.metrics.mu.Unlock()

	// Get the message type
	messageType, err := w.serializer.GetMessageType(data)
	if err != nil {
		w.logger.Error("Failed to get message type", zap.Error(err))
		w.metrics.mu.Lock()
		w.metrics.ErrorCount++
		w.metrics.mu.Unlock()
		return
	}

	// Get the handler for this message type
	w.handlersMu.RLock()
	handler, exists := w.handlers[messageType]
	w.handlersMu.RUnlock()

	if !exists {
		w.logger.Warn("No handler for message type", zap.Int("messageType", int(messageType)))
		return
	}

	// Call the handler
	err = handler(peerID, messageType, data)
	if err != nil {
		w.logger.Error("Handler failed", zap.Error(err), zap.Int("messageType", int(messageType)))
		w.metrics.mu.Lock()
		w.metrics.ErrorCount++
		w.metrics.mu.Unlock()
	}
}

// GetMetrics gets the WebRTC messaging metrics
func (w *WebRTCMessaging) GetMetrics() map[string]interface{} {
	w.metrics.mu.RLock()
	defer w.metrics.mu.RUnlock()

	return map[string]interface{}{
		"messages_sent":     w.metrics.MessagesSent,
		"messages_received": w.metrics.MessagesReceived,
		"bytes_sent":        w.metrics.BytesSent,
		"bytes_received":    w.metrics.BytesReceived,
		"error_count":       w.metrics.ErrorCount,
	}
}

// ResetMetrics resets the WebRTC messaging metrics
func (w *WebRTCMessaging) ResetMetrics() {
	w.metrics.mu.Lock()
	defer w.metrics.mu.Unlock()

	w.metrics.MessagesSent = 0
	w.metrics.MessagesReceived = 0
	w.metrics.BytesSent = 0
	w.metrics.BytesReceived = 0
	w.metrics.ErrorCount = 0
}

// GetSerializer returns the FlatBuffers serializer
func (w *WebRTCMessaging) GetSerializer() *file.FlatBuffersSerializer {
	return w.serializer
}
