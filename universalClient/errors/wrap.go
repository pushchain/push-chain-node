package errors

import (
	"errors"
	"fmt"
)

// Wrap wraps an error with additional context
func Wrap(err error, message string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", message, err)
}

// Wrapf wraps an error with formatted message
func Wrapf(err error, format string, args ...interface{}) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", fmt.Sprintf(format, args...), err)
}

// WrapChainError wraps an error as a ChainError if it isn't already one
func WrapChainError(err error, code ErrorCode, chain, message string) *ChainError {
	if err == nil {
		return nil
	}
	
	// If it's already a ChainError, add context
	var chainErr *ChainError
	if errors.As(err, &chainErr) {
		// Preserve original error but add new context
		chainErr.Context["wrapped_message"] = message
		if chain != "" && chainErr.Chain == "" {
			chainErr.Chain = chain
		}
		return chainErr
	}
	
	// Create new ChainError
	return NewChainError(code, chain, message, err)
}

// Is checks if an error is of a specific type
func Is(err error, target error) bool {
	return errors.Is(err, target)
}

// As checks if an error can be assigned to a target type
func As(err error, target interface{}) bool {
	return errors.As(err, target)
}

// IsChainError checks if an error is a ChainError with specific code
func IsChainError(err error, code ErrorCode) bool {
	var chainErr *ChainError
	if errors.As(err, &chainErr) {
		return chainErr.Code == code
	}
	return false
}

// IsRetryable checks if an error is retryable
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	
	var chainErr *ChainError
	if errors.As(err, &chainErr) {
		return chainErr.IsRetryable()
	}
	
	// Check for common retryable error patterns
	errStr := err.Error()
	retryablePatterns := []string{
		"connection refused",
		"connection reset",
		"timeout",
		"temporary failure",
		"too many requests",
		"rate limit",
	}
	
	for _, pattern := range retryablePatterns {
		if contains(errStr, pattern) {
			return true
		}
	}
	
	return false
}

// GetSeverity returns the severity of an error
func GetSeverity(err error) Severity {
	if err == nil {
		return SeverityInfo
	}
	
	var chainErr *ChainError
	if errors.As(err, &chainErr) {
		return chainErr.Severity
	}
	
	// Default severity based on error message patterns
	errStr := err.Error()
	if contains(errStr, "panic") || contains(errStr, "fatal") {
		return SeverityCritical
	}
	if contains(errStr, "failed") || contains(errStr, "error") {
		return SeverityHigh
	}
	if contains(errStr, "warning") {
		return SeverityMedium
	}
	
	return SeverityLow
}

// contains checks if a string contains a substring (case-insensitive)
func contains(s, substr string) bool {
	return len(s) >= len(substr) && 
		(s == substr || 
		 len(s) > 0 && len(substr) > 0 && 
		 containsIgnoreCase(s, substr))
}

// containsIgnoreCase checks if string contains substring ignoring case
func containsIgnoreCase(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	if len(s) < len(substr) {
		return false
	}
	
	// Simple case-insensitive contains
	for i := 0; i <= len(s)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			if toLower(s[i+j]) != toLower(substr[j]) {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// toLower converts a byte to lowercase
func toLower(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + 32
	}
	return b
}