package registry

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"

	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
)

// connectionInfo holds information about a gRPC connection
type connectionInfo struct {
	url         string
	conn        *grpc.ClientConn
	queryClient uregistrytypes.QueryClient
	healthy     bool
	lastCheck   time.Time
}

// RegistryClient handles communication with Push Chain's uregistry module
type RegistryClient struct {
	connections []*connectionInfo
	currentIdx  int
	mu          sync.RWMutex
	logger      zerolog.Logger

	// Retry configuration
	maxRetries   int
	retryBackoff time.Duration
	// Health check configuration
	healthCheckInterval time.Duration
	unhealthyCooldown   time.Duration
}

// NewRegistryClient creates a new registry client with multiple URLs
func NewRegistryClient(grpcURLs []string, logger zerolog.Logger) (*RegistryClient, error) {
	if len(grpcURLs) == 0 {
		return nil, fmt.Errorf("at least one gRPC URL must be provided")
	}

	client := &RegistryClient{
		connections:         make([]*connectionInfo, 0, len(grpcURLs)),
		logger:              logger.With().Str("component", "registry_client").Logger(),
		maxRetries:          3,
		retryBackoff:        time.Second,
		healthCheckInterval: 30 * time.Second,
		unhealthyCooldown:   10 * time.Second,
	}

	// Create connections to all URLs
	for _, url := range grpcURLs {
		conn, err := grpc.Dial(url, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			client.logger.Warn().
				Str("url", url).
				Err(err).
				Msg("failed to create connection, will retry later")
			// Add as unhealthy connection to retry later
			client.connections = append(client.connections, &connectionInfo{
				url:       url,
				conn:      nil,
				healthy:   false,
				lastCheck: time.Now(),
			})
			continue
		}

		client.connections = append(client.connections, &connectionInfo{
			url:         url,
			conn:        conn,
			queryClient: uregistrytypes.NewQueryClient(conn),
			healthy:     true,
			lastCheck:   time.Now(),
		})
	}

	// Allow client to start even without healthy connections
	// They will be recovered through health checks and retry mechanisms
	hasHealthy := false
	for _, conn := range client.connections {
		if conn.healthy {
			hasHealthy = true
			break
		}
	}
	if !hasHealthy {
		client.logger.Warn().
			Int("total_urls", len(grpcURLs)).
			Msg("starting with no healthy connections, will attempt recovery")
	}

	// Start health check goroutine
	go client.runHealthChecks()

	return client, nil
}

