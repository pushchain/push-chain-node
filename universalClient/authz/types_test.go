package authz

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsAllowedMsgType(t *testing.T) {
	// Test with default message types (current configuration)
	tests := []struct {
		name     string
		msgType  string
		expected bool
	}{
		{
			name:     "allowed UE vote inbound message",
			msgType:  "/ue.v1.MsgVoteInbound",
			expected: true,
		},
		{
			name:     "not allowed observer message",
			msgType:  "/push.observer.MsgVoteOnObservedEvent",
			expected: false,
		},
		{
			name:     "empty message type",
			msgType:  "",
			expected: false,
		},
		{
			name:     "invalid message type",
			msgType:  "invalid-message-type",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsAllowedMsgType(tt.msgType)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDefaultAllowedMsgTypes(t *testing.T) {
	// Should contain expected default message types
	expectedTypes := []string{
		"/ue.v1.MsgVoteInbound",
	}

	assert.Equal(t, len(expectedTypes), len(DefaultAllowedMsgTypes))
	for _, expected := range expectedTypes {
		assert.Contains(t, DefaultAllowedMsgTypes, expected)
	}
}

func TestDefaultAllowedMsgTypesFormat(t *testing.T) {
	// Verify that the DefaultAllowedMsgTypes slice contains expected values
	assert.NotNil(t, DefaultAllowedMsgTypes)
	assert.Greater(t, len(DefaultAllowedMsgTypes), 0)

	// Check for specific expected default message types
	expectedTypes := map[string]bool{
		"/ue.v1.MsgVoteInbound": true,
	}

	for _, msgType := range DefaultAllowedMsgTypes {
		// Each message type should be properly formatted
		assert.NotEmpty(t, msgType)
		// Check for ue module messages
		assert.True(t, msgType[:1] == "/", "Message type should start with /")
		
		// Remove from expected types if found
		delete(expectedTypes, msgType)
	}

	// All expected types should have been found
	assert.Empty(t, expectedTypes, "Missing expected message types: %v", expectedTypes)
}

