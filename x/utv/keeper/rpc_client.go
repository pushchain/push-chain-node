package keeper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// rpcClient is a reusable client for making RPC calls with proper configuration
type rpcClient struct {
	client *http.Client
}

// newRPCClient creates a new production-grade RPC client with proper timeout, connection pooling
func newRPCClient() *rpcClient {
	// Check if we have a mock client first
	if globalRPCClient != nil {
		if isMock, ok := globalRPCClient.(*MockRPCClient); ok && isMock != nil {
			return &rpcClient{
				client: &http.Client{}, // Dummy client, won't be used
			}
		}
	}

	// Otherwise create a real client
	transport := &http.Transport{
		MaxIdleConns:        100,              // Increased for better connection reuse
		MaxIdleConnsPerHost: 10,               // Increased for better connection reuse to same host
		MaxConnsPerHost:     20,               // Limit max connections per host
		IdleConnTimeout:     90 * time.Second, // Keep connections alive longer
		TLSHandshakeTimeout: 10 * time.Second, // Increased for more reliable TLS
		DisableCompression:  false,            // Enable compression for better performance
	}

	client := &http.Client{
		Timeout:   10 * time.Second, // Increased global timeout
		Transport: transport,
	}

	return &rpcClient{
		client: client,
	}
}

// callRPC makes an RPC call with retries and proper error handling
func (r *rpcClient) callRPC(ctx context.Context, url string, method string, params interface{}) ([]byte, error) {
	// Construct the JSON-RPC request
	rpcRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
		"id":      1,
	}

	// Marshal the request to JSON
	requestBody, err := json.Marshal(rpcRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal JSON-RPC request: %w", err)
	}

	// Retry logic
	maxRetries := 3
	backoff := 500 * time.Millisecond
	var responseBytes []byte

	for attempt := 0; attempt < maxRetries; attempt++ {
		// If not first attempt, apply backoff
		if attempt > 0 {
			// Check if context is still valid before sleeping
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
				// Exponential backoff
				backoff *= 2
			}
		}

		// Create HTTP request with context
		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(requestBody))
		if err != nil {
			return nil, fmt.Errorf("failed to create HTTP request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		// Make the HTTP request
		resp, err := r.client.Do(req)
		if err != nil {
			// Log error and continue to retry
			continue
		}

		// Ensure body is closed even on panic
		defer resp.Body.Close()

		// Check HTTP status code
		if resp.StatusCode != http.StatusOK {
			// Read error body for better diagnosis
			errBody, _ := io.ReadAll(resp.Body)
			// If status is 429 or 5xx, retry
			if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
				continue
			}
			return nil, fmt.Errorf("HTTP error: %d - %s", resp.StatusCode, string(errBody))
		}

		// Read the response body with size limit to prevent memory issues
		responseBytes, err = io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024)) // 10MB limit
		if err != nil {
			return nil, fmt.Errorf("failed to read response body: %w", err)
		}

		// Check for JSON-RPC error in the response
		var errorResponse struct {
			Error *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}

		if err := json.Unmarshal(responseBytes, &errorResponse); err == nil && errorResponse.Error != nil {
			// Some RPC errors are worth retrying (like rate limits)
			if errorResponse.Error.Code == -32005 || // Rate limit
				errorResponse.Error.Code == -32000 || // Execution error
				errorResponse.Error.Code == -32002 { // Resource not available
				continue
			}
			return nil, fmt.Errorf("RPC error: %s (code: %d)", errorResponse.Error.Message, errorResponse.Error.Code)
		}

		// Success, return the response
		return responseBytes, nil
	}

	// If we got here, all retries failed
	if responseBytes != nil {
		return responseBytes, nil // Return partial success if we have any data
	}
	return nil, fmt.Errorf("failed to get response after %d attempts", maxRetries)
}

// MockRPCClient is a mock implementation of the rpcClient for testing
type MockRPCClient struct {
	CallRPCFunc func(ctx context.Context, rpcURL, method string, params interface{}) ([]byte, error)
}

// callRPC implements the mocked RPC call interface
func (m *MockRPCClient) callRPC(ctx context.Context, rpcURL, method string, params interface{}) ([]byte, error) {
	if m.CallRPCFunc != nil {
		return m.CallRPCFunc(ctx, rpcURL, method, params)
	}
	return nil, fmt.Errorf("mock RPC client not implemented")
}

// Global instance of the RPC client for easier mocking in tests
var globalRPCClient interface {
	callRPC(ctx context.Context, rpcURL, method string, params interface{}) ([]byte, error)
}

// GetRPCClient returns the current RPC client (used for testing)
func GetRPCClient() interface{} {
	return globalRPCClient
}

// SetRPCClient sets a new RPC client (used for testing)
func SetRPCClient(client interface{}) {
	if client, ok := client.(interface {
		callRPC(ctx context.Context, rpcURL, method string, params interface{}) ([]byte, error)
	}); ok {
		globalRPCClient = client
	}
}

// Initialize the global RPC client
func init() {
	// Create and set the default RPC client if it hasn't been set
	if globalRPCClient == nil {
		globalRPCClient = newRPCClient()
	}
}
