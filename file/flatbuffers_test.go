package file_test

import (
	"testing"

	"github.com/TFMV/furymesh/file"
	"github.com/TFMV/furymesh/fury/fury"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestFlatBuffersSerializer_GetMessageType(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	serializer := file.NewFlatBuffersSerializer(logger, "test-node-id")

	// Create a simple error message
	data := serializer.SerializeErrorMessage(404, "File not found", "test-file-id")
	assert.NotEmpty(t, data)

	// Get the message type
	msgType, err := serializer.GetMessageType(data)
	assert.NoError(t, err)
	assert.Equal(t, fury.MessageTypeErrorMessage, msgType)

	// Get the sender ID
	senderID, err := serializer.GetSenderID(data)
	assert.NoError(t, err)
	assert.Equal(t, "test-node-id", senderID)

	// Get the timestamp
	_, err = serializer.GetTimestamp(data)
	assert.NoError(t, err)
}
