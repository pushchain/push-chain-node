package pushcore

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	cmtservice "github.com/cosmos/cosmos-sdk/client/grpc/cmtservice"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/cosmos/cosmos-sdk/x/authz"
	"github.com/pushchain/push-chain-node/universalClient/constant"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
	utsstypes "github.com/pushchain/push-chain-node/x/utss/types"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// Client is a minimal fan-out client over multiple gRPC endpoints.
// Each call tries endpoints in round-robin order and returns the first success.
type Client struct {
	logger             zerolog.Logger
	eps                []uregistrytypes.QueryClient
	uvalidatorClients  []uvalidatortypes.QueryClient
	utssClients        []utsstypes.QueryClient
	cmtClients         []cmtservice.ServiceClient
	txClients          []tx.ServiceClient // for querying transactions by events
	conns              []*grpc.ClientConn // owned connections for Close()
	rr                 uint32             // round-robin counter
}

// New dials the provided gRPC URLs (best-effort) and builds a Client.
// - Uses insecure transport by default.
// - Skips endpoints that fail to dial; requires at least one success.
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

// Close closes all owned connections.
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
	c.cmtClients = nil
	c.txClients = nil
	return firstErr
}

// GetAllChainConfigs tries each endpoint once in round-robin order.
// If all endpoints fail, returns the last error.
func (c *Client) GetAllChainConfigs(ctx context.Context) ([]*uregistrytypes.ChainConfig, error) {
	if len(c.eps) == 0 {
		return nil, errors.New("pushcore: no endpoints configured")
	}

	start := int(atomic.AddUint32(&c.rr, 1)-1) % len(c.eps)

	var lastErr error
	for i := 0; i < len(c.eps); i++ {
		idx := (start + i) % len(c.eps)
		qc := c.eps[idx]

		resp, err := qc.AllChainConfigs(ctx, &uregistrytypes.QueryAllChainConfigsRequest{})
		if err == nil {
			return resp.Configs, nil
		}

		lastErr = err
		c.logger.Debug().
			Int("attempt", i+1).
			Int("endpoint_index", idx).
			Err(err).
			Msg("GetAllChainConfigs failed; trying next endpoint")
	}

	return nil, fmt.Errorf("pushcore: GetAllChainConfigs failed on all %d endpoints: %w", len(c.eps), lastErr)
}

// CreateGRPCConnection creates a gRPC connection with appropriate transport security.
// It automatically detects whether to use TLS based on the URL scheme (https:// or http://).
// The function handles:
//   - https:// URLs: Uses TLS with default credentials
//   - http:// or no scheme: Uses insecure connection
//   - Automatically adds default port 9090 if no port is specified
//
// The endpoint is processed to remove the scheme prefix before dialing.
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

// ExtractHostnameFromURL extracts the hostname from a URL string.
// It handles various URL formats including:
//   - Full URLs with scheme (https://example.com:443)
//   - URLs without scheme (example.com:9090)
//   - Plain hostnames (example.com)
//
// The function returns just the hostname without port or scheme.
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

// QueryGrantsWithRetry queries AuthZ grants for a grantee with retry logic
func QueryGrantsWithRetry(grpcURL, granteeAddr string, cdc *codec.ProtoCodec, log zerolog.Logger) (string, []string, error) {
	// Simple retry: 15s, then 30s
	timeouts := []time.Duration{15 * time.Second, 30 * time.Second}

	for attempt, timeout := range timeouts {
		conn, err := CreateGRPCConnection(grpcURL)
		if err != nil {
			return "", nil, err
		}
		defer conn.Close()

		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()

		// Single gRPC call to get all grants
		authzClient := authz.NewQueryClient(conn)
		grantResp, err := authzClient.GranteeGrants(ctx, &authz.QueryGranteeGrantsRequest{
			Grantee: granteeAddr,
		})

		if err == nil {
			// Process the grants
			return processGrants(grantResp, granteeAddr, cdc)
		}

		// On timeout, retry with longer timeout
		if ctx.Err() == context.DeadlineExceeded && attempt < len(timeouts)-1 {
			log.Warn().
				Int("attempt", attempt+1).
				Dur("timeout", timeout).
				Msg("Timeout querying grants, retrying...")
			time.Sleep(2 * time.Second)
			continue
		}

		return "", nil, fmt.Errorf("failed to query grants: %w", err)
	}

	return "", nil, fmt.Errorf("failed after all retries")
}

