package authz

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestKeyTypeString(t *testing.T) {
	tests := []struct {
		name     string
		keyType  KeyType
		expected string
	}{
		{
			name:     "UniversalValidatorHotKey",
			keyType:  UniversalValidatorHotKey,
			expected: "UniversalValidatorHotKey",
		},
		{
			name:     "Unknown key type",
			keyType:  KeyType(999),
			expected: "Unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.keyType.String()
			assert.Equal(t, tt.expected, result)
		})
	}
}

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

func TestGetAllAllowedMsgTypes(t *testing.T) {
	allowedTypes := GetAllAllowedMsgTypes()

	// Should return a copy, not the original slice
	assert.NotNil(t, allowedTypes)
	assert.Equal(t, len(AllowedMsgTypes), len(allowedTypes))

	// Should contain expected default message types
	expectedTypes := []string{
		"/cosmos.bank.v1beta1.MsgSend",
		"/cosmos.staking.v1beta1.MsgDelegate",
		"/cosmos.staking.v1beta1.MsgUndelegate",
		"/cosmos.gov.v1beta1.MsgVote",
	}

	for _, expected := range expectedTypes {
		assert.Contains(t, allowedTypes, expected)
	}

	// Modifying returned slice should not affect original
	originalLen := len(AllowedMsgTypes)
	allowedTypes[0] = "modified"
	assert.Equal(t, originalLen, len(AllowedMsgTypes))
	assert.NotEqual(t, "modified", AllowedMsgTypes[0])
}

func TestAllowedMsgTypesConstant(t *testing.T) {
	// Verify that the AllowedMsgTypes slice contains expected values
	assert.NotNil(t, AllowedMsgTypes)
	assert.Greater(t, len(AllowedMsgTypes), 0)

	// Check for specific expected default message types
	expectedTypes := map[string]bool{
		"/cosmos.bank.v1beta1.MsgSend":          true,
		"/cosmos.staking.v1beta1.MsgDelegate":   true,
		"/cosmos.staking.v1beta1.MsgUndelegate": true,
		"/cosmos.gov.v1beta1.MsgVote":           true,
	}

	for _, msgType := range AllowedMsgTypes {
		// Each message type should be properly formatted
		assert.NotEmpty(t, msgType)
		assert.Contains(t, msgType, "/cosmos.")
		
		// Remove from expected types if found
		delete(expectedTypes, msgType)
	}

	// All expected types should have been found
	assert.Empty(t, expectedTypes, "Missing expected message types: %v", expectedTypes)
}

// Test the new configuration functions
func TestSetAllowedMsgTypes(t *testing.T) {
	// Save original state
	original := make([]string, len(AllowedMsgTypes))
	copy(original, AllowedMsgTypes)
	defer func() {
		AllowedMsgTypes = original
	}()

	// Test setting custom message types
	custom := []string{"/custom.module.MsgCustom", "/another.module.MsgAnother"}
	SetAllowedMsgTypes(custom)

	assert.Equal(t, len(custom), len(AllowedMsgTypes))
	assert.True(t, IsAllowedMsgType("/custom.module.MsgCustom"))
	assert.True(t, IsAllowedMsgType("/another.module.MsgAnother"))
	assert.False(t, IsAllowedMsgType("/cosmos.bank.v1beta1.MsgSend"))
}

func TestUseDefaultMsgTypes(t *testing.T) {
	// Save original state
	original := make([]string, len(AllowedMsgTypes))
	copy(original, AllowedMsgTypes)
	defer func() {
		AllowedMsgTypes = original
	}()

	// Set custom types first
	SetAllowedMsgTypes([]string{"/custom.module.MsgCustom"})
	assert.True(t, IsAllowedMsgType("/custom.module.MsgCustom"))

	// Switch back to default
	UseDefaultMsgTypes()
	assert.False(t, IsAllowedMsgType("/custom.module.MsgCustom"))
	assert.True(t, IsAllowedMsgType("/cosmos.bank.v1beta1.MsgSend"))
	assert.Equal(t, "default", GetMsgTypeCategory())
}

func TestUseUniversalValidatorMsgTypes(t *testing.T) {
	// Save original state
	original := make([]string, len(AllowedMsgTypes))
	copy(original, AllowedMsgTypes)
	defer func() {
		AllowedMsgTypes = original
	}()

	// Switch to Universal Validator types
	UseUniversalValidatorMsgTypes()
	assert.True(t, IsAllowedMsgType("/push.observer.MsgVoteOnObservedEvent"))
	assert.True(t, IsAllowedMsgType("/push.observer.MsgSubmitObservation"))
	assert.False(t, IsAllowedMsgType("/cosmos.bank.v1beta1.MsgSend"))
	assert.Equal(t, "universal-validator", GetMsgTypeCategory())
}

func TestGetMsgTypeCategory(t *testing.T) {
	// Save original state
	original := make([]string, len(AllowedMsgTypes))
	copy(original, AllowedMsgTypes)
	defer func() {
		AllowedMsgTypes = original
	}()

	// Test default category
	UseDefaultMsgTypes()
	assert.Equal(t, "default", GetMsgTypeCategory())

	// Test universal-validator category
	UseUniversalValidatorMsgTypes()
	assert.Equal(t, "universal-validator", GetMsgTypeCategory())

	// Test custom category
	SetAllowedMsgTypes([]string{"/custom.module.MsgCustom"})
	assert.Equal(t, "custom", GetMsgTypeCategory())
}
