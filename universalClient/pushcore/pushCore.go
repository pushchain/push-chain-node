// Package pushcore provides a client for interacting with Push Chain gRPC endpoints.
// It implements a fan-out pattern that tries multiple endpoints in round-robin order
// to provide high availability and fault tolerance.
package pushcore

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"strings"
	"sync/atomic"

	cmtservice "github.com/cosmos/cosmos-sdk/client/grpc/cmtservice"
	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/cosmos/cosmos-sdk/types/tx"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
	utsstypes "github.com/pushchain/push-chain-node/x/utss/types"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// Client is a fan-out client that connects to multiple Push Chain gRPC endpoints.
// It implements round-robin failover, trying each endpoint in sequence until one succeeds.
type Client struct {
	logger            zerolog.Logger                // Logger for client operations
	eps               []uregistrytypes.QueryClient  // Registry query clients
	uvalidatorClients []uvalidatortypes.QueryClient // Universal validator query clients
	utssClients       []utsstypes.QueryClient       // TSS query clients
	uexecutorClients  []uexecutortypes.QueryClient  // Executor query clients (for gas price queries)
	cmtClients        []cmtservice.ServiceClient    // CometBFT service clients
	txClients         []tx.ServiceClient            // Transaction service clients
	conns             []*grpc.ClientConn            // Owned gRPC connections (for cleanup)
	rr                uint32                        // Round-robin counter for endpoint selection
}

// TxResult represents a transaction result with its associated events and metadata.
type TxResult struct {
	TxHash     string            // Transaction hash
	Height     int64             // Block height where the transaction was included
	TxResponse *tx.GetTxResponse // Full transaction response from the chain
}

// New creates a new Client by dialing the provided gRPC URLs.
// It attempts to connect to all endpoints and skips any that fail to dial.
// At least one endpoint must succeed, otherwise an error is returned.
//
// Parameters:
//   - urls: List of gRPC endpoint URLs (schemes are automatically detected)
//   - logger: Logger instance for client operations
//
// Returns:
//   - *Client: A configured client instance, or nil on error
//   - error: Error if all endpoints fail to connect
func New(urls []string, logger zerolog.Logger) (*Client, error) {
	if len(urls) == 0 {
		return nil, errors.New("pushcore: at least one gRPC URL is required")
	}

	c := &Client{
		logger: logger.With().Str("component", "push_core").Logger(),
	}

	for i, u := range urls {
		// Use the local utility function
		conn, err := CreateGRPCConnection(u)
		if err != nil {
			c.logger.Warn().Str("url", u).Int("index", i).Err(err).Msg("dial failed; skipping endpoint")
			continue
		}
		c.conns = append(c.conns, conn)
		c.eps = append(c.eps, uregistrytypes.NewQueryClient(conn))
		c.uvalidatorClients = append(c.uvalidatorClients, uvalidatortypes.NewQueryClient(conn))
		c.utssClients = append(c.utssClients, utsstypes.NewQueryClient(conn))
		c.uexecutorClients = append(c.uexecutorClients, uexecutortypes.NewQueryClient(conn))
		c.cmtClients = append(c.cmtClients, cmtservice.NewServiceClient(conn))
		c.txClients = append(c.txClients, tx.NewServiceClient(conn))
	}

	if len(c.eps) == 0 {
		// nothing usable
		_ = c.Close()
		return nil, fmt.Errorf("pushcore: all dials failed (%d urls)", len(urls))
	}

	return c, nil
}