// Close closes all gRPC connections
func (c *RegistryClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var errs []error
	for _, connInfo := range c.connections {
		if connInfo.conn != nil {
			if err := connInfo.conn.Close(); err != nil {
				errs = append(errs, fmt.Errorf("failed to close connection to %s: %w", connInfo.url, err))
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors closing connections: %v", errs)
	}
	return nil
}

// runHealthChecks periodically checks connection health
func (c *RegistryClient) runHealthChecks() {
	ticker := time.NewTicker(c.healthCheckInterval)
	defer ticker.Stop()

	for range ticker.C {
		c.checkAllConnections()
	}
}

// checkAllConnections checks the health of all connections
func (c *RegistryClient) checkAllConnections() {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	c.checkAllConnectionsLocked(false)
}

// TryRecoverConnections attempts to recover all unhealthy connections immediately
func (c *RegistryClient) TryRecoverConnections() {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	c.logger.Debug().Msg("attempting immediate connection recovery")
	c.checkAllConnectionsLocked(true)
}

// checkAllConnectionsLocked checks connections while holding the lock
func (c *RegistryClient) checkAllConnectionsLocked(forceCheck bool) {
	for i, connInfo := range c.connections {
		// Skip if recently checked and still in cooldown (unless forced)
		if !forceCheck && !connInfo.healthy && time.Since(connInfo.lastCheck) < c.unhealthyCooldown {
			continue
		}

		// Check or recreate connection
		if connInfo.conn == nil && connInfo.queryClient == nil {
			// Try to establish connection
			conn, err := grpc.Dial(connInfo.url, grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				c.logger.Debug().
					Str("url", connInfo.url).
					Err(err).
					Msg("failed to reconnect during health check")
				connInfo.lastCheck = time.Now()
				continue
			}
			connInfo.conn = conn
			connInfo.queryClient = uregistrytypes.NewQueryClient(conn)
		}

		// Check connection state
		wasHealthy := connInfo.healthy
		
		if connInfo.conn != nil {
			// Real gRPC connection
			state := connInfo.conn.GetState()
			if state == connectivity.Ready || state == connectivity.Idle {
				// Try a simple query to verify it actually works
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				_, err := connInfo.queryClient.AllChainConfigs(ctx, &uregistrytypes.QueryAllChainConfigsRequest{})
				cancel()
				connInfo.healthy = err == nil
				if err != nil {
					c.logger.Debug().
						Str("url", connInfo.url).
						Err(err).
						Msg("health check query failed")
				}
			} else {
				connInfo.healthy = false
			}
		} else if connInfo.queryClient != nil {
			// Mock connection - skip health check, preserve current state
			// Don't change health status for mocks
		} else {
			// No connection at all
			connInfo.healthy = false
		}

		connInfo.lastCheck = time.Now()

		// Log status changes
		if wasHealthy && !connInfo.healthy {
			stateStr := "unknown"
			if connInfo.conn != nil {
				stateStr = connInfo.conn.GetState().String()
			}
			c.logger.Warn().
				Str("url", connInfo.url).
				Str("state", stateStr).
				Msg("connection marked unhealthy")
		} else if !wasHealthy && connInfo.healthy {
			c.logger.Info().
				Str("url", connInfo.url).
				Msg("connection recovered")
		}

		// Update current index if current connection became unhealthy
		if i == c.currentIdx && !connInfo.healthy {
			c.selectNextHealthy()
		}
	}
}

// selectNextHealthy selects the next healthy connection
func (c *RegistryClient) selectNextHealthy() {
	start := c.currentIdx
	for i := 0; i < len(c.connections); i++ {
		idx := (start + i + 1) % len(c.connections)
		if c.connections[idx].healthy {
			c.currentIdx = idx
			c.logger.Info().
				Str("url", c.connections[idx].url).
				Int("index", idx).
				Msg("switched to healthy connection")
			return
		}
	}
	// No healthy connections found, keep current
	c.logger.Error().Msg("no healthy connections available")
}

// getHealthyConnection returns a healthy connection or error
func (c *RegistryClient) getHealthyConnection() (*connectionInfo, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Try current connection first
	if c.currentIdx < len(c.connections) && c.connections[c.currentIdx].healthy {
		return c.connections[c.currentIdx], nil
	}

	// Find any healthy connection
	for _, conn := range c.connections {
		if conn.healthy {
			return conn, nil
		}
	}

	return nil, fmt.Errorf("no healthy connections available")
}

// GetChainConfig fetches configuration for a specific chain
func (c *RegistryClient) GetChainConfig(ctx context.Context, chainID string) (*uregistrytypes.ChainConfig, error) {
	req := &uregistrytypes.QueryChainConfigRequest{
		Chain: chainID,
	}

	resp, err := c.executeWithRetry(ctx, "GetChainConfig", func(queryClient uregistrytypes.QueryClient) (interface{}, error) {
		return queryClient.ChainConfig(ctx, req)
	})

	if err != nil {
		return nil, err
	}

	return resp.(*uregistrytypes.QueryChainConfigResponse).Config, nil
}

// GetAllChainConfigs fetches all chain configurations
func (c *RegistryClient) GetAllChainConfigs(ctx context.Context) ([]*uregistrytypes.ChainConfig, error) {
	req := &uregistrytypes.QueryAllChainConfigsRequest{}

	resp, err := c.executeWithRetry(ctx, "GetAllChainConfigs", func(queryClient uregistrytypes.QueryClient) (interface{}, error) {
		return queryClient.AllChainConfigs(ctx, req)
	})

	if err != nil {
		return nil, err
	}

	return resp.(*uregistrytypes.QueryAllChainConfigsResponse).Configs, nil
}

// GetTokenConfig fetches configuration for a specific token
func (c *RegistryClient) GetTokenConfig(ctx context.Context, chain, address string) (*uregistrytypes.TokenConfig, error) {
	req := &uregistrytypes.QueryTokenConfigRequest{
		Chain:   chain,
		Address: address,
	}

	resp, err := c.executeWithRetry(ctx, "GetTokenConfig", func(queryClient uregistrytypes.QueryClient) (interface{}, error) {
		return queryClient.TokenConfig(ctx, req)
	})

	if err != nil {
		return nil, err
	}

	return resp.(*uregistrytypes.QueryTokenConfigResponse).Config, nil
}

// GetTokenConfigsByChain fetches all token configurations for a specific chain
func (c *RegistryClient) GetTokenConfigsByChain(ctx context.Context, chain string) ([]*uregistrytypes.TokenConfig, error) {
	req := &uregistrytypes.QueryTokenConfigsByChainRequest{
		Chain: chain,
	}

	resp, err := c.executeWithRetry(ctx, "GetTokenConfigsByChain", func(queryClient uregistrytypes.QueryClient) (interface{}, error) {
		return queryClient.TokenConfigsByChain(ctx, req)
	})

	if err != nil {
		return nil, err
	}

	return resp.(*uregistrytypes.QueryTokenConfigsByChainResponse).Configs, nil
}

// GetAllTokenConfigs fetches all token configurations
func (c *RegistryClient) GetAllTokenConfigs(ctx context.Context) ([]*uregistrytypes.TokenConfig, error) {
	req := &uregistrytypes.QueryAllTokenConfigsRequest{}

	resp, err := c.executeWithRetry(ctx, "GetAllTokenConfigs", func(queryClient uregistrytypes.QueryClient) (interface{}, error) {
		return queryClient.AllTokenConfigs(ctx, req)
	})

	if err != nil {
		return nil, err
	}

	return resp.(*uregistrytypes.QueryAllTokenConfigsResponse).Configs, nil
}

// executeWithRetry executes a function with exponential backoff retry and failover
func (c *RegistryClient) executeWithRetry(ctx context.Context, queryName string, fn func(uregistrytypes.QueryClient) (interface{}, error)) (interface{}, error) {
	var lastErr error
	backoff := c.retryBackoff
	connectionAttempts := 0
	maxConnectionAttempts := len(c.connections)

	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			c.logger.Debug().
				Int("attempt", attempt).
				Dur("backoff", backoff).
				Msg("retrying after backoff")

			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}

			// Exponential backoff: 1s → 2s → 4s → 8s (max 30s)
			backoff *= 2
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
		}

		// Try with current connection
		connInfo, err := c.getHealthyConnection()
		if err != nil {
			lastErr = err
			c.logger.Error().
				Err(err).
				Str("query", queryName).
				Msg("no healthy connections available")
			
			// Try to recover connections immediately
			c.TryRecoverConnections()
			
			// Check again after recovery attempt
			connInfo, err = c.getHealthyConnection()
			if err != nil {
				// If still no healthy connections, try to trigger failover
				if connectionAttempts < maxConnectionAttempts {
					connectionAttempts++
					c.mu.Lock()
					c.selectNextHealthy()
					c.mu.Unlock()
					continue
				}
				// Continue retrying instead of failing immediately
				continue
			}
		}

		c.logger.Debug().
			Str("url", connInfo.url).
			Str("query", queryName).
			Int("attempt", attempt+1).
			Msg("executing query")

		result, err := fn(connInfo.queryClient)
		if err == nil {
			return result, nil
		}

		lastErr = err
		c.logger.Warn().
			Err(err).
			Str("url", connInfo.url).
			Str("query", queryName).
			Int("attempt", attempt+1).
			Int("max_retries", c.maxRetries).
			Msg("query failed")

		// Mark connection as unhealthy if it's a connection error
		if isConnectionError(err) {
			c.mu.Lock()
			connInfo.healthy = false
			connInfo.lastCheck = time.Now()
			c.logger.Warn().
				Str("url", connInfo.url).
				Msg("marking connection unhealthy due to error")
			c.selectNextHealthy()
			c.mu.Unlock()
			
			// Try immediate recovery for connection errors
			c.TryRecoverConnections()
			
			// Don't count connection failures against retry limit
			if connectionAttempts < maxConnectionAttempts {
				connectionAttempts++
				attempt--
			}
		}
	}

	return nil, fmt.Errorf("failed after %d attempts: %w", c.maxRetries+1, lastErr)
}

// isConnectionError checks if the error is a connection-related error
func isConnectionError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return contains(errStr, "connection refused") ||
		contains(errStr, "connection reset") ||
		contains(errStr, "no connection") ||
		contains(errStr, "transport closing") ||
		contains(errStr, "unavailable") ||
		contains(errStr, "deadline exceeded")
}

// contains checks if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && s[len(s)-len(substr):] == substr ||
		len(s) >= len(substr) && s[:len(substr)] == substr ||
		len(s) > len(substr) && containsMiddle(s, substr)
}

func containsMiddle(s, substr string) bool {
	for i := 1; i < len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