// processGrants processes the AuthZ grant response
func processGrants(grantResp *authz.QueryGranteeGrantsResponse, granteeAddr string, cdc *codec.ProtoCodec) (string, []string, error) {
	if len(grantResp.Grants) == 0 {
		return "", nil, fmt.Errorf("no AuthZ grants found. Please grant permissions:\npuniversald tx authz grant %s generic --msg-type=/uexecutor.v1.MsgVoteInbound --from <granter>", granteeAddr)
	}

	authorizedMessages := make(map[string]string) // msgType -> granter
	var granter string

	// Check each grant for our required message types
	for _, grant := range grantResp.Grants {
		if grant.Authorization == nil {
			continue
		}

		// Only process GenericAuthorization
		if grant.Authorization.TypeUrl != "/cosmos.authz.v1beta1.GenericAuthorization" {
			continue
		}

		msgType, err := extractMessageType(grant.Authorization, cdc)
		if err != nil {
			continue // Skip if we can't extract the message type
		}

		// Check if this is a required message
		for _, requiredMsg := range constant.SupportedMessages {
			if msgType == requiredMsg {
				// Check if grant is not expired
				if grant.Expiration != nil && grant.Expiration.Before(time.Now()) {
					continue // Skip expired grants
				}

				authorizedMessages[msgType] = grant.Granter
				if granter == "" {
					granter = grant.Granter
				}
				break
			}
		}
	}

	// Check if all required messages are authorized
	var missingMessages []string
	for _, requiredMsg := range constant.SupportedMessages {
		if _, ok := authorizedMessages[requiredMsg]; !ok {
			missingMessages = append(missingMessages, requiredMsg)
		}
	}

	if len(missingMessages) > 0 {
		return "", nil, fmt.Errorf("missing AuthZ grants for: %v\nGrant permissions using:\npuniversald tx authz grant %s generic --msg-type=<message_type> --from <granter>", missingMessages, granteeAddr)
	}

	// Return authorized messages
	authorizedList := make([]string, 0, len(authorizedMessages))
	for msgType := range authorizedMessages {
		authorizedList = append(authorizedList, msgType)
	}

	return granter, authorizedList, nil
}

// extractMessageType extracts the message type from a GenericAuthorization
func extractMessageType(authzAny *codectypes.Any, cdc *codec.ProtoCodec) (string, error) {
	var genericAuth authz.GenericAuthorization
	if err := cdc.Unmarshal(authzAny.Value, &genericAuth); err != nil {
		return "", err
	}
	return genericAuth.Msg, nil
}

// GetLatestBlockNum returns the latest block number from Push Chain.
// It tries each endpoint in round-robin order until one succeeds.
func (c *Client) GetLatestBlockNum() (uint64, error) {
	if len(c.cmtClients) == 0 {
		return 0, errors.New("pushcore: no endpoints configured")
	}

	start := int(atomic.AddUint32(&c.rr, 1)-1) % len(c.cmtClients)

	var lastErr error
	for i := 0; i < len(c.cmtClients); i++ {
		idx := (start + i) % len(c.cmtClients)
		client := c.cmtClients[idx]

		resp, err := client.GetLatestBlock(context.Background(), &cmtservice.GetLatestBlockRequest{})
		if err == nil && resp.SdkBlock != nil {
			return uint64(resp.SdkBlock.Header.Height), nil
		}

		lastErr = err
		c.logger.Debug().
			Int("attempt", i+1).
			Int("endpoint_index", idx).
			Err(err).
			Msg("GetLatestBlockNum failed; trying next endpoint")
	}

	return 0, fmt.Errorf("pushcore: GetLatestBlockNum failed on all %d endpoints: %w", len(c.cmtClients), lastErr)
}

// GetUniversalValidators returns all universal validators from Push Chain.
// It tries each endpoint in round-robin order until one succeeds.
func (c *Client) GetUniversalValidators() ([]*uvalidatortypes.UniversalValidator, error) {
	if len(c.uvalidatorClients) == 0 {
		return nil, errors.New("pushcore: no endpoints configured")
	}

	start := int(atomic.AddUint32(&c.rr, 1)-1) % len(c.uvalidatorClients)

	var lastErr error
	for i := 0; i < len(c.uvalidatorClients); i++ {
		idx := (start + i) % len(c.uvalidatorClients)
		client := c.uvalidatorClients[idx]

		resp, err := client.AllUniversalValidators(context.Background(), &uvalidatortypes.QueryUniversalValidatorsSetRequest{})
		if err == nil {
			return resp.UniversalValidator, nil
		}

		lastErr = err
		c.logger.Debug().
			Int("attempt", i+1).
			Int("endpoint_index", idx).
			Err(err).
			Msg("GetUniversalValidators failed; trying next endpoint")
	}

	return nil, fmt.Errorf("pushcore: GetUniversalValidators failed on all %d endpoints: %w", len(c.uvalidatorClients), lastErr)
}

