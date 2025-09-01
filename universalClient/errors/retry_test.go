package errors

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultRetryConfig(t *testing.T) {
	config := DefaultRetryConfig()
	
	assert.Equal(t, 3, config.MaxAttempts)
	assert.Equal(t, 1*time.Second, config.InitialDelay)
	assert.Equal(t, 30*time.Second, config.MaxDelay)
	assert.Equal(t, 2.0, config.Multiplier)
	assert.Contains(t, config.RetryableErrors, ErrCodeNetwork)
	assert.Contains(t, config.RetryableErrors, ErrCodeRPC)
	assert.Contains(t, config.RetryableErrors, ErrCodeTimeout)
}

func TestRetryWithConfig_Success(t *testing.T) {
	tests := []struct {
		name            string
		attemptsToSucceed int
		config          *RetryConfig
	}{
		{
			name:              "succeeds on first attempt",
			attemptsToSucceed: 1,
			config: &RetryConfig{
				MaxAttempts:     3,
				InitialDelay:    1 * time.Millisecond,
				MaxDelay:        10 * time.Millisecond,
				Multiplier:      2.0,
				RetryableErrors: []ErrorCode{ErrCodeNetwork},
			},
		},
		{
			name:              "succeeds on second attempt",
			attemptsToSucceed: 2,
			config: &RetryConfig{
				MaxAttempts:     3,
				InitialDelay:    1 * time.Millisecond,
				MaxDelay:        10 * time.Millisecond,
				Multiplier:      2.0,
				RetryableErrors: []ErrorCode{ErrCodeNetwork},
			},
		},
		{
			name:              "succeeds on last attempt",
			attemptsToSucceed: 3,
			config: &RetryConfig{
				MaxAttempts:     3,
				InitialDelay:    1 * time.Millisecond,
				MaxDelay:        10 * time.Millisecond,
				Multiplier:      2.0,
				RetryableErrors: []ErrorCode{ErrCodeNetwork},
			},
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attempts := 0
			fn := func() error {
				attempts++
				if attempts < tt.attemptsToSucceed {
					return NewChainError(ErrCodeNetwork, "test", "network error", nil)
				}
				return nil
			}
			
			ctx := context.Background()
			err := RetryWithConfig(ctx, fn, tt.config)
			
			assert.NoError(t, err)
			assert.Equal(t, tt.attemptsToSucceed, attempts)
		})
	}
}

func TestRetryWithConfig_NonRetryableError(t *testing.T) {
	config := &RetryConfig{
		MaxAttempts:     3,
		InitialDelay:    1 * time.Millisecond,
		MaxDelay:        10 * time.Millisecond,
		Multiplier:      2.0,
		RetryableErrors: []ErrorCode{ErrCodeNetwork},
	}
	
	attempts := 0
	fn := func() error {
		attempts++
		// Return a non-retryable error
		return NewChainError(ErrCodeValidation, "test", "validation error", nil)
	}
	
	ctx := context.Background()
	err := RetryWithConfig(ctx, fn, config)
	
	assert.Error(t, err)
	assert.Equal(t, 1, attempts) // Should not retry
	assert.True(t, IsChainError(err, ErrCodeValidation))
}

func TestRetryWithConfig_MaxAttemptsExceeded(t *testing.T) {
	config := &RetryConfig{
		MaxAttempts:     3,
		InitialDelay:    1 * time.Millisecond,
		MaxDelay:        10 * time.Millisecond,
		Multiplier:      2.0,
		RetryableErrors: []ErrorCode{ErrCodeNetwork},
	}
	
	attempts := 0
	fn := func() error {
		attempts++
		return NewChainError(ErrCodeNetwork, "test", "network error", nil)
	}
	
	ctx := context.Background()
	err := RetryWithConfig(ctx, fn, config)
	
	assert.Error(t, err)
	assert.Equal(t, 3, attempts)
	
	// Check that the error has retry information in context
	var chainErr *ChainError
	assert.True(t, As(err, &chainErr))
	// The original error code is preserved when wrapping
	assert.Equal(t, ErrCodeNetwork, chainErr.Code)
	// The wrapped message is added to context
	assert.Contains(t, chainErr.Context["wrapped_message"], "maximum retry attempts exceeded")
	assert.Equal(t, 3, chainErr.Context["attempts"])
}

