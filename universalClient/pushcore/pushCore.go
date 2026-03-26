// Package pushcore provides a client for interacting with Push Chain gRPC endpoints.
// It implements a fan-out pattern that tries multiple endpoints in round-robin order
// to provide high availability and fault tolerance.
package pushcore

import (
	"context"
	"errors"
	"fmt"
	"math/big"
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
	authzClients      []authz.QueryClient           // AuthZ query clients
	authClients       []authtypes.QueryClient       // Auth query clients
	conns             []*grpc.ClientConn            // Owned gRPC connections (for cleanup)
	rr                uint32                        // Round-robin counter for endpoint selection
}

// New creates a new Client by dialing the provided gRPC URLs.
// It attempts to connect to all endpoints and skips any that fail to dial.
// At least one endpoint must succeed, otherwise an error is returned.
func New(urls []string, logger zerolog.Logger) (*Client, error) {
	if len(urls) == 0 {
		return nil, errors.New("pushcore: at least one gRPC URL is required")
	}

	c := &Client{
		logger: logger.With().Str("component", "push_core").Logger(),
	}

	for i, u := range urls {
		conn, err := createGRPCConnection(u)
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
		c.authzClients = append(c.authzClients, authz.NewQueryClient(conn))
		c.authClients = append(c.authClients, authtypes.NewQueryClient(conn))
	}

	if len(c.eps) == 0 {
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
	c.authzClients = nil
	c.authClients = nil
	return firstErr
}

// retryWithRoundRobin executes a function across multiple endpoints in round-robin order.
// It tries each endpoint until one succeeds or all fail.
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

// GetGasPrice retrieves the median gas price for a specific chain from the on-chain oracle.
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

			if len(resp.GasPrice.Prices) == 0 {
				return nil, fmt.Errorf("pushcore: no gas prices available for chain %s", chainID)
			}

			medianIdx := resp.GasPrice.MedianIndex
			if medianIdx >= uint64(len(resp.GasPrice.Prices)) {
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
func (c *Client) GetGranteeGrants(ctx context.Context, granteeAddr string) (*authz.QueryGranteeGrantsResponse, error) {
	return retryWithRoundRobin(
		len(c.authzClients),
		&c.rr,
		func(idx int) (*authz.QueryGranteeGrantsResponse, error) {
			return c.authzClients[idx].GranteeGrants(ctx, &authz.QueryGranteeGrantsRequest{
				Grantee: granteeAddr,
			})
		},
		"GetGranteeGrants",
		c.logger,
	)
}

// GetAccount retrieves account information for a given address.
func (c *Client) GetAccount(ctx context.Context, address string) (*authtypes.QueryAccountResponse, error) {
	return retryWithRoundRobin(
		len(c.authClients),
		&c.rr,
		func(idx int) (*authtypes.QueryAccountResponse, error) {
			return c.authClients[idx].Account(ctx, &authtypes.QueryAccountRequest{
				Address: address,
			})
		},
		"GetAccount",
		c.logger,
	)
}

// BroadcastTx broadcasts a signed transaction to the chain.
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

// GetActiveTssEvents retrieves up to the first 1000 active TSS events from Push Chain.
func (c *Client) GetActiveTssEvents(ctx context.Context) ([]*utsstypes.TssEvent, error) {
	return retryWithRoundRobin(
		len(c.utssClients),
		&c.rr,
		func(idx int) ([]*utsstypes.TssEvent, error) {
			resp, err := c.utssClients[idx].ActiveTssEvents(ctx, &utsstypes.QueryActiveTssEventsRequest{
				Pagination: &query.PageRequest{Limit: 1000},
			})
			if err != nil {
				return nil, err
			}
			return resp.Events, nil
		},
		"GetActiveTssEvents",
		c.logger,
	)
}

// GetAllPendingOutbounds retrieves up to the first 1000 pending outbound transactions from Push Chain.
func (c *Client) GetAllPendingOutbounds(ctx context.Context) ([]*uexecutortypes.PendingOutboundEntry, []*uexecutortypes.OutboundTx, error) {
	resp, err := retryWithRoundRobin(
		len(c.uexecutorClients),
		&c.rr,
		func(idx int) (*uexecutortypes.QueryAllPendingOutboundsResponse, error) {
			return c.uexecutorClients[idx].AllPendingOutbounds(ctx, &uexecutortypes.QueryAllPendingOutboundsRequest{
				Pagination: &query.PageRequest{Limit: 1000},
			})
		},
		"GetAllPendingOutbounds",
		c.logger,
	)
	if err != nil {
		return nil, nil, err
	}
	return resp.Entries, resp.Outbounds, nil
}

// createGRPCConnection creates a gRPC connection with appropriate transport security.
// It automatically detects whether to use TLS based on the URL scheme
// and adds default port 9090 if no port is specified.
func createGRPCConnection(endpoint string) (*grpc.ClientConn, error) {
	if endpoint == "" {
		return nil, fmt.Errorf("empty endpoint provided")
	}

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
		lastColon := strings.LastIndex(processedEndpoint, ":")
		afterColon := processedEndpoint[lastColon+1:]
		if afterColon == "" || strings.Contains(afterColon, "/") {
			processedEndpoint = strings.TrimSuffix(processedEndpoint, ":") + ":9090"
		}
	}

	var opts []grpc.DialOption
	if useTLS {
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(nil)))
	} else {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	conn, err := grpc.NewClient(processedEndpoint, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC connection to %s: %w", processedEndpoint, err)
	}

	return conn, nil
}
