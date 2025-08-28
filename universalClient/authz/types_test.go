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
			name:     "allowed bank send message (default)",
			msgType:  "/cosmos.bank.v1beta1.MsgSend",
			expected: true,
		},
		{
			name:     "allowed staking delegate message (default)",
			msgType:  "/cosmos.staking.v1beta1.MsgDelegate",
			expected: true,
		},
		{
			name:     "allowed gov vote message (default)",
			msgType:  "/cosmos.gov.v1beta1.MsgVote",
			expected: true,
		},
		{
			name:     "not allowed universal validator message (default config)",
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
		"/cosmos.bank.v1beta1.MsgSend",
		"/cosmos.staking.v1beta1.MsgDelegate",
		"/cosmos.staking.v1beta1.MsgUndelegate",
		"/cosmos.gov.v1beta1.MsgVote",
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
		"/cosmos.bank.v1beta1.MsgSend":          true,
		"/cosmos.staking.v1beta1.MsgDelegate":   true,
		"/cosmos.staking.v1beta1.MsgUndelegate": true,
		"/cosmos.gov.v1beta1.MsgVote":           true,
	}

	for _, msgType := range DefaultAllowedMsgTypes {
		// Each message type should be properly formatted
		assert.NotEmpty(t, msgType)
		assert.Contains(t, msgType, "/cosmos.")
		
		// Remove from expected types if found
		delete(expectedTypes, msgType)
	}

	// All expected types should have been found
	assert.Empty(t, expectedTypes, "Missing expected message types: %v", expectedTypes)
}

