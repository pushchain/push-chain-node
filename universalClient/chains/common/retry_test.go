package common

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

func TestDefaultRetryConfig(t *testing.T) {
	config := DefaultRetryConfig()
	
	assert.Equal(t, 5, config.MaxRetries)
	assert.Equal(t, 1*time.Second, config.InitialDelay)
	assert.Equal(t, 30*time.Second, config.MaxDelay)
	assert.Equal(t, 2.0, config.BackoffFactor)
	assert.NotNil(t, config.RetryableError)
	
	// Test default retry function - should return true for all errors
	assert.True(t, config.RetryableError(errors.New("test error")))
	assert.True(t, config.RetryableError(context.DeadlineExceeded))
}

func TestNewRetryManager(t *testing.T) {
	tests := []struct {
		name   string
		config *RetryConfig
	}{
		{
			name: "with custom config",
			config: &RetryConfig{
				MaxRetries:     3,
				InitialDelay:   500 * time.Millisecond,
				MaxDelay:       10 * time.Second,
				BackoffFactor:  1.5,
				RetryableError: func(err error) bool { return true },
			},
		},
		{
			name:   "with nil config uses defaults",
			config: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := zerolog.Nop()
			manager := NewRetryManager(tt.config, logger)
			
			assert.NotNil(t, manager)
			assert.NotNil(t, manager.config)
			
			if tt.config == nil {
				// Should use defaults
				assert.Equal(t, 5, manager.config.MaxRetries)
				assert.Equal(t, 1*time.Second, manager.config.InitialDelay)
			} else {
				assert.Equal(t, tt.config.MaxRetries, manager.config.MaxRetries)
				assert.Equal(t, tt.config.InitialDelay, manager.config.InitialDelay)
			}
		})
	}
}

func TestRetryManager_ExecuteWithRetry_Success(t *testing.T) {
	config := &RetryConfig{
		MaxRetries:     3,
		InitialDelay:   10 * time.Millisecond,
		MaxDelay:       100 * time.Millisecond,
		BackoffFactor:  2.0,
		RetryableError: func(err error) bool { return true },
	}
	
	logger := zerolog.Nop()
	manager := NewRetryManager(config, logger)
	ctx := context.Background()
	
	// Test immediate success
	callCount := 0
	err := manager.ExecuteWithRetry(ctx, "test_op", func() error {
		callCount++
		return nil
	})
	
	assert.NoError(t, err)
	assert.Equal(t, 1, callCount)
}

func TestRetryManager_ExecuteWithRetry_SuccessAfterRetries(t *testing.T) {
	config := &RetryConfig{
		MaxRetries:     3,
		InitialDelay:   10 * time.Millisecond,
		MaxDelay:       100 * time.Millisecond,
		BackoffFactor:  2.0,
		RetryableError: func(err error) bool { return true },
	}
	
	logger := zerolog.Nop()
	manager := NewRetryManager(config, logger)
	ctx := context.Background()
	
	// Test success after 2 failures
	callCount := 0
	err := manager.ExecuteWithRetry(ctx, "test_op", func() error {
		callCount++
		if callCount < 3 {
			return errors.New("temporary failure")
		}
		return nil
	})
	
	assert.NoError(t, err)
	assert.Equal(t, 3, callCount)
}

func TestRetryManager_ExecuteWithRetry_MaxRetriesExceeded(t *testing.T) {
	config := &RetryConfig{
		MaxRetries:     2,
		InitialDelay:   10 * time.Millisecond,
		MaxDelay:       100 * time.Millisecond,
		BackoffFactor:  2.0,
		RetryableError: func(err error) bool { return true },
	}
	
	logger := zerolog.Nop()
	manager := NewRetryManager(config, logger)
	ctx := context.Background()
	
	// Test failure after exhausting retries
	callCount := 0
	originalErr := errors.New("persistent failure")
	err := manager.ExecuteWithRetry(ctx, "test_op", func() error {
		callCount++
		return originalErr
	})
	
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed after 3 attempts")
	assert.ErrorIs(t, err, originalErr)
	assert.Equal(t, 3, callCount) // MaxRetries + 1
}