func TestRetryWithConfig_ContextCancellation(t *testing.T) {
	config := &RetryConfig{
		MaxAttempts:     5,
		InitialDelay:    100 * time.Millisecond,
		MaxDelay:        1 * time.Second,
		Multiplier:      2.0,
		RetryableErrors: []ErrorCode{ErrCodeNetwork},
	}
	
	attempts := 0
	fn := func() error {
		attempts++
		return NewChainError(ErrCodeNetwork, "test", "network error", nil)
	}
	
	ctx, cancel := context.WithCancel(context.Background())
	
	// Cancel context after a short delay
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	
	err := RetryWithConfig(ctx, fn, config)
	
	assert.Error(t, err)
	assert.Equal(t, context.Canceled, err)
	assert.Less(t, attempts, 5) // Should not reach max attempts
}

func TestRetryWithConfig_NilConfig(t *testing.T) {
	attempts := 0
	fn := func() error {
		attempts++
		if attempts < 2 {
			return NewChainError(ErrCodeNetwork, "test", "network error", nil)
		}
		return nil
	}
	
	ctx := context.Background()
	err := RetryWithConfig(ctx, fn, nil) // nil config should use default
	
	assert.NoError(t, err)
	assert.Equal(t, 2, attempts)
}

func TestRetry_DefaultBehavior(t *testing.T) {
	attempts := 0
	fn := func() error {
		attempts++
		if attempts < 2 {
			return NewChainError(ErrCodeNetwork, "test", "network error", nil)
		}
		return nil
	}
	
	ctx := context.Background()
	err := Retry(ctx, fn)
	
	assert.NoError(t, err)
	assert.Equal(t, 2, attempts)
}

func TestRetryWithBackoff(t *testing.T) {
	attempts := 0
	fn := func() error {
		attempts++
		if attempts < 3 {
			return NewChainError(ErrCodeTimeout, "test", "timeout error", nil)
		}
		return nil
	}
	
	ctx := context.Background()
	err := RetryWithBackoff(ctx, fn, 5)
	
	assert.NoError(t, err)
	assert.Equal(t, 3, attempts)
}

func TestExponentialBackoff(t *testing.T) {
	tests := []struct {
		name      string
		attempt   int
		baseDelay time.Duration
		maxDelay  time.Duration
		expected  time.Duration
	}{
		{
			name:      "attempt 0 returns base delay",
			attempt:   0,
			baseDelay: 100 * time.Millisecond,
			maxDelay:  10 * time.Second,
			expected:  100 * time.Millisecond,
		},
		{
			name:      "attempt 1 returns base delay",
			attempt:   1,
			baseDelay: 100 * time.Millisecond,
			maxDelay:  10 * time.Second,
			expected:  100 * time.Millisecond,
		},
		{
			name:      "attempt 2 returns 2x base delay",
			attempt:   2,
			baseDelay: 100 * time.Millisecond,
			maxDelay:  10 * time.Second,
			expected:  200 * time.Millisecond,
		},
		{
			name:      "attempt 3 returns 4x base delay",
			attempt:   3,
			baseDelay: 100 * time.Millisecond,
			maxDelay:  10 * time.Second,
			expected:  400 * time.Millisecond,
		},
		{
			name:      "caps at max delay",
			attempt:   10,
			baseDelay: 100 * time.Millisecond,
			maxDelay:  500 * time.Millisecond,
			expected:  500 * time.Millisecond,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			delay := ExponentialBackoff(tt.attempt, tt.baseDelay, tt.maxDelay)
			assert.Equal(t, tt.expected, delay)
		})
	}
}

