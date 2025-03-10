package node

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/pion/webrtc/v3"
	"go.uber.org/zap"
)

// WebRTCConfig contains configuration for WebRTC connections
type WebRTCConfig struct {
	STUNServers []string `json:"stun_servers"`
	TURNServers []string `json:"turn_servers"`
	Username    string   `json:"username"`
	Credential  string   `json:"credential"`
	ICETimeout  int      `json:"ice_timeout"` // in seconds
	MaxRetries  int      `json:"max_retries"`
}

// DefaultWebRTCConfig returns a default WebRTC configuration
func DefaultWebRTCConfig() WebRTCConfig {
	return WebRTCConfig{
		STUNServers: []string{"stun:stun.l.google.com:19302", "stun:stun1.l.google.com:19302"},
		TURNServers: []string{},
		Username:    "",
		Credential:  "",
		ICETimeout:  30,
		MaxRetries:  3,
	}
}

// PeerConnectionState represents the state of a peer connection
type PeerConnectionState int

const (
	// PeerConnectionStateNew indicates the connection is new
	PeerConnectionStateNew PeerConnectionState = iota
	// PeerConnectionStateConnecting indicates the connection is connecting
	PeerConnectionStateConnecting
	// PeerConnectionStateConnected indicates the connection is connected
	PeerConnectionStateConnected
	// PeerConnectionStateDisconnected indicates the connection is disconnected
	PeerConnectionStateDisconnected
	// PeerConnectionStateFailed indicates the connection failed
	PeerConnectionStateFailed
	// PeerConnectionStateClosed indicates the connection is closed
	PeerConnectionStateClosed
)

// String returns a string representation of the peer connection state
func (s PeerConnectionState) String() string {
	switch s {
	case PeerConnectionStateNew:
		return "new"
	case PeerConnectionStateConnecting:
		return "connecting"
	case PeerConnectionStateConnected:
		return "connected"
	case PeerConnectionStateDisconnected:
		return "disconnected"
	case PeerConnectionStateFailed:
		return "failed"
	case PeerConnectionStateClosed:
		return "closed"
	default:
		return "unknown"
	}
}

// PeerConnection represents a WebRTC connection to a peer
type PeerConnection struct {
	PeerID            string
	Connection        *webrtc.PeerConnection
	DataChannels      map[string]*webrtc.DataChannel
	State             PeerConnectionState
	LastActivity      time.Time
	PendingCandidates []webrtc.ICECandidateInit
	mu                sync.RWMutex
}

// WebRTCManager manages WebRTC connections to peers
type WebRTCManager struct {
	logger           *zap.Logger
	config           WebRTCConfig
	nodeID           string
	peerConnections  map[string]*PeerConnection
	mu               sync.RWMutex
	ctx              context.Context
	cancel           context.CancelFunc
	onPeerConnected  func(peerID string)
	onPeerDisconnect func(peerID string)
	messaging        *WebRTCMessaging
}

// NewWebRTCManager creates a new WebRTC manager
func NewWebRTCManager(logger *zap.Logger, config WebRTCConfig, nodeID string) *WebRTCManager {
	ctx, cancel := context.WithCancel(context.Background())
	manager := &WebRTCManager{
		logger:          logger,
		config:          config,
		nodeID:          nodeID,
		peerConnections: make(map[string]*PeerConnection),
		ctx:             ctx,
		cancel:          cancel,
	}
	return manager
}

// Start starts the WebRTC manager
func (w *WebRTCManager) Start() {
	go w.monitorConnections()
}

// Stop stops the WebRTC manager
func (w *WebRTCManager) Stop() {
	w.cancel()
	w.CloseAllConnections()
}

// SetPeerCallbacks sets the callbacks for peer connection events
func (w *WebRTCManager) SetPeerCallbacks(onConnect func(peerID string), onDisconnect func(peerID string)) {
	w.onPeerConnected = onConnect
	w.onPeerDisconnect = onDisconnect
}