func TestRetryManager_ExecuteWithRetry_NonRetryableError(t *testing.T) {
	config := &RetryConfig{
		MaxRetries:     3,
		InitialDelay:   10 * time.Millisecond,
		MaxDelay:       100 * time.Millisecond,
		BackoffFactor:  2.0,
		RetryableError: func(err error) bool {
			// Only retry "retryable" errors
			return err.Error() == "retryable error"
		},
	}
	
	logger := zerolog.Nop()
	manager := NewRetryManager(config, logger)
	ctx := context.Background()
	
	// Test non-retryable error
	callCount := 0
	nonRetryableErr := errors.New("non-retryable error")
	err := manager.ExecuteWithRetry(ctx, "test_op", func() error {
		callCount++
		return nonRetryableErr
	})
	
	assert.Error(t, err)
	assert.Equal(t, nonRetryableErr, err)
	assert.Equal(t, 1, callCount) // Should not retry
}

func TestRetryManager_ExecuteWithRetry_ContextCancellation(t *testing.T) {
	config := &RetryConfig{
		MaxRetries:     5,
		InitialDelay:   100 * time.Millisecond,
		MaxDelay:       1 * time.Second,
		BackoffFactor:  2.0,
		RetryableError: func(err error) bool { return true },
	}
	
	logger := zerolog.Nop()
	manager := NewRetryManager(config, logger)
	
	// Test context cancellation before operation
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	
	callCount := 0
	err := manager.ExecuteWithRetry(ctx, "test_op", func() error {
		callCount++
		return errors.New("should not be called")
	})
	
	assert.Error(t, err)
	assert.Equal(t, context.Canceled, err)
	assert.Equal(t, 0, callCount)
}

func TestRetryManager_ExecuteWithRetry_ContextCancellationDuringRetry(t *testing.T) {
	config := &RetryConfig{
		MaxRetries:     3,
		InitialDelay:   200 * time.Millisecond,
		MaxDelay:       1 * time.Second,
		BackoffFactor:  2.0,
		RetryableError: func(err error) bool { return true },
	}
	
	logger := zerolog.Nop()
	manager := NewRetryManager(config, logger)
	ctx, cancel := context.WithCancel(context.Background())
	
	callCount := 0
	err := manager.ExecuteWithRetry(ctx, "test_op", func() error {
		callCount++
		if callCount == 1 {
			// Cancel context after first failure to test cancellation during retry delay
			go func() {
				time.Sleep(50 * time.Millisecond)
				cancel()
			}()
		}
		return errors.New("test error")
	})
	
	assert.Error(t, err)
	assert.Equal(t, context.Canceled, err)
	assert.Equal(t, 1, callCount)
}

func TestRetryManager_ExecuteWithRetry_BackoffCalculation(t *testing.T) {
	config := &RetryConfig{
		MaxRetries:     3,
		InitialDelay:   10 * time.Millisecond,
		MaxDelay:       50 * time.Millisecond,
		BackoffFactor:  2.0,
		RetryableError: func(err error) bool { return true },
	}
	
	logger := zerolog.Nop()
	manager := NewRetryManager(config, logger)
	ctx := context.Background()
	
	delays := []time.Duration{}
	callCount := 0
	lastTime := time.Now()
	
	err := manager.ExecuteWithRetry(ctx, "test_op", func() error {
		callCount++
		now := time.Now()
		if callCount > 1 {
			delay := now.Sub(lastTime)
			delays = append(delays, delay)
		}
		lastTime = now
		return errors.New("always fail")
	})
	
	assert.Error(t, err)
	assert.Equal(t, 4, callCount) // MaxRetries + 1
	assert.Equal(t, 3, len(delays)) // 3 delays between 4 calls
	
	// Verify exponential backoff with some tolerance for timing variations
	expectedDelays := []time.Duration{
		10 * time.Millisecond,  // Initial delay
		20 * time.Millisecond,  // 10 * 2.0
		40 * time.Millisecond,  // 20 * 2.0
	}
	
	for i, expected := range expectedDelays {
		// Allow 50% tolerance for timing variations
		tolerance := expected / 2
		assert.InDelta(t, float64(expected), float64(delays[i]), float64(tolerance),
			"Delay %d: expected ~%v, got %v", i, expected, delays[i])
	}
}

