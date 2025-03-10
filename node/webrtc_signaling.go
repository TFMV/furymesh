package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/pion/webrtc/v3"
	"go.uber.org/zap"
)

// DHTNode represents a DHT node interface for signaling
type DHTNode interface {
	// SendMessage sends a message to a peer
	SendMessage(peerID string, messageType string, data []byte) error

	// RegisterMessageHandler registers a handler for incoming messages
	RegisterMessageHandler(messageType string, handler func(senderID string, data []byte))
}

// SignalingMessage represents a signaling message
type SignalingMessage struct {
	Type       string                     `json:"type"`
	SenderID   string                     `json:"sender_id"`
	ReceiverID string                     `json:"receiver_id"`
	Offer      *webrtc.SessionDescription `json:"offer,omitempty"`
	Answer     *webrtc.SessionDescription `json:"answer,omitempty"`
	Candidate  *webrtc.ICECandidateInit   `json:"candidate,omitempty"`
	Timestamp  int64                      `json:"timestamp"`
}

// SignalingTransport is an interface for sending signaling messages
type SignalingTransport interface {
	// SendSignalingMessage sends a signaling message to a peer
	SendSignalingMessage(peerID string, message []byte) error

	// RegisterSignalingHandler registers a handler for incoming signaling messages
	RegisterSignalingHandler(handler func(peerID string, message []byte))
}

// WebRTCSignaling handles WebRTC signaling
type WebRTCSignaling struct {
	logger            *zap.Logger
	nodeID            string
	webrtcMgr         *WebRTCManager
	transport         SignalingTransport
	pendingOffers     map[string]*SignalingMessage
	pendingAnswers    map[string]*SignalingMessage
	pendingCandidates map[string][]*webrtc.ICECandidateInit
	mu                sync.RWMutex
	ctx               context.Context
	cancel            context.CancelFunc
	onPeerConnected   func(peerID string)
}

// NewWebRTCSignaling creates a new WebRTC signaling service
func NewWebRTCSignaling(logger *zap.Logger, nodeID string, webrtcMgr *WebRTCManager, transport SignalingTransport) *WebRTCSignaling {
	ctx, cancel := context.WithCancel(context.Background())
	signaling := &WebRTCSignaling{
		logger:            logger,
		nodeID:            nodeID,
		webrtcMgr:         webrtcMgr,
		transport:         transport,
		pendingOffers:     make(map[string]*SignalingMessage),
		pendingAnswers:    make(map[string]*SignalingMessage),
		pendingCandidates: make(map[string][]*webrtc.ICECandidateInit),
		ctx:               ctx,
		cancel:            cancel,
	}

	// Register signaling handler
	transport.RegisterSignalingHandler(signaling.handleSignalingMessage)

	return signaling
}

// Start starts the WebRTC signaling service
func (w *WebRTCSignaling) Start() {
	go w.monitorPendingConnections()
}

// Stop stops the WebRTC signaling service
func (w *WebRTCSignaling) Stop() {
	w.cancel()
}

// SetPeerConnectedCallback sets the callback for when a peer is connected
func (w *WebRTCSignaling) SetPeerConnectedCallback(callback func(peerID string)) {
	w.onPeerConnected = callback
}

// InitiateConnection initiates a WebRTC connection to a peer
func (w *WebRTCSignaling) InitiateConnection(peerID string) error {
	// Create peer connection
	_, err := w.webrtcMgr.CreatePeerConnection(peerID)
	if err != nil {
		return fmt.Errorf("failed to create peer connection: %w", err)
	}

	// Create offer
	offer, err := w.webrtcMgr.CreateOffer(peerID)
	if err != nil {
		return fmt.Errorf("failed to create offer: %w", err)
	}

	// Create signaling message
	message := SignalingMessage{
		Type:       "offer",
		SenderID:   w.nodeID,
		ReceiverID: peerID,
		Offer:      &offer,
		Timestamp:  time.Now().UnixNano(),
	}

	// Marshal message
	messageBytes, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal offer: %w", err)
	}

	// Send offer
	err = w.transport.SendSignalingMessage(peerID, messageBytes)
	if err != nil {
		return fmt.Errorf("failed to send offer: %w", err)
	}

	w.logger.Info("Sent WebRTC offer", zap.String("peerID", peerID))

	// Start ICE candidate gathering
	go w.gatherAndSendCandidates(peerID)

	return nil
}