func TestLinearBackoff(t *testing.T) {
	tests := []struct {
		name      string
		attempt   int
		baseDelay time.Duration
		maxDelay  time.Duration
		expected  time.Duration
	}{
		{
			name:      "attempt 0 returns base delay",
			attempt:   0,
			baseDelay: 100 * time.Millisecond,
			maxDelay:  10 * time.Second,
			expected:  100 * time.Millisecond,
		},
		{
			name:      "attempt 1 returns base delay",
			attempt:   1,
			baseDelay: 100 * time.Millisecond,
			maxDelay:  10 * time.Second,
			expected:  100 * time.Millisecond,
		},
		{
			name:      "attempt 2 returns 2x base delay",
			attempt:   2,
			baseDelay: 100 * time.Millisecond,
			maxDelay:  10 * time.Second,
			expected:  200 * time.Millisecond,
		},
		{
			name:      "attempt 3 returns 3x base delay",
			attempt:   3,
			baseDelay: 100 * time.Millisecond,
			maxDelay:  10 * time.Second,
			expected:  300 * time.Millisecond,
		},
		{
			name:      "caps at max delay",
			attempt:   10,
			baseDelay: 100 * time.Millisecond,
			maxDelay:  500 * time.Millisecond,
			expected:  500 * time.Millisecond,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			delay := LinearBackoff(tt.attempt, tt.baseDelay, tt.maxDelay)
			assert.Equal(t, tt.expected, delay)
		})
	}
}

func TestRetryOperation_Execute(t *testing.T) {
	t.Run("successful operation", func(t *testing.T) {
		var attempts int32
		var successCalled bool
		
		op := &RetryOperation{
			Name: "test_operation",
			Fn: func() error {
				atomic.AddInt32(&attempts, 1)
				if atomic.LoadInt32(&attempts) < 2 {
					return NewChainError(ErrCodeNetwork, "test", "network error", nil)
				}
				return nil
			},
			Config: &RetryConfig{
				MaxAttempts:     3,
				InitialDelay:    1 * time.Millisecond,
				MaxDelay:        10 * time.Millisecond,
				Multiplier:      2.0,
				RetryableErrors: []ErrorCode{ErrCodeNetwork},
			},
			OnRetry: func(attempt int, err error) {
				t.Logf("Retry attempt %d: %v", attempt, err)
			},
			OnSuccess: func() {
				successCalled = true
			},
			OnFailure: func(err error) {
				t.Errorf("Unexpected failure: %v", err)
			},
		}
		
		ctx := context.Background()
		err := op.Execute(ctx)
		
		assert.NoError(t, err)
		assert.Equal(t, int32(2), atomic.LoadInt32(&attempts))
		assert.True(t, successCalled)
	})
	
	t.Run("failed operation", func(t *testing.T) {
		var attempts int32
		var failureCalled bool
		var retryCount int
		
		op := &RetryOperation{
			Name: "test_operation",
			Fn: func() error {
				atomic.AddInt32(&attempts, 1)
				return NewChainError(ErrCodeNetwork, "test", "persistent network error", nil)
			},
			Config: &RetryConfig{
				MaxAttempts:     3,
				InitialDelay:    1 * time.Millisecond,
				MaxDelay:        10 * time.Millisecond,
				Multiplier:      2.0,
				RetryableErrors: []ErrorCode{ErrCodeNetwork},
			},
			OnRetry: func(attempt int, err error) {
				retryCount++
			},
			OnSuccess: func() {
				t.Error("Success should not be called")
			},
			OnFailure: func(err error) {
				failureCalled = true
			},
		}
		
		ctx := context.Background()
		err := op.Execute(ctx)
		
		assert.Error(t, err)
		assert.Equal(t, int32(3), atomic.LoadInt32(&attempts))
		assert.True(t, failureCalled)
		assert.Equal(t, 2, retryCount) // 2 retries after initial attempt
	})
}