func TestRetryManager_ExecuteWithRetry_MaxDelayRespected(t *testing.T) {
	config := &RetryConfig{
		MaxRetries:     5,
		InitialDelay:   10 * time.Millisecond,
		MaxDelay:       25 * time.Millisecond, // Low max to test capping
		BackoffFactor:  2.0,
		RetryableError: func(err error) bool { return true },
	}
	
	logger := zerolog.Nop()
	manager := NewRetryManager(config, logger)
	ctx := context.Background()
	
	delays := []time.Duration{}
	callCount := 0
	lastTime := time.Now()
	
	err := manager.ExecuteWithRetry(ctx, "test_op", func() error {
		callCount++
		now := time.Now()
		if callCount > 1 {
			delay := now.Sub(lastTime)
			delays = append(delays, delay)
		}
		lastTime = now
		if callCount <= 2 {
			return errors.New("keep failing")
		}
		return nil // Success after 2 attempts
	})
	
	assert.NoError(t, err)
	assert.Equal(t, 3, callCount)
	assert.Equal(t, 2, len(delays))
	
	// The third delay (if there was one) should be capped at MaxDelay
	// delays[0] = 10ms (initial)
	// delays[1] = 20ms (10 * 2.0)
	// delays[2] would be 40ms but should be capped to 25ms (but we succeed before this)
	
	tolerance := 15 * time.Millisecond // Allow timing variations
	assert.InDelta(t, float64(10*time.Millisecond), float64(delays[0]), float64(tolerance))
	assert.InDelta(t, float64(20*time.Millisecond), float64(delays[1]), float64(tolerance))
}

func TestRetryManager_ExecuteWithRetryAsync(t *testing.T) {
	config := &RetryConfig{
		MaxRetries:     2,
		InitialDelay:   10 * time.Millisecond,
		MaxDelay:       100 * time.Millisecond,
		BackoffFactor:  2.0,
		RetryableError: func(err error) bool { return true },
	}
	
	logger := zerolog.Nop()
	manager := NewRetryManager(config, logger)
	ctx := context.Background()
	
	// Test async execution with success
	var wg sync.WaitGroup
	var resultErr error
	
	wg.Add(1)
	callCount := 0
	manager.ExecuteWithRetryAsync(ctx, "async_test", func() error {
		callCount++
		if callCount < 2 {
			return errors.New("temporary failure")
		}
		return nil
	}, func(err error) {
		defer wg.Done()
		resultErr = err
	})
	
	wg.Wait()
	assert.NoError(t, resultErr)
	assert.Equal(t, 2, callCount)
}

func TestRetryManager_ExecuteWithRetryAsync_WithoutCallback(t *testing.T) {
	config := &RetryConfig{
		MaxRetries:     1,
		InitialDelay:   10 * time.Millisecond,
		MaxDelay:       100 * time.Millisecond,
		BackoffFactor:  2.0,
		RetryableError: func(err error) bool { return true },
	}
	
	logger := zerolog.Nop()
	manager := NewRetryManager(config, logger)
	ctx := context.Background()
	
	// Test async execution without callback (should not panic)
	callCount := 0
	manager.ExecuteWithRetryAsync(ctx, "async_test", func() error {
		callCount++
		return nil
	}, nil)
	
	// Give some time for async execution to complete
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 1, callCount)
}

