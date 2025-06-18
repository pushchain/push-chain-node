package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

type Client struct {
	httpClient *http.Client
}

type RpcCallConfig struct {
	PrivateRPC string
	PublicRPC  string
}

const (
	defaultRetryCount  = 3
	fallbackRetryCount = 1
)

var (
	clientInstance *Client
	once           sync.Once
)

// getClient returns a singleton RPC client instance
func GetClient() *Client {
	once.Do(func() {
		transport := &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			MaxConnsPerHost:     20,
			IdleConnTimeout:     90 * time.Second,
			TLSHandshakeTimeout: 10 * time.Second,
		}
		clientInstance = &Client{
			httpClient: &http.Client{
				Timeout:   10 * time.Second,
				Transport: transport,
			},
		}
	})
	return clientInstance
}

// Call performs a JSON-RPC call with retry logic (3 retries).
func (c *Client) Call(ctx context.Context, url, method string, params interface{}, result interface{}) error {
	return c.callWithRetry(ctx, url, method, params, result, defaultRetryCount)
}

// CallWithFallback tries the private RPC (with retries), then public RPC (once) if private fails.
func (c *Client) CallWithFallback(ctx context.Context, primaryURL, fallbackURL, method string, params interface{}, result interface{}) error {
	// First try private RPC with retries
	err := c.callWithRetry(ctx, primaryURL, method, params, result, defaultRetryCount)
	if err == nil {
		return nil
	}

	// Fallback: Try public RPC only once
	return c.callWithRetry(ctx, fallbackURL, method, params, result, fallbackRetryCount)
}

// callWithRetry performs a JSON-RPC call with retry logic.
func (c *Client) callWithRetry(ctx context.Context, url, method string, params interface{}, result interface{}, maxRetries int) error {
	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
		"id":      1,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	var respBytes []byte
	backoff := 500 * time.Millisecond

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
				backoff *= 2
			}
		}

		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			_, _ = io.ReadAll(resp.Body)
			continue
		}

		respBytes, err = io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
		if err != nil {
			return fmt.Errorf("failed to read response: %w", err)
		}

		var rpcErr struct {
			Error *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(respBytes, &rpcErr); err == nil && rpcErr.Error != nil {
			continue
		}

		if err := json.Unmarshal(respBytes, &struct {
			Result interface{} `json:"result"`
		}{Result: result}); err != nil {
			return fmt.Errorf("failed to unmarshal result: %w", err)
		}
		return nil
	}

	return fmt.Errorf("rpc call failed after retries")
}