// handleSignalingMessage handles incoming signaling messages
func (w *WebRTCSignaling) handleSignalingMessage(peerID string, messageBytes []byte) {
	// Unmarshal message
	var message SignalingMessage
	if err := json.Unmarshal(messageBytes, &message); err != nil {
		w.logger.Error("Failed to unmarshal signaling message", zap.Error(err))
		return
	}

	// Verify sender ID
	if message.SenderID != peerID {
		w.logger.Warn("Sender ID mismatch in signaling message",
			zap.String("expected", peerID),
			zap.String("actual", message.SenderID))
		return
	}

	// Handle message based on type
	switch message.Type {
	case "offer":
		w.handleOffer(peerID, &message)
	case "answer":
		w.handleAnswer(peerID, &message)
	case "candidate":
		w.handleCandidate(peerID, &message)
	default:
		w.logger.Warn("Unknown signaling message type", zap.String("type", message.Type))
	}
}

// handleOffer handles an offer from a peer
func (w *WebRTCSignaling) handleOffer(peerID string, message *SignalingMessage) {
	if message.Offer == nil {
		w.logger.Error("Received offer message with no offer", zap.String("peerID", peerID))
		return
	}

	w.logger.Info("Received WebRTC offer", zap.String("peerID", peerID))

	// Create answer
	answer, err := w.webrtcMgr.HandleOffer(peerID, *message.Offer)
	if err != nil {
		w.logger.Error("Failed to handle offer", zap.Error(err), zap.String("peerID", peerID))
		return
	}

	// Create signaling message
	responseMessage := SignalingMessage{
		Type:       "answer",
		SenderID:   w.nodeID,
		ReceiverID: peerID,
		Answer:     &answer,
		Timestamp:  time.Now().UnixNano(),
	}

	// Marshal message
	responseBytes, err := json.Marshal(responseMessage)
	if err != nil {
		w.logger.Error("Failed to marshal answer", zap.Error(err))
		return
	}

	// Send answer
	err = w.transport.SendSignalingMessage(peerID, responseBytes)
	if err != nil {
		w.logger.Error("Failed to send answer", zap.Error(err))
		return
	}

	w.logger.Info("Sent WebRTC answer", zap.String("peerID", peerID))

	// Start ICE candidate gathering
	go w.gatherAndSendCandidates(peerID)

	// Apply any pending candidates
	w.mu.Lock()
	if candidates, exists := w.pendingCandidates[peerID]; exists && len(candidates) > 0 {
		w.logger.Info("Applying pending ICE candidates",
			zap.String("peerID", peerID),
			zap.Int("count", len(candidates)))

		for _, candidate := range candidates {
			err := w.webrtcMgr.AddICECandidate(peerID, *candidate)
			if err != nil {
				w.logger.Error("Failed to add pending ICE candidate", zap.Error(err))
			}
		}

		// Clear pending candidates
		delete(w.pendingCandidates, peerID)
	}
	w.mu.Unlock()
}

// handleAnswer handles an answer from a peer
func (w *WebRTCSignaling) handleAnswer(peerID string, message *SignalingMessage) {
	if message.Answer == nil {
		w.logger.Error("Received answer message with no answer", zap.String("peerID", peerID))
		return
	}

	w.logger.Info("Received WebRTC answer", zap.String("peerID", peerID))

	// Handle answer
	err := w.webrtcMgr.HandleAnswer(peerID, *message.Answer)
	if err != nil {
		w.logger.Error("Failed to handle answer", zap.Error(err), zap.String("peerID", peerID))
		return
	}

	// Apply any pending candidates
	w.mu.Lock()
	if candidates, exists := w.pendingCandidates[peerID]; exists && len(candidates) > 0 {
		w.logger.Info("Applying pending ICE candidates",
			zap.String("peerID", peerID),
			zap.Int("count", len(candidates)))

		for _, candidate := range candidates {
			err := w.webrtcMgr.AddICECandidate(peerID, *candidate)
			if err != nil {
				w.logger.Error("Failed to add pending ICE candidate", zap.Error(err))
			}
		}

		// Clear pending candidates
		delete(w.pendingCandidates, peerID)
	}
	w.mu.Unlock()
}

// handleCandidate handles an ICE candidate from a peer
func (w *WebRTCSignaling) handleCandidate(peerID string, message *SignalingMessage) {
	if message.Candidate == nil {
		w.logger.Error("Received candidate message with no candidate", zap.String("peerID", peerID))
		return
	}

	w.logger.Debug("Received ICE candidate", zap.String("peerID", peerID))

	// Check if we have a peer connection
	state, err := w.webrtcMgr.GetPeerConnectionState(peerID)
	if err != nil || state == PeerConnectionStateClosed {
		// Store candidate for later
		w.mu.Lock()
		if _, exists := w.pendingCandidates[peerID]; !exists {
			w.pendingCandidates[peerID] = []*webrtc.ICECandidateInit{}
		}
		w.pendingCandidates[peerID] = append(w.pendingCandidates[peerID], message.Candidate)
		w.mu.Unlock()

		w.logger.Debug("Stored ICE candidate for later", zap.String("peerID", peerID))
		return
	}

	// Add candidate
	err = w.webrtcMgr.AddICECandidate(peerID, *message.Candidate)
	if err != nil {
		w.logger.Error("Failed to add ICE candidate", zap.Error(err), zap.String("peerID", peerID))
		return
	}
}