// Close gracefully closes all gRPC connections owned by the client.
// Returns the first error encountered, if any.
func (c *Client) Close() error {
	var firstErr error
	for _, conn := range c.conns {
		if err := conn.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	c.conns = nil
	c.eps = nil
	c.uvalidatorClients = nil
	c.utssClients = nil
	c.uexecutorClients = nil
	c.cmtClients = nil
	c.txClients = nil
	return firstErr
}

// retryWithRoundRobin executes a function across multiple endpoints in round-robin order.
// It tries each endpoint until one succeeds or all fail.
//
// Parameters:
//   - numClients: Number of client endpoints available
//   - rrCounter: Pointer to round-robin counter (atomic)
//   - operation: Function to execute for each attempt, receives the endpoint index
//   - operationName: Name of the operation (for logging and error messages)
//   - logger: Logger for debug messages
//
// Returns:
//   - T: Result from the operation if successful
//   - error: Error if all endpoints fail
func retryWithRoundRobin[T any](
	numClients int,
	rrCounter *uint32,
	operation func(idx int) (T, error),
	operationName string,
	logger zerolog.Logger,
) (T, error) {
	var zero T
	if numClients == 0 {
		return zero, errors.New("pushcore: no endpoints configured")
	}

	start := int(atomic.AddUint32(rrCounter, 1)-1) % numClients

	var lastErr error
	for i := 0; i < numClients; i++ {
		idx := (start + i) % numClients

		result, err := operation(idx)
		if err == nil {
			return result, nil
		}

		lastErr = err
		logger.Debug().
			Int("attempt", i+1).
			Int("endpoint_index", idx).
			Err(err).
			Msgf("%s failed; trying next endpoint", operationName)
	}

	return zero, fmt.Errorf("pushcore: %s failed on all %d endpoints: %w", operationName, numClients, lastErr)
}

// GetAllChainConfigs retrieves all chain configurations from Push Chain.
// It tries each endpoint in round-robin order until one succeeds.
//
// Parameters:
//   - ctx: Context for the request
//
// Returns:
//   - []*uregistrytypes.ChainConfig: List of chain configurations
//   - error: Error if all endpoints fail
func (c *Client) GetAllChainConfigs(ctx context.Context) ([]*uregistrytypes.ChainConfig, error) {
	return retryWithRoundRobin(
		len(c.eps),
		&c.rr,
		func(idx int) ([]*uregistrytypes.ChainConfig, error) {
			resp, err := c.eps[idx].AllChainConfigs(ctx, &uregistrytypes.QueryAllChainConfigsRequest{})
			if err != nil {
				return nil, err
			}
			return resp.Configs, nil
		},
		"GetAllChainConfigs",
		c.logger,
	)
}

// GetLatestBlock retrieves the latest block from Push Chain.
// It tries each endpoint in round-robin order until one succeeds.
//
// Parameters:
//   - ctx: Context for the request
//
// Returns:
//   - uint64: Latest block height
//   - error: Error if all endpoints fail
func (c *Client) GetLatestBlock(ctx context.Context) (uint64, error) {
	return retryWithRoundRobin(
		len(c.cmtClients),
		&c.rr,
		func(idx int) (uint64, error) {
			resp, err := c.cmtClients[idx].GetLatestBlock(ctx, &cmtservice.GetLatestBlockRequest{})
			if err != nil {
				return 0, err
			}
			if resp.SdkBlock == nil {
				return 0, errors.New("pushcore: SdkBlock is nil")
			}
			return uint64(resp.SdkBlock.Header.Height), nil
		},
		"GetLatestBlock",
		c.logger,
	)
}

// GetAllUniversalValidators retrieves all universal validators from Push Chain.
// It tries each endpoint in round-robin order until one succeeds.
//
// Parameters:
//   - ctx: Context for the request
//
// Returns:
//   - []*uvalidatortypes.UniversalValidator: List of universal validators
//   - error: Error if all endpoints fail
func (c *Client) GetAllUniversalValidators(ctx context.Context) ([]*uvalidatortypes.UniversalValidator, error) {
	return retryWithRoundRobin(
		len(c.uvalidatorClients),
		&c.rr,
		func(idx int) ([]*uvalidatortypes.UniversalValidator, error) {
			resp, err := c.uvalidatorClients[idx].AllUniversalValidators(ctx, &uvalidatortypes.QueryUniversalValidatorsSetRequest{})
			if err != nil {
				return nil, err
			}
			return resp.UniversalValidator, nil
		},
		"GetAllUniversalValidators",
		c.logger,
	)
}

// GetCurrentKey retrieves the current TSS key from Push Chain.
// It tries each endpoint in round-robin order until one succeeds.
//
// Parameters:
//   - ctx: Context for the request
//
// Returns:
//   - *utsstypes.TssKey: TSS key
//   - error: Error if all endpoints fail or no key exists
func (c *Client) GetCurrentKey(ctx context.Context) (*utsstypes.TssKey, error) {
	return retryWithRoundRobin(
		len(c.utssClients),
		&c.rr,
		func(idx int) (*utsstypes.TssKey, error) {
			resp, err := c.utssClients[idx].CurrentKey(ctx, &utsstypes.QueryCurrentKeyRequest{})
			if err != nil {
				return nil, err
			}
			if resp.Key == nil {
				return nil, errors.New("pushcore: no TSS key found")
			}
			return resp.Key, nil
		},
		"GetCurrentKey",
		c.logger,
	)
}

// GetTxsByEvents queries transactions matching the given event query.
// The query should follow Cosmos SDK event query format, e.g., "tss_process_initiated.process_id EXISTS".
//
// Parameters:
//   - ctx: Context for the request
//   - eventQuery: Cosmos SDK event query string
//   - minHeight: Minimum block height to search (0 means no minimum)
//   - maxHeight: Maximum block height to search (0 means no maximum)
//   - limit: Maximum number of results to return (0 defaults to 100)
//
// Returns:
//   - []*TxResult: List of matching transaction results
//   - error: Error if all endpoints fail
func (c *Client) GetTxsByEvents(ctx context.Context, eventQuery string, minHeight, maxHeight uint64, limit uint64) ([]*TxResult, error) {
	// Build the query events (same for all attempts)
	events := []string{eventQuery}
	if minHeight > 0 {
		events = append(events, fmt.Sprintf("tx.height>=%d", minHeight))
	}
	if maxHeight > 0 {
		events = append(events, fmt.Sprintf("tx.height<=%d", maxHeight))
	}

	// Set pagination limit
	pageLimit := limit
	if pageLimit == 0 {
		pageLimit = 100 // default limit
	}

	// Join events with AND to create query string (SDK v0.50+ uses Query field)
	queryString := strings.Join(events, " AND ")

	return retryWithRoundRobin(
		len(c.txClients),
		&c.rr,
		func(idx int) ([]*TxResult, error) {
			req := &tx.GetTxsEventRequest{
				Query: queryString,
				Pagination: &query.PageRequest{
					Limit: pageLimit,
				},
				OrderBy: tx.OrderBy_ORDER_BY_ASC,
			}

			resp, err := c.txClients[idx].GetTxsEvent(ctx, req)
			if err != nil {
				return nil, err
			}

			results := make([]*TxResult, 0, len(resp.TxResponses))
			for _, txResp := range resp.TxResponses {
				results = append(results, &TxResult{
					TxHash: txResp.TxHash,
					Height: txResp.Height,
					TxResponse: &tx.GetTxResponse{
						Tx:         resp.Txs[len(results)],
						TxResponse: txResp,
					},
				})
			}
			return results, nil
		},
		"GetTxsByEvents",
		c.logger,
	)
}

// GetGasPrice retrieves the median gas price for a specific chain from the on-chain oracle.
// The gas price is voted on by universal validators and stored on-chain.
//
// Parameters:
//   - ctx: Context for the request
//   - chainID: Chain identifier in CAIP-2 format (e.g., "eip155:84532" for Base Sepolia)
//
// Returns:
//   - *big.Int: Median gas price in the chain's native unit (Wei for EVM chains, lamports for Solana)
//   - error: Error if all endpoints fail or chainID is invalid
func (c *Client) GetGasPrice(ctx context.Context, chainID string) (*big.Int, error) {
	if chainID == "" {
		return nil, errors.New("pushcore: chainID is required")
	}

	return retryWithRoundRobin(
		len(c.uexecutorClients),
		&c.rr,
		func(idx int) (*big.Int, error) {
			resp, err := c.uexecutorClients[idx].GasPrice(ctx, &uexecutortypes.QueryGasPriceRequest{
				ChainId: chainID,
			})
			if err != nil {
				return nil, err
			}
			if resp.GasPrice == nil {
				return nil, errors.New("pushcore: GasPrice response is nil")
			}

			// Get the median price using MedianIndex
			if len(resp.GasPrice.Prices) == 0 {
				return nil, fmt.Errorf("pushcore: no gas prices available for chain %s", chainID)
			}

			medianIdx := resp.GasPrice.MedianIndex
			if medianIdx >= uint64(len(resp.GasPrice.Prices)) {
				// Fallback to first price if median index is out of bounds
				medianIdx = 0
			}

			medianPrice := resp.GasPrice.Prices[medianIdx]
			return new(big.Int).SetUint64(medianPrice), nil
		},
		"GetGasPrice",
		c.logger,
	)
}

// GetGranteeGrants queries AuthZ grants for a grantee using round-robin logic.
// This function only queries and returns raw grant data; it does not perform validation or processing.
//
// Parameters:
//   - ctx: Context for the request
//   - granteeAddr: Address of the grantee to query grants for
//
// Returns:
//   - *authz.QueryGranteeGrantsResponse: Raw grant response from the chain
//   - error: Error if all endpoints fail
func (c *Client) GetGranteeGrants(ctx context.Context, granteeAddr string) (*authz.QueryGranteeGrantsResponse, error) {
	// Create authz clients from existing connections
	authzClients := make([]authz.QueryClient, len(c.conns))
	for i, conn := range c.conns {
		authzClients[i] = authz.NewQueryClient(conn)
	}

	return retryWithRoundRobin(
		len(authzClients),
		&c.rr,
		func(idx int) (*authz.QueryGranteeGrantsResponse, error) {
			return authzClients[idx].GranteeGrants(ctx, &authz.QueryGranteeGrantsRequest{
				Grantee: granteeAddr,
			})
		},
		"GetGranteeGrants",
		c.logger,
	)
}

// GetAccount retrieves account information for a given address.
// It tries each endpoint in round-robin order until one succeeds.
//
// Parameters:
//   - ctx: Context for the request
//   - address: Bech32 address of the account
//
// Returns:
//   - *authtypes.QueryAccountResponse: Account response
//   - error: Error if all endpoints fail
func (c *Client) GetAccount(ctx context.Context, address string) (*authtypes.QueryAccountResponse, error) {
	// Create auth clients from existing connections
	authClients := make([]authtypes.QueryClient, len(c.conns))
	for i, conn := range c.conns {
		authClients[i] = authtypes.NewQueryClient(conn)
	}

	return retryWithRoundRobin(
		len(authClients),
		&c.rr,
		func(idx int) (*authtypes.QueryAccountResponse, error) {
			return authClients[idx].Account(ctx, &authtypes.QueryAccountRequest{
				Address: address,
			})
		},
		"GetAccount",
		c.logger,
	)
}

// CreateGRPCConnection creates a gRPC connection with appropriate transport security.
// It automatically detects whether to use TLS based on the URL scheme.
//
// The function handles:
//   - https:// URLs: Uses TLS with default credentials
//   - http:// or no scheme: Uses insecure connection
//   - Automatically adds default port 9090 if no port is specified
//
// Parameters:
//   - endpoint: gRPC endpoint URL (scheme is optional, port defaults to 9090)
//
// Returns:
//   - *grpc.ClientConn: gRPC client connection
//   - error: Error if connection fails
func CreateGRPCConnection(endpoint string) (*grpc.ClientConn, error) {
	if endpoint == "" {
		return nil, fmt.Errorf("empty endpoint provided")
	}

	// Determine if we should use TLS and process the endpoint
	processedEndpoint := endpoint
	useTLS := false

	if strings.HasPrefix(endpoint, "https://") {
		processedEndpoint = strings.TrimPrefix(endpoint, "https://")
		useTLS = true
	} else if strings.HasPrefix(endpoint, "http://") {
		processedEndpoint = strings.TrimPrefix(endpoint, "http://")
		useTLS = false
	}

	// Add default port if not present
	if !strings.Contains(processedEndpoint, ":") {
		processedEndpoint = processedEndpoint + ":9090"
	} else {
		// Check if the port is valid (i.e., after the last colon is a number)
		lastColon := strings.LastIndex(processedEndpoint, ":")
		afterColon := processedEndpoint[lastColon+1:]
		if afterColon == "" || strings.Contains(afterColon, "/") {
			// No valid port, add default
			processedEndpoint = strings.TrimSuffix(processedEndpoint, ":") + ":9090"
		}
	}

	// Configure connection options
	var opts []grpc.DialOption
	if useTLS {
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(nil)))
	} else {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	// Create the connection
	conn, err := grpc.NewClient(processedEndpoint, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC connection to %s: %w", processedEndpoint, err)
	}

	return conn, nil
}

