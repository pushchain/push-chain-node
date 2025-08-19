package common

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/rs/zerolog"
)

// RetryConfig holds retry configuration
type RetryConfig struct {
	MaxRetries     int           // Maximum number of retry attempts
	InitialDelay   time.Duration // Initial delay between retries
	MaxDelay       time.Duration // Maximum delay between retries
	BackoffFactor  float64       // Exponential backoff factor (e.g., 2.0)
	RetryableError func(error) bool // Function to determine if error is retryable
}

// DefaultRetryConfig returns default retry configuration
func DefaultRetryConfig() *RetryConfig {
	return &RetryConfig{
		MaxRetries:    5,
		InitialDelay:  1 * time.Second,
		MaxDelay:      30 * time.Second,
		BackoffFactor: 2.0,
		RetryableError: func(err error) bool {
			// By default, retry all errors
			return true
		},
	}
}

// RetryManager handles retry logic with exponential backoff
type RetryManager struct {
	config *RetryConfig
	logger zerolog.Logger
}

// NewRetryManager creates a new retry manager
func NewRetryManager(config *RetryConfig, logger zerolog.Logger) *RetryManager {
	if config == nil {
		config = DefaultRetryConfig()
	}
	return &RetryManager{
		config: config,
		logger: logger.With().Str("component", "retry_manager").Logger(),
	}
}

// ExecuteWithRetry executes a function with retry logic
func (r *RetryManager) ExecuteWithRetry(
	ctx context.Context,
	operation string,
	fn func() error,
) error {
	var lastErr error
	delay := r.config.InitialDelay

	for attempt := 0; attempt <= r.config.MaxRetries; attempt++ {
		// Check context before attempting
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Try the operation
		err := fn()
		if err == nil {
			if attempt > 0 {
				r.logger.Info().
					Str("operation", operation).
					Int("attempts", attempt + 1).
					Msg("operation succeeded after retries")
			}
			return nil
		}

		lastErr = err

		// Check if error is retryable
		if !r.config.RetryableError(err) {
			r.logger.Error().
				Err(err).
				Str("operation", operation).
				Msg("non-retryable error encountered")
			return err
		}

		// Don't retry if we've exhausted attempts
		if attempt >= r.config.MaxRetries {
			break
		}

		// Log retry attempt
		r.logger.Warn().
			Err(err).
			Str("operation", operation).
			Int("attempt", attempt + 1).
			Int("max_attempts", r.config.MaxRetries + 1).
			Dur("retry_in", delay).
			Msg("operation failed, retrying")

		// Wait before retrying
		select {
		case <-time.After(delay):
			// Calculate next delay with exponential backoff
			delay = time.Duration(float64(delay) * r.config.BackoffFactor)
			if delay > r.config.MaxDelay {
				delay = r.config.MaxDelay
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	r.logger.Error().
		Err(lastErr).
		Str("operation", operation).
		Int("attempts", r.config.MaxRetries + 1).
		Msg("operation failed after all retries")

	return fmt.Errorf("operation %s failed after %d attempts: %w", 
		operation, r.config.MaxRetries + 1, lastErr)
}

// ExecuteWithRetryAsync executes a function with retry logic asynchronously
func (r *RetryManager) ExecuteWithRetryAsync(
	ctx context.Context,
	operation string,
	fn func() error,
	callback func(error),
) {
	go func() {
		err := r.ExecuteWithRetry(ctx, operation, fn)
		if callback != nil {
			callback(err)
		}
	}()
}

// CalculateBackoff calculates the next backoff delay
func (r *RetryManager) CalculateBackoff(attempt int) time.Duration {
	delay := float64(r.config.InitialDelay) * math.Pow(r.config.BackoffFactor, float64(attempt))
	if delay > float64(r.config.MaxDelay) {
		return r.config.MaxDelay
	}
	return time.Duration(delay)
}