// GetCurrentTSSKeyId returns the current TSS key ID from Push Chain.
// It tries each endpoint in round-robin order until one succeeds.
// Returns empty string if no key exists.
func (c *Client) GetCurrentTSSKeyId() (string, error) {
	if len(c.utssClients) == 0 {
		return "", errors.New("pushcore: no endpoints configured")
	}

	start := int(atomic.AddUint32(&c.rr, 1)-1) % len(c.utssClients)

	var lastErr error
	for i := 0; i < len(c.utssClients); i++ {
		idx := (start + i) % len(c.utssClients)
		client := c.utssClients[idx]

		resp, err := client.CurrentKey(context.Background(), &utsstypes.QueryCurrentKeyRequest{})
		if err == nil {
			if resp.Key != nil {
				return resp.Key.KeyId, nil
			}
			return "", nil // No key exists
		}

		lastErr = err
		c.logger.Debug().
			Int("attempt", i+1).
			Int("endpoint_index", idx).
			Err(err).
			Msg("GetCurrentTSSKeyId failed; trying next endpoint")
	}

	return "", fmt.Errorf("pushcore: GetCurrentTSSKeyId failed on all %d endpoints: %w", len(c.utssClients), lastErr)
}

// TxResult represents a transaction result with its events.
type TxResult struct {
	TxHash      string
	Height      int64
	TxResponse  *tx.GetTxResponse
}

// GetTxsByEvents queries transactions matching the given event query.
// The query should follow Cosmos SDK event query format, e.g., "tss_process_initiated.process_id EXISTS"
// minHeight and maxHeight can be used to filter by block range (0 means no limit).
func (c *Client) GetTxsByEvents(eventQuery string, minHeight, maxHeight uint64, limit uint64) ([]*TxResult, error) {
	if len(c.txClients) == 0 {
		return nil, errors.New("pushcore: no endpoints configured")
	}

	start := int(atomic.AddUint32(&c.rr, 1)-1) % len(c.txClients)

	var lastErr error
	for i := 0; i < len(c.txClients); i++ {
		idx := (start + i) % len(c.txClients)
		client := c.txClients[idx]

		// Build the query events
		events := []string{eventQuery}

		// Add height range filters if specified
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

		req := &tx.GetTxsEventRequest{
			Query: queryString,
			Pagination: &query.PageRequest{
				Limit: pageLimit,
			},
			OrderBy: tx.OrderBy_ORDER_BY_ASC,
		}

		resp, err := client.GetTxsEvent(context.Background(), req)
		if err == nil {
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
		}

		lastErr = err
		c.logger.Debug().
			Int("attempt", i+1).
			Int("endpoint_index", idx).
			Err(err).
			Msg("GetTxsByEvents failed; trying next endpoint")
	}

	return nil, fmt.Errorf("pushcore: GetTxsByEvents failed on all %d endpoints: %w", len(c.txClients), lastErr)
}

// GetBlockByHeight returns block information for a specific height.
func (c *Client) GetBlockByHeight(height int64) (*cmtservice.GetBlockByHeightResponse, error) {
	if len(c.cmtClients) == 0 {
		return nil, errors.New("pushcore: no endpoints configured")
	}

	start := int(atomic.AddUint32(&c.rr, 1)-1) % len(c.cmtClients)

	var lastErr error
	for i := 0; i < len(c.cmtClients); i++ {
		idx := (start + i) % len(c.cmtClients)
		client := c.cmtClients[idx]

		resp, err := client.GetBlockByHeight(context.Background(), &cmtservice.GetBlockByHeightRequest{
			Height: height,
		})
		if err == nil {
			return resp, nil
		}

		lastErr = err
		c.logger.Debug().
			Int("attempt", i+1).
			Int("endpoint_index", idx).
			Err(err).
			Msg("GetBlockByHeight failed; trying next endpoint")
	}

	return nil, fmt.Errorf("pushcore: GetBlockByHeight failed on all %d endpoints: %w", len(c.cmtClients), lastErr)
}