// SetMessaging sets the WebRTC messaging handler
func (w *WebRTCManager) SetMessaging(messaging *WebRTCMessaging) {
	w.messaging = messaging
}

// CreatePeerConnection creates a new peer connection
func (w *WebRTCManager) CreatePeerConnection(peerID string) (*PeerConnection, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Check if connection already exists
	if conn, exists := w.peerConnections[peerID]; exists {
		return conn, nil
	}

	// Create ICE servers configuration
	iceServers := []webrtc.ICEServer{}

	// Add STUN servers
	if len(w.config.STUNServers) > 0 {
		iceServers = append(iceServers, webrtc.ICEServer{
			URLs: w.config.STUNServers,
		})
	}

	// Add TURN servers if configured
	if len(w.config.TURNServers) > 0 && w.config.Username != "" && w.config.Credential != "" {
		iceServers = append(iceServers, webrtc.ICEServer{
			URLs:       w.config.TURNServers,
			Username:   w.config.Username,
			Credential: w.config.Credential,
		})
	}

	// Create WebRTC configuration
	config := webrtc.Configuration{
		ICEServers:         iceServers,
		ICETransportPolicy: webrtc.ICETransportPolicyAll,
		BundlePolicy:       webrtc.BundlePolicyBalanced,
		RTCPMuxPolicy:      webrtc.RTCPMuxPolicyRequire,
	}

	// Create peer connection
	peerConnection, err := webrtc.NewPeerConnection(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create peer connection: %w", err)
	}

	// Create peer connection object
	conn := &PeerConnection{
		PeerID:            peerID,
		Connection:        peerConnection,
		DataChannels:      make(map[string]*webrtc.DataChannel),
		State:             PeerConnectionStateNew,
		LastActivity:      time.Now(),
		PendingCandidates: []webrtc.ICECandidateInit{},
	}

	// Set up event handlers
	peerConnection.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate == nil {
			return
		}
		w.logger.Debug("ICE candidate generated", zap.String("peerID", peerID), zap.String("candidate", candidate.String()))

		// Store the candidate for later signaling
		conn.mu.Lock()
		conn.PendingCandidates = append(conn.PendingCandidates, candidate.ToJSON())
		conn.mu.Unlock()
	})

	peerConnection.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		w.logger.Info("Peer connection state changed", zap.String("peerID", peerID), zap.String("state", state.String()))

		conn.mu.Lock()
		switch state {
		case webrtc.PeerConnectionStateConnected:
			conn.State = PeerConnectionStateConnected
			conn.LastActivity = time.Now()
			w.mu.RLock()
			if w.onPeerConnected != nil {
				go w.onPeerConnected(peerID)
			}
			w.mu.RUnlock()
		case webrtc.PeerConnectionStateDisconnected:
			conn.State = PeerConnectionStateDisconnected
		case webrtc.PeerConnectionStateFailed:
			conn.State = PeerConnectionStateFailed
		case webrtc.PeerConnectionStateClosed:
			conn.State = PeerConnectionStateClosed
			w.mu.RLock()
			if w.onPeerDisconnect != nil {
				go w.onPeerDisconnect(peerID)
			}
			w.mu.RUnlock()
		}
		conn.mu.Unlock()
	})

	peerConnection.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		w.logger.Debug("ICE connection state changed", zap.String("peerID", peerID), zap.String("state", state.String()))

		if state == webrtc.ICEConnectionStateChecking {
			// Start ICE timeout timer
			go func() {
				timer := time.NewTimer(time.Duration(w.config.ICETimeout) * time.Second)
				defer timer.Stop()

				select {
				case <-timer.C:
					// Check if still in checking state
					conn.mu.RLock()
					currentState := conn.State
					conn.mu.RUnlock()

					if currentState == PeerConnectionStateConnecting {
						w.logger.Warn("ICE connection timed out", zap.String("peerID", peerID))
						w.ClosePeerConnection(peerID)
					}
				case <-w.ctx.Done():
					return
				}
			}()
		}
	})

	peerConnection.OnDataChannel(func(dataChannel *webrtc.DataChannel) {
		w.logger.Info("Data channel received", zap.String("peerID", peerID), zap.String("label", dataChannel.Label()))

		conn.mu.Lock()
		conn.DataChannels[dataChannel.Label()] = dataChannel
		conn.mu.Unlock()

		// Set up data channel handlers
		dataChannel.OnOpen(func() {
			w.logger.Info("Data channel opened", zap.String("peerID", peerID), zap.String("label", dataChannel.Label()))
			conn.LastActivity = time.Now()
		})

		dataChannel.OnClose(func() {
			w.logger.Info("Data channel closed", zap.String("peerID", peerID), zap.String("label", dataChannel.Label()))
			conn.mu.Lock()
			delete(conn.DataChannels, dataChannel.Label())
			conn.mu.Unlock()
		})

		dataChannel.OnMessage(func(msg webrtc.DataChannelMessage) {
			conn.LastActivity = time.Now()

			// If this is a control channel, pass to messaging handler
			if dataChannel.Label() == "control" && w.messaging != nil {
				w.messaging.handleIncomingMessage(peerID, msg.Data)
			}
		})
	})

	// Store the connection
	w.peerConnections[peerID] = conn

	return conn, nil
}