// gatherAndSendCandidates gathers and sends ICE candidates to a peer
func (w *WebRTCSignaling) gatherAndSendCandidates(peerID string) {
	// Wait for candidates to be gathered
	time.Sleep(500 * time.Millisecond)

	// Get pending candidates
	candidates, err := w.webrtcMgr.GetPendingCandidates(peerID)
	if err != nil {
		w.logger.Error("Failed to get pending candidates", zap.Error(err), zap.String("peerID", peerID))
		return
	}

	if len(candidates) == 0 {
		w.logger.Debug("No ICE candidates to send", zap.String("peerID", peerID))
		return
	}

	w.logger.Info("Sending ICE candidates", zap.String("peerID", peerID), zap.Int("count", len(candidates)))

	// Send each candidate
	for _, candidate := range candidates {
		// Create signaling message
		message := SignalingMessage{
			Type:       "candidate",
			SenderID:   w.nodeID,
			ReceiverID: peerID,
			Candidate:  &candidate,
			Timestamp:  time.Now().UnixNano(),
		}

		// Marshal message
		messageBytes, err := json.Marshal(message)
		if err != nil {
			w.logger.Error("Failed to marshal candidate", zap.Error(err))
			continue
		}

		// Send candidate
		err = w.transport.SendSignalingMessage(peerID, messageBytes)
		if err != nil {
			w.logger.Error("Failed to send candidate", zap.Error(err))
			continue
		}
	}

	// Clear pending candidates
	err = w.webrtcMgr.ClearPendingCandidates(peerID)
	if err != nil {
		w.logger.Error("Failed to clear pending candidates", zap.Error(err))
	}
}

// monitorPendingConnections monitors pending connections for timeouts
func (w *WebRTCSignaling) monitorPendingConnections() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			w.checkPendingConnections()
		case <-w.ctx.Done():
			return
		}
	}
}

// checkPendingConnections checks for timed out pending connections
func (w *WebRTCSignaling) checkPendingConnections() {
	w.mu.Lock()
	defer w.mu.Unlock()

	now := time.Now().UnixNano()
	timeout := int64(2 * 60 * 1000000000) // 2 minutes in nanoseconds

	// Check pending offers
	for peerID, offer := range w.pendingOffers {
		if now-offer.Timestamp > timeout {
			w.logger.Warn("Offer timed out", zap.String("peerID", peerID))
			delete(w.pendingOffers, peerID)
		}
	}

	// Check pending answers
	for peerID, answer := range w.pendingAnswers {
		if now-answer.Timestamp > timeout {
			w.logger.Warn("Answer timed out", zap.String("peerID", peerID))
			delete(w.pendingAnswers, peerID)
		}
	}
}

// DHT-based signaling transport implementation
type DHTSignalingTransport struct {
	logger  *zap.Logger
	nodeID  string
	dht     *DHTNode
	handler func(peerID string, message []byte)
}

// NewDHTSignalingTransport creates a new DHT-based signaling transport
func NewDHTSignalingTransport(logger *zap.Logger, nodeID string, dht *DHTNode) *DHTSignalingTransport {
	return &DHTSignalingTransport{
		logger: logger,
		nodeID: nodeID,
		dht:    dht,
	}
}

// SendSignalingMessage sends a signaling message to a peer via DHT
func (d *DHTSignalingTransport) SendSignalingMessage(peerID string, message []byte) error {
	// Use DHT to send the message
	// This is a placeholder - actual implementation would depend on the DHT implementation
	if d.dht == nil {
		return errors.New("DHT node is not initialized")
	}

	// Send the message through DHT
	// d.dht.SendMessage(peerID, "webrtc-signaling", message)

	return nil
}

// RegisterSignalingHandler registers a handler for incoming signaling messages
func (d *DHTSignalingTransport) RegisterSignalingHandler(handler func(peerID string, message []byte)) {
	d.handler = handler

	// Register with DHT to receive messages
	// This is a placeholder - actual implementation would depend on the DHT implementation
	if d.dht != nil {
		// d.dht.RegisterMessageHandler("webrtc-signaling", func(senderID string, data []byte) {
		//     d.handler(senderID, data)
		// })
	}
}

// HandleIncomingMessage handles an incoming message from the DHT
func (d *DHTSignalingTransport) HandleIncomingMessage(senderID string, data []byte) {
	if d.handler != nil {
		d.handler(senderID, data)
	}
}