func TestIsRetryableError(t *testing.T) {
	tests := []struct {
		name           string
		err            error
		retryableCodes []ErrorCode
		expected       bool
	}{
		{
			name:           "nil error is not retryable",
			err:            nil,
			retryableCodes: []ErrorCode{ErrCodeNetwork},
			expected:       false,
		},
		{
			name:           "network error is retryable",
			err:            NewChainError(ErrCodeNetwork, "test", "network error", nil),
			retryableCodes: []ErrorCode{ErrCodeNetwork, ErrCodeTimeout},
			expected:       true,
		},
		{
			name:           "timeout error is retryable",
			err:            NewChainError(ErrCodeTimeout, "test", "timeout error", nil),
			retryableCodes: []ErrorCode{ErrCodeNetwork, ErrCodeTimeout},
			expected:       true,
		},
		{
			name:           "validation error is not retryable",
			err:            NewChainError(ErrCodeValidation, "test", "validation error", nil),
			retryableCodes: []ErrorCode{ErrCodeNetwork, ErrCodeTimeout},
			expected:       false,
		},
		{
			name:           "database error with non-critical severity is retryable",
			err:            NewChainError(ErrCodeDatabase, "test", "database error", nil),
			retryableCodes: []ErrorCode{},
			expected:       true, // Database errors are retryable if not critical
		},
		{
			name:           "generic error falls back to IsRetryable check",
			err:            errors.New("connection refused"),
			retryableCodes: []ErrorCode{ErrCodeNetwork},
			expected:       true, // Should match retryable pattern
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isRetryableError(tt.err, tt.retryableCodes)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRetryDelayCalculation(t *testing.T) {
	config := &RetryConfig{
		MaxAttempts:     5,
		InitialDelay:    10 * time.Millisecond,
		MaxDelay:        100 * time.Millisecond,
		Multiplier:      2.0,
		RetryableErrors: []ErrorCode{ErrCodeNetwork},
	}
	
	// Track delays between retries
	var delays []time.Duration
	var lastAttemptTime time.Time
	attemptCount := 0
	
	fn := func() error {
		now := time.Now()
		if attemptCount > 0 { // Skip first attempt
			delay := now.Sub(lastAttemptTime)
			delays = append(delays, delay)
		}
		lastAttemptTime = now
		attemptCount++
		return NewChainError(ErrCodeNetwork, "test", "network error", nil)
	}
	
	ctx := context.Background()
	_ = RetryWithConfig(ctx, fn, config)
	
	// Verify we have the right number of delays (attempts - 1)
	require.Len(t, delays, config.MaxAttempts-1)
	
	// Check that delays follow exponential pattern (with some tolerance for timing)
	expectedDelays := []time.Duration{
		10 * time.Millisecond,  // First retry delay
		20 * time.Millisecond,  // Second retry delay (2x)
		40 * time.Millisecond,  // Third retry delay (4x)
		80 * time.Millisecond,  // Fourth retry delay (8x)
	}
	
	for i, expectedDelay := range expectedDelays {
		// Allow 20ms tolerance for timing variations
		assert.InDelta(t, float64(expectedDelay), float64(delays[i]), float64(20*time.Millisecond),
			"Delay %d should be approximately %v but was %v", i+1, expectedDelay, delays[i])
	}
}

// Benchmark tests
func BenchmarkRetryWithConfig(b *testing.B) {
	config := &RetryConfig{
		MaxAttempts:     3,
		InitialDelay:    1 * time.Microsecond,
		MaxDelay:        10 * time.Microsecond,
		Multiplier:      2.0,
		RetryableErrors: []ErrorCode{ErrCodeNetwork},
	}
	
	fn := func() error {
		return nil // Always succeed
	}
	
	ctx := context.Background()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = RetryWithConfig(ctx, fn, config)
	}
}

func BenchmarkExponentialBackoff(b *testing.B) {
	baseDelay := 100 * time.Millisecond
	maxDelay := 10 * time.Second
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ExponentialBackoff(i%10, baseDelay, maxDelay)
	}
}

func BenchmarkLinearBackoff(b *testing.B) {
	baseDelay := 100 * time.Millisecond
	maxDelay := 10 * time.Second
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = LinearBackoff(i%10, baseDelay, maxDelay)
	}
}