// CreateOffer creates an offer for a peer connection
func (w *WebRTCManager) CreateOffer(peerID string) (webrtc.SessionDescription, error) {
	w.mu.RLock()
	conn, exists := w.peerConnections[peerID]
	w.mu.RUnlock()

	if !exists {
		return webrtc.SessionDescription{}, errors.New("peer connection does not exist")
	}

	// Create offer
	offer, err := conn.Connection.CreateOffer(nil)
	if err != nil {
		return webrtc.SessionDescription{}, fmt.Errorf("failed to create offer: %w", err)
	}

	// Set local description
	err = conn.Connection.SetLocalDescription(offer)
	if err != nil {
		return webrtc.SessionDescription{}, fmt.Errorf("failed to set local description: %w", err)
	}

	conn.mu.Lock()
	conn.State = PeerConnectionStateConnecting
	conn.mu.Unlock()

	return offer, nil
}

// HandleAnswer handles an answer from a peer
func (w *WebRTCManager) HandleAnswer(peerID string, answer webrtc.SessionDescription) error {
	w.mu.RLock()
	conn, exists := w.peerConnections[peerID]
	w.mu.RUnlock()

	if !exists {
		return errors.New("peer connection does not exist")
	}

	// Set remote description
	err := conn.Connection.SetRemoteDescription(answer)
	if err != nil {
		return fmt.Errorf("failed to set remote description: %w", err)
	}

	return nil
}

// HandleOffer handles an offer from a peer
func (w *WebRTCManager) HandleOffer(peerID string, offer webrtc.SessionDescription) (webrtc.SessionDescription, error) {
	// Create peer connection if it doesn't exist
	conn, err := w.CreatePeerConnection(peerID)
	if err != nil {
		return webrtc.SessionDescription{}, err
	}

	// Set remote description
	err = conn.Connection.SetRemoteDescription(offer)
	if err != nil {
		return webrtc.SessionDescription{}, fmt.Errorf("failed to set remote description: %w", err)
	}

	// Create answer
	answer, err := conn.Connection.CreateAnswer(nil)
	if err != nil {
		return webrtc.SessionDescription{}, fmt.Errorf("failed to create answer: %w", err)
	}

	// Set local description
	err = conn.Connection.SetLocalDescription(answer)
	if err != nil {
		return webrtc.SessionDescription{}, fmt.Errorf("failed to set local description: %w", err)
	}

	conn.mu.Lock()
	conn.State = PeerConnectionStateConnecting
	conn.mu.Unlock()

	return answer, nil
}