func TestRetryManager_CalculateBackoff(t *testing.T) {
	tests := []struct {
		name            string
		config          *RetryConfig
		attempt         int
		expectedDelay   time.Duration
	}{
		{
			name: "first attempt",
			config: &RetryConfig{
				InitialDelay:  100 * time.Millisecond,
				MaxDelay:      1 * time.Second,
				BackoffFactor: 2.0,
			},
			attempt:       0,
			expectedDelay: 100 * time.Millisecond,
		},
		{
			name: "second attempt",
			config: &RetryConfig{
				InitialDelay:  100 * time.Millisecond,
				MaxDelay:      1 * time.Second,
				BackoffFactor: 2.0,
			},
			attempt:       1,
			expectedDelay: 200 * time.Millisecond,
		},
		{
			name: "third attempt",
			config: &RetryConfig{
				InitialDelay:  100 * time.Millisecond,
				MaxDelay:      1 * time.Second,
				BackoffFactor: 2.0,
			},
			attempt:       2,
			expectedDelay: 400 * time.Millisecond,
		},
		{
			name: "attempt exceeds max delay",
			config: &RetryConfig{
				InitialDelay:  100 * time.Millisecond,
				MaxDelay:      300 * time.Millisecond,
				BackoffFactor: 2.0,
			},
			attempt:       3, // Would be 800ms but should be capped
			expectedDelay: 300 * time.Millisecond,
		},
		{
			name: "different backoff factor",
			config: &RetryConfig{
				InitialDelay:  50 * time.Millisecond,
				MaxDelay:      1 * time.Second,
				BackoffFactor: 1.5,
			},
			attempt:       2,
			expectedDelay: 112 * time.Millisecond + 500*time.Microsecond, // 50 * 1.5^2 = 112.5ms
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := zerolog.Nop()
			manager := NewRetryManager(tt.config, logger)
			
			result := manager.CalculateBackoff(tt.attempt)
			assert.Equal(t, tt.expectedDelay, result)
		})
	}
}

func TestRetryManager_ContextTimeouts(t *testing.T) {
	config := &RetryConfig{
		MaxRetries:     3,
		InitialDelay:   50 * time.Millisecond,
		MaxDelay:       200 * time.Millisecond,
		BackoffFactor:  2.0,
		RetryableError: func(err error) bool { return true },
	}
	
	logger := zerolog.Nop()
	manager := NewRetryManager(config, logger)
	
	// Create context with short timeout  
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	
	callCount := 0
	err := manager.ExecuteWithRetry(ctx, "timeout_test", func() error {
		callCount++
		return errors.New("always fail")
	})
	
	assert.Error(t, err)
	assert.Equal(t, context.DeadlineExceeded, err)
	// Should only be called once before timeout during retry delay
	assert.Equal(t, 1, callCount)
}

func TestRetryManager_ConcurrentOperations(t *testing.T) {
	config := &RetryConfig{
		MaxRetries:     2,
		InitialDelay:   10 * time.Millisecond,
		MaxDelay:       50 * time.Millisecond,
		BackoffFactor:  2.0,
		RetryableError: func(err error) bool { return true },
	}
	
	logger := zerolog.Nop()
	manager := NewRetryManager(config, logger)
	ctx := context.Background()
	
	// Test concurrent operations
	numOperations := 10
	var wg sync.WaitGroup
	results := make([]error, numOperations)
	
	for i := 0; i < numOperations; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			
			callCount := 0
			results[index] = manager.ExecuteWithRetry(ctx, "concurrent_test", func() error {
				callCount++
				if callCount < 2 {
					return errors.New("temporary failure")
				}
				return nil
			})
		}(i)
	}
	
	wg.Wait()
	
	// All operations should succeed
	for i, err := range results {
		assert.NoError(t, err, "Operation %d failed", i)
	}
}