// BroadcastTx broadcasts a signed transaction to the chain.
// It tries each endpoint in round-robin order until one succeeds.
//
// Parameters:
//   - ctx: Context for the request
//   - txBytes: Signed transaction bytes
//
// Returns:
//   - *tx.BroadcastTxResponse: Broadcast response containing tx hash and result
//   - error: Error if all endpoints fail
func (c *Client) BroadcastTx(ctx context.Context, txBytes []byte) (*tx.BroadcastTxResponse, error) {
	return retryWithRoundRobin(
		len(c.txClients),
		&c.rr,
		func(idx int) (*tx.BroadcastTxResponse, error) {
			return c.txClients[idx].BroadcastTx(ctx, &tx.BroadcastTxRequest{
				TxBytes: txBytes,
				Mode:    tx.BroadcastMode_BROADCAST_MODE_SYNC,
			})
		},
		"BroadcastTx",
		c.logger,
	)
}

// ExtractHostnameFromURL extracts the hostname from a URL string.
// It handles various URL formats including full URLs with scheme, URLs without scheme, and plain hostnames.
//
// Parameters:
//   - grpcURL: URL string in any format (with or without scheme/port)
//
// Returns:
//   - string: Hostname without port or scheme
//   - error: Error if hostname cannot be extracted
func ExtractHostnameFromURL(grpcURL string) (string, error) {
	if grpcURL == "" {
		return "", fmt.Errorf("empty URL provided")
	}

	// Try to parse as a standard URL
	parsedURL, err := url.Parse(grpcURL)
	if err == nil && parsedURL.Hostname() != "" {
		// Successfully parsed and has a hostname
		return parsedURL.Hostname(), nil
	}

	// Fallback: Handle cases where url.Parse fails or returns empty hostname
	// This handles plain hostnames like "example.com" or "example.com:9090"
	hostname := grpcURL

	// Remove common schemes if present
	if strings.HasPrefix(hostname, "https://") {
		hostname = strings.TrimPrefix(hostname, "https://")
	} else if strings.HasPrefix(hostname, "http://") {
		hostname = strings.TrimPrefix(hostname, "http://")
	}

	// Remove port if present (but check that there's something before the colon)
	if colonIndex := strings.Index(hostname, ":"); colonIndex >= 0 {
		if colonIndex == 0 {
			// URL starts with ":", no hostname
			return "", fmt.Errorf("could not extract hostname from URL: %s", grpcURL)
		}
		hostname = hostname[:colonIndex]
	}

	// Remove any trailing slashes
	hostname = strings.TrimSuffix(hostname, "/")

	if hostname == "" {
		return "", fmt.Errorf("could not extract hostname from URL: %s", grpcURL)
	}

	return hostname, nil
}
