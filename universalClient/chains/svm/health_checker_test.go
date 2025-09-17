package svm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSVMHealthChecker(t *testing.T) {
	expectedHash := "5eykt4UsFv8P8NJdTREpY1vzqKqZKvdpKuc147dw2N9d"
	checker := NewSVMHealthChecker(expectedHash)
	
	assert.NotNil(t, checker)
	assert.Equal(t, expectedHash, checker.expectedGenesisHash)
}

func TestSVMHealthChecker_InvalidClientType(t *testing.T) {
	checker := NewSVMHealthChecker("")
	
	// Pass an invalid client type
	invalidClient := "not-a-client"
	
	ctx := context.Background()
	err := checker.CheckHealth(ctx, invalidClient)
	
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid client type for SVM health check")
}