// AddICECandidate adds an ICE candidate to a peer connection
func (w *WebRTCManager) AddICECandidate(peerID string, candidate webrtc.ICECandidateInit) error {
	w.mu.RLock()
	conn, exists := w.peerConnections[peerID]
	w.mu.RUnlock()

	if !exists {
		return errors.New("peer connection does not exist")
	}

	// Add ICE candidate
	err := conn.Connection.AddICECandidate(candidate)
	if err != nil {
		return fmt.Errorf("failed to add ICE candidate: %w", err)
	}

	return nil
}

// GetPendingCandidates returns the pending ICE candidates for a peer connection
func (w *WebRTCManager) GetPendingCandidates(peerID string) ([]webrtc.ICECandidateInit, error) {
	w.mu.RLock()
	conn, exists := w.peerConnections[peerID]
	w.mu.RUnlock()

	if !exists {
		return nil, errors.New("peer connection does not exist")
	}

	conn.mu.RLock()
	candidates := make([]webrtc.ICECandidateInit, len(conn.PendingCandidates))
	copy(candidates, conn.PendingCandidates)
	conn.mu.RUnlock()

	return candidates, nil
}

// ClearPendingCandidates clears the pending ICE candidates for a peer connection
func (w *WebRTCManager) ClearPendingCandidates(peerID string) error {
	w.mu.RLock()
	conn, exists := w.peerConnections[peerID]
	w.mu.RUnlock()

	if !exists {
		return errors.New("peer connection does not exist")
	}

	conn.mu.Lock()
	conn.PendingCandidates = []webrtc.ICECandidateInit{}
	conn.mu.Unlock()

	return nil
}

