package errors

import (
	"context"
	"math"
	"time"
)

// RetryConfig configures retry behavior
type RetryConfig struct {
	MaxAttempts     int
	InitialDelay    time.Duration
	MaxDelay        time.Duration
	Multiplier      float64
	RetryableErrors []ErrorCode
}

// DefaultRetryConfig returns default retry configuration
func DefaultRetryConfig() *RetryConfig {
	return &RetryConfig{
		MaxAttempts:  3,
		InitialDelay: 1 * time.Second,
		MaxDelay:     30 * time.Second,
		Multiplier:   2.0,
		RetryableErrors: []ErrorCode{
			ErrCodeNetwork,
			ErrCodeRPC,
			ErrCodeTimeout,
		},
	}
}

// RetryFunc is a function that can be retried
type RetryFunc func() error

// RetryWithConfig retries a function with custom configuration
func RetryWithConfig(ctx context.Context, fn RetryFunc, config *RetryConfig) error {
	if config == nil {
		config = DefaultRetryConfig()
	}
	
	var lastErr error
	delay := config.InitialDelay
	
	for attempt := 1; attempt <= config.MaxAttempts; attempt++ {
		// Check context
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		
		// Try the function
		err := fn()
		if err == nil {
			return nil
		}
		
		lastErr = err
		
		// Check if error is retryable
		if !isRetryableError(err, config.RetryableErrors) {
			return err
		}
		
		// Don't retry on last attempt
		if attempt == config.MaxAttempts {
			break
		}
		
		// Wait before next retry
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
		
		// Calculate next delay with exponential backoff
		delay = time.Duration(float64(delay) * config.Multiplier)
		if delay > config.MaxDelay {
			delay = config.MaxDelay
		}
	}
	
	// Wrap the last error with retry information
	return WrapChainError(
		lastErr,
		ErrCodeInternal,
		"",
		"maximum retry attempts exceeded",
	).WithContext("attempts", config.MaxAttempts)
}

// Retry retries a function with default configuration
func Retry(ctx context.Context, fn RetryFunc) error {
	return RetryWithConfig(ctx, fn, DefaultRetryConfig())
}

// RetryWithBackoff retries with exponential backoff
func RetryWithBackoff(ctx context.Context, fn RetryFunc, maxAttempts int) error {
	config := DefaultRetryConfig()
	config.MaxAttempts = maxAttempts
	return RetryWithConfig(ctx, fn, config)
}

// isRetryableError checks if an error is retryable based on configuration
func isRetryableError(err error, retryableCodes []ErrorCode) bool {
	// First check if it's a ChainError
	var chainErr *ChainError
	if As(err, &chainErr) {
		// Check if the error code is in the retryable list
		for _, code := range retryableCodes {
			if chainErr.Code == code {
				return true
			}
		}
		// Also check the built-in IsRetryable method
		return chainErr.IsRetryable()
	}
	
	// Fallback to generic retryable check
	return IsRetryable(err)
}

// ExponentialBackoff calculates exponential backoff delay
func ExponentialBackoff(attempt int, baseDelay time.Duration, maxDelay time.Duration) time.Duration {
	if attempt <= 0 {
		return baseDelay
	}
	
	delay := baseDelay * time.Duration(math.Pow(2, float64(attempt-1)))
	if delay > maxDelay {
		return maxDelay
	}
	return delay
}

// LinearBackoff calculates linear backoff delay
func LinearBackoff(attempt int, baseDelay time.Duration, maxDelay time.Duration) time.Duration {
	if attempt <= 0 {
		return baseDelay
	}
	
	delay := baseDelay * time.Duration(attempt)
	if delay > maxDelay {
		return maxDelay
	}
	return delay
}

// RetryOperation represents an operation that can be retried
type RetryOperation struct {
	Name        string
	Fn          RetryFunc
	Config      *RetryConfig
	OnRetry     func(attempt int, err error)
	OnSuccess   func()
	OnFailure   func(err error)
}

// Execute runs the retry operation
func (op *RetryOperation) Execute(ctx context.Context) error {
	if op.Config == nil {
		op.Config = DefaultRetryConfig()
	}
	
	var lastErr error
	delay := op.Config.InitialDelay
	
	for attempt := 1; attempt <= op.Config.MaxAttempts; attempt++ {
		// Check context
		select {
		case <-ctx.Done():
			if op.OnFailure != nil {
				op.OnFailure(ctx.Err())
			}
			return ctx.Err()
		default:
		}
		
		// Try the function
		err := op.Fn()
		if err == nil {
			if op.OnSuccess != nil {
				op.OnSuccess()
			}
			return nil
		}
		
		lastErr = err
		
		// Call retry callback
		if op.OnRetry != nil && attempt < op.Config.MaxAttempts {
			op.OnRetry(attempt, err)
		}
		
		// Check if error is retryable
		if !isRetryableError(err, op.Config.RetryableErrors) {
			if op.OnFailure != nil {
				op.OnFailure(err)
			}
			return err
		}
		
		// Don't retry on last attempt
		if attempt == op.Config.MaxAttempts {
			break
		}
		
		// Wait before next retry
		select {
		case <-ctx.Done():
			if op.OnFailure != nil {
				op.OnFailure(ctx.Err())
			}
			return ctx.Err()
		case <-time.After(delay):
		}
		
		// Calculate next delay
		delay = time.Duration(float64(delay) * op.Config.Multiplier)
		if delay > op.Config.MaxDelay {
			delay = op.Config.MaxDelay
		}
	}
	
	// Call failure callback
	if op.OnFailure != nil {
		op.OnFailure(lastErr)
	}
	
	// Wrap the last error with retry information
	return WrapChainError(
		lastErr,
		ErrCodeInternal,
		"",
		"operation '"+op.Name+"' failed after retries",
	).WithContext("attempts", op.Config.MaxAttempts)
}