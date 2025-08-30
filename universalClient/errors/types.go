package errors

import (
	"fmt"
)

// ErrorCode represents different categories of errors
type ErrorCode string

const (
	// ErrCodeValidation indicates input validation errors
	ErrCodeValidation ErrorCode = "VALIDATION"
	
	// ErrCodeNetwork indicates network-related errors
	ErrCodeNetwork ErrorCode = "NETWORK"
	
	// ErrCodeDatabase indicates database operation errors
	ErrCodeDatabase ErrorCode = "DATABASE"
	
	// ErrCodeTransaction indicates transaction-related errors
	ErrCodeTransaction ErrorCode = "TRANSACTION"
	
	// ErrCodeConfig indicates configuration errors
	ErrCodeConfig ErrorCode = "CONFIG"
	
	// ErrCodeRPC indicates RPC-related errors
	ErrCodeRPC ErrorCode = "RPC"
	
	// ErrCodeGateway indicates gateway-related errors
	ErrCodeGateway ErrorCode = "GATEWAY"
	
	// ErrCodeRegistry indicates registry-related errors
	ErrCodeRegistry ErrorCode = "REGISTRY"
	
	// ErrCodeTimeout indicates timeout errors
	ErrCodeTimeout ErrorCode = "TIMEOUT"
	
	// ErrCodeInternal indicates internal system errors
	ErrCodeInternal ErrorCode = "INTERNAL"
)

// Severity represents the severity level of an error
type Severity string

const (
	// SeverityCritical indicates critical errors that require immediate attention
	SeverityCritical Severity = "CRITICAL"
	
	// SeverityHigh indicates high priority errors
	SeverityHigh Severity = "HIGH"
	
	// SeverityMedium indicates medium priority errors
	SeverityMedium Severity = "MEDIUM"
	
	// SeverityLow indicates low priority errors
	SeverityLow Severity = "LOW"
	
	// SeverityInfo indicates informational errors
	SeverityInfo Severity = "INFO"
)

// ChainError represents an error specific to a blockchain chain
type ChainError struct {
	Code     ErrorCode `json:"code"`
	Message  string    `json:"message"`
	Chain    string    `json:"chain,omitempty"`
	Severity Severity  `json:"severity"`
	Cause    error     `json:"-"`
	Context  map[string]interface{} `json:"context,omitempty"`
}

// NewChainError creates a new ChainError
func NewChainError(code ErrorCode, chain, message string, cause error) *ChainError {
	return &ChainError{
		Code:     code,
		Message:  message,
		Chain:    chain,
		Severity: determineSeverity(code),
		Cause:    cause,
		Context:  make(map[string]interface{}),
	}
}

// NewChainErrorWithContext creates a new ChainError with additional context
func NewChainErrorWithContext(code ErrorCode, chain, message string, cause error, context map[string]interface{}) *ChainError {
	return &ChainError{
		Code:     code,
		Message:  message,
		Chain:    chain,
		Severity: determineSeverity(code),
		Cause:    cause,
		Context:  context,
	}
}

// Error implements the error interface
func (e *ChainError) Error() string {
	if e.Chain != "" {
		return fmt.Sprintf("[%s:%s] %s: %s", e.Chain, e.Code, e.Severity, e.Message)
	}
	return fmt.Sprintf("[%s] %s: %s", e.Code, e.Severity, e.Message)
}

// Unwrap returns the underlying cause
func (e *ChainError) Unwrap() error {
	return e.Cause
}

// WithContext adds context to the error
func (e *ChainError) WithContext(key string, value interface{}) *ChainError {
	if e.Context == nil {
		e.Context = make(map[string]interface{})
	}
	e.Context[key] = value
	return e
}

// WithSeverity overrides the default severity
func (e *ChainError) WithSeverity(severity Severity) *ChainError {
	e.Severity = severity
	return e
}

// IsRetryable returns true if the error is retryable
func (e *ChainError) IsRetryable() bool {
	switch e.Code {
	case ErrCodeNetwork, ErrCodeRPC, ErrCodeTimeout:
		return true
	case ErrCodeDatabase:
		// Database errors might be retryable depending on the specific error
		return e.Severity != SeverityCritical
	default:
		return false
	}
}

// determineSeverity determines the default severity based on error code
func determineSeverity(code ErrorCode) Severity {
	switch code {
	case ErrCodeInternal:
		return SeverityCritical
	case ErrCodeDatabase, ErrCodeRegistry:
		return SeverityHigh
	case ErrCodeTransaction, ErrCodeGateway:
		return SeverityMedium
	case ErrCodeNetwork, ErrCodeRPC, ErrCodeTimeout:
		return SeverityMedium
	case ErrCodeValidation, ErrCodeConfig:
		return SeverityLow
	default:
		return SeverityInfo
	}
}

// ErrorGroup represents a collection of errors
type ErrorGroup struct {
	Errors []error
}

// NewErrorGroup creates a new error group
func NewErrorGroup() *ErrorGroup {
	return &ErrorGroup{
		Errors: make([]error, 0),
	}
}

// Add adds an error to the group
func (eg *ErrorGroup) Add(err error) {
	if err != nil {
		eg.Errors = append(eg.Errors, err)
	}
}

// HasErrors returns true if there are any errors
func (eg *ErrorGroup) HasErrors() bool {
	return len(eg.Errors) > 0
}

// Error implements the error interface
func (eg *ErrorGroup) Error() string {
	if len(eg.Errors) == 0 {
		return ""
	}
	if len(eg.Errors) == 1 {
		return eg.Errors[0].Error()
	}
	return fmt.Sprintf("%d errors occurred: %v", len(eg.Errors), eg.Errors[0])
}

// GetErrors returns all errors
func (eg *ErrorGroup) GetErrors() []error {
	return eg.Errors
}

// Common error constructors

// NewValidationError creates a validation error
func NewValidationError(chain, message string) *ChainError {
	return NewChainError(ErrCodeValidation, chain, message, nil)
}

// NewNetworkError creates a network error
func NewNetworkError(chain, message string, cause error) *ChainError {
	return NewChainError(ErrCodeNetwork, chain, message, cause)
}

// NewDatabaseError creates a database error
func NewDatabaseError(chain, message string, cause error) *ChainError {
	return NewChainError(ErrCodeDatabase, chain, message, cause)
}

// NewTransactionError creates a transaction error
func NewTransactionError(chain, message string, cause error) *ChainError {
	return NewChainError(ErrCodeTransaction, chain, message, cause)
}

// NewConfigError creates a configuration error
func NewConfigError(chain, message string) *ChainError {
	return NewChainError(ErrCodeConfig, chain, message, nil)
}

// NewRPCError creates an RPC error
func NewRPCError(chain, message string, cause error) *ChainError {
	return NewChainError(ErrCodeRPC, chain, message, cause)
}

// NewGatewayError creates a gateway error
func NewGatewayError(chain, message string, cause error) *ChainError {
	return NewChainError(ErrCodeGateway, chain, message, cause)
}

// NewRegistryError creates a registry error
func NewRegistryError(message string, cause error) *ChainError {
	return NewChainError(ErrCodeRegistry, "", message, cause)
}

// NewTimeoutError creates a timeout error
func NewTimeoutError(chain, message string) *ChainError {
	return NewChainError(ErrCodeTimeout, chain, message, nil)
}

// NewInternalError creates an internal error
func NewInternalError(chain, message string, cause error) *ChainError {
	return NewChainError(ErrCodeInternal, chain, message, cause)
}