// CreateDataChannel creates a data channel on a peer connection
func (w *WebRTCManager) CreateDataChannel(peerID string, label string, ordered bool, maxRetransmits *uint16) (*webrtc.DataChannel, error) {
	w.mu.RLock()
	conn, exists := w.peerConnections[peerID]
	w.mu.RUnlock()

	if !exists {
		return nil, errors.New("peer connection does not exist")
	}

	// Check if data channel already exists
	conn.mu.RLock()
	if dc, exists := conn.DataChannels[label]; exists {
		conn.mu.RUnlock()
		return dc, nil
	}
	conn.mu.RUnlock()

	// Create data channel
	dataChannel, err := conn.Connection.CreateDataChannel(label, &webrtc.DataChannelInit{
		Ordered:        &ordered,
		MaxRetransmits: maxRetransmits,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create data channel: %w", err)
	}

	// Store data channel
	conn.mu.Lock()
	conn.DataChannels[label] = dataChannel
	conn.mu.Unlock()

	// Set up data channel handlers
	dataChannel.OnOpen(func() {
		w.logger.Info("Data channel opened", zap.String("peerID", peerID), zap.String("label", dataChannel.Label()))
		conn.LastActivity = time.Now()
	})

	dataChannel.OnClose(func() {
		w.logger.Info("Data channel closed", zap.String("peerID", peerID), zap.String("label", dataChannel.Label()))
		conn.mu.Lock()
		delete(conn.DataChannels, dataChannel.Label())
		conn.mu.Unlock()
	})

	dataChannel.OnMessage(func(msg webrtc.DataChannelMessage) {
		conn.LastActivity = time.Now()

		// If this is a control channel, pass to messaging handler
		if dataChannel.Label() == "control" && w.messaging != nil {
			w.messaging.handleIncomingMessage(peerID, msg.Data)
		}
	})

	return dataChannel, nil
}

// GetDataChannel gets a data channel from a peer connection
func (w *WebRTCManager) GetDataChannel(peerID string, label string) (*webrtc.DataChannel, error) {
	w.mu.RLock()
	conn, exists := w.peerConnections[peerID]
	w.mu.RUnlock()

	if !exists {
		return nil, errors.New("peer connection does not exist")
	}

	conn.mu.RLock()
	dataChannel, exists := conn.DataChannels[label]
	conn.mu.RUnlock()

	if !exists {
		return nil, errors.New("data channel does not exist")
	}

	return dataChannel, nil
}

// SendData sends data over a data channel
func (w *WebRTCManager) SendData(peerID string, label string, data []byte) error {
	dataChannel, err := w.GetDataChannel(peerID, label)
	if err != nil {
		return err
	}

	if dataChannel.ReadyState() != webrtc.DataChannelStateOpen {
		return errors.New("data channel is not open")
	}

	return dataChannel.Send(data)
}

// ClosePeerConnection closes a peer connection
func (w *WebRTCManager) ClosePeerConnection(peerID string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	conn, exists := w.peerConnections[peerID]
	if !exists {
		return errors.New("peer connection does not exist")
	}

	// Close all data channels
	conn.mu.Lock()
	for _, dataChannel := range conn.DataChannels {
		if dataChannel.ReadyState() != webrtc.DataChannelStateClosed {
			dataChannel.Close()
		}
	}
	conn.mu.Unlock()

	// Close peer connection
	err := conn.Connection.Close()
	if err != nil {
		return fmt.Errorf("failed to close peer connection: %w", err)
	}

	// Remove from map
	delete(w.peerConnections, peerID)

	return nil
}

// CloseAllConnections closes all peer connections
func (w *WebRTCManager) CloseAllConnections() {
	w.mu.Lock()
	defer w.mu.Unlock()

	for peerID, conn := range w.peerConnections {
		// Close all data channels
		conn.mu.Lock()
		for _, dataChannel := range conn.DataChannels {
			if dataChannel.ReadyState() != webrtc.DataChannelStateClosed {
				dataChannel.Close()
			}
		}
		conn.mu.Unlock()

		// Close peer connection
		err := conn.Connection.Close()
		if err != nil {
			w.logger.Error("Failed to close peer connection", zap.String("peerID", peerID), zap.Error(err))
		}
	}

	// Clear map
	w.peerConnections = make(map[string]*PeerConnection)
}

// GetPeerConnectionState gets the state of a peer connection
func (w *WebRTCManager) GetPeerConnectionState(peerID string) (PeerConnectionState, error) {
	w.mu.RLock()
	conn, exists := w.peerConnections[peerID]
	w.mu.RUnlock()

	if !exists {
		return PeerConnectionStateClosed, errors.New("peer connection does not exist")
	}

	conn.mu.RLock()
	state := conn.State
	conn.mu.RUnlock()

	return state, nil
}

// GetConnectedPeers gets a list of connected peers
func (w *WebRTCManager) GetConnectedPeers() []string {
	w.mu.RLock()
	defer w.mu.RUnlock()

	peers := []string{}
	for peerID, conn := range w.peerConnections {
		conn.mu.RLock()
		if conn.State == PeerConnectionStateConnected {
			peers = append(peers, peerID)
		}
		conn.mu.RUnlock()
	}

	return peers
}

// monitorConnections monitors peer connections for inactivity
func (w *WebRTCManager) monitorConnections() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			w.checkInactiveConnections()
		case <-w.ctx.Done():
			return
		}
	}
}

// checkInactiveConnections checks for inactive peer connections
func (w *WebRTCManager) checkInactiveConnections() {
	w.mu.RLock()
	defer w.mu.RUnlock()

	now := time.Now()
	inactivityThreshold := 5 * time.Minute

	for peerID, conn := range w.peerConnections {
		conn.mu.RLock()
		state := conn.State
		lastActivity := conn.LastActivity
		conn.mu.RUnlock()

		// Close inactive connections
		if state == PeerConnectionStateConnected && now.Sub(lastActivity) > inactivityThreshold {
			w.logger.Warn("Closing inactive peer connection", zap.String("peerID", peerID), zap.Duration("inactivity", now.Sub(lastActivity)))
			go w.ClosePeerConnection(peerID)
		}
	}
}
