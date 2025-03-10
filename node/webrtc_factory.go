package node

import (
	"go.uber.org/zap"

	"github.com/TFMV/furymesh/common"
	"github.com/TFMV/furymesh/file"
)

// WebRTCFactory creates WebRTC components
type WebRTCFactory struct {
	logger *zap.Logger
	nodeID string
}

// NewWebRTCFactory creates a new WebRTC factory
func NewWebRTCFactory(logger *zap.Logger, nodeID string) *WebRTCFactory {
	return &WebRTCFactory{
		logger: logger,
		nodeID: nodeID,
	}
}

// CreateWebRTCComponents creates WebRTC components and integrates them with the file manager
func (f *WebRTCFactory) CreateWebRTCComponents(fileManager *FileManager) error {
	// Create WebRTC manager
	webrtcMgr := NewWebRTCManager(f.logger, DefaultWebRTCConfig(), f.nodeID)

	// Create WebRTC messaging
	messaging := NewWebRTCMessaging(f.logger, f.nodeID, webrtcMgr)

	// Create WebRTC transfer manager
	transferMgr := NewWebRTCTransferManager(
		f.logger,
		common.DefaultWebRTCTransferConfig(),
		webrtcMgr,
		messaging,
		fileManager,
	)

	// Create WebRTC transport adapter
	transportAdapter := file.NewWebRTCTransportAdapter(
		f.logger,
		fileManager.transferManager,
		fileManager.storageManager,
		transferMgr,
		f.nodeID,
	)

	// Set the WebRTC transport in the file manager
	fileManager.SetWebRTCTransport(transportAdapter)

	// Start the WebRTC components
	webrtcMgr.Start()
	transferMgr.Start()
	transportAdapter.Start()

	return nil
}
