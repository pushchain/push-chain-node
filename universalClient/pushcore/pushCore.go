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
			c.logger.Warn().Int("index", i).Err(err).Msg("dial failed; skipping endpoint")
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
			Str("operation", operationName).
			Int("attempt", i+1).
			Int("endpoint_index", idx).
			Err(err).
			Msg("operation failed; trying next endpoint")
	}

	return zero, fmt.Errorf("pushcore: %s failed on all %d endpoints: %w", operationName, numClients, lastErr)
}

// GetAllChainConfigs retrieves all chain configurations from Push Chain.
func (c *Client) GetAllChainConfigs(ctx context.Context) ([]*uregistrytypes.ChainConfig, error) {
	// Paged rather than a single request: the server paginates this collection,
	// and an omitted PageRequest silently caps the response at the SDK default of
	// 100. A chain missing from this list is simply never watched, so truncation
	// must not be possible.
	var (
		configs []*uregistrytypes.ChainConfig
		nextKey []byte
	)
	for page := 0; page < chainConfigMaxPages; page++ {
		key := nextKey
		resp, err := retryWithRoundRobin(
			len(c.eps),
			&c.rr,
			func(idx int) (*uregistrytypes.QueryAllChainConfigsResponse, error) {
				return c.eps[idx].AllChainConfigs(ctx, &uregistrytypes.QueryAllChainConfigsRequest{
					Pagination: &query.PageRequest{Key: key, Limit: chainConfigPageSize},
				})
			},
			"GetAllChainConfigs",
			c.logger,
		)
		if err != nil {
			return nil, err
		}

		configs = append(configs, resp.Configs...)

		if resp.Pagination == nil || len(resp.Pagination.NextKey) == 0 {
			return configs, nil
		}
		nextKey = resp.Pagination.NextKey
	}

	// Unreachable with any plausible number of chains; loud rather than silent.
	c.logger.Error().
		Int("max_pages", chainConfigMaxPages).
		Int("fetched", len(configs)).
		Msg("chain config page cap reached; some chains will not be watched")
	return configs, nil
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

// GetKeyByID retrieves a single TSS key from the on-chain key history.
// Returns an error if the key ID is not in the history.
func (c *Client) GetKeyByID(ctx context.Context, keyID string) (*utsstypes.TssKey, error) {
	return retryWithRoundRobin(
		len(c.utssClients),
		&c.rr,
		func(idx int) (*utsstypes.TssKey, error) {
			resp, err := c.utssClients[idx].KeyById(ctx, &utsstypes.QueryKeyByIdRequest{KeyId: keyID})
			if err != nil {
				return nil, err
			}
			if resp == nil || resp.Key == nil {
				return nil, fmt.Errorf("pushcore: TSS key %s not found", keyID)
			}
			return resp.Key, nil
		},
		"GetKeyByID",
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

// GetTx queries for a transaction by its hash. Returns the response if found,
// or an error if the tx does not exist or the query fails.
func (c *Client) GetTx(ctx context.Context, txHash string) (*tx.GetTxResponse, error) {
	return retryWithRoundRobin(
		len(c.txClients),
		&c.rr,
		func(idx int) (*tx.GetTxResponse, error) {
			return c.txClients[idx].GetTx(ctx, &tx.GetTxRequest{
				Hash: txHash,
			})
		},
		"GetTx",
		c.logger,
	)
}

// GetPendingTssEvents retrieves up to the first 1000 pending TSS events from Push Chain.
// Sorted by process_id ascending (oldest first).
func (c *Client) GetPendingTssEvents(ctx context.Context) ([]*utsstypes.TssEvent, error) {
	return retryWithRoundRobin(
		len(c.utssClients),
		&c.rr,
		func(idx int) ([]*utsstypes.TssEvent, error) {
			resp, err := c.utssClients[idx].AllPendingTssEvents(ctx, &utsstypes.QueryAllPendingTssEventsRequest{
				Pagination: &query.PageRequest{Limit: 1000},
			})
			if err != nil {
				return nil, err
			}
			return resp.Events, nil
		},
		"GetPendingTssEvents",
		c.logger,
	)
}

// GetPendingFundMigrations retrieves all pending fund migrations from Push Chain.
func (c *Client) GetPendingFundMigrations(ctx context.Context) ([]*utsstypes.FundMigration, error) {
	return retryWithRoundRobin(
		len(c.utssClients),
		&c.rr,
		func(idx int) ([]*utsstypes.FundMigration, error) {
			resp, err := c.utssClients[idx].PendingFundMigrations(ctx, &utsstypes.QueryPendingFundMigrationsRequest{})
			if err != nil {
				return nil, err
			}
			return resp.Migrations, nil
		},
		"GetPendingFundMigrations",
		c.logger,
	)
}

// Page size and page cap for the pending-outbound walk. The cap bounds a single
// poll; the remainder is picked up on the next tick.
const (
	// AllPendingOutbounds pages by offset and returns only Total, never a NextKey,
	// so a key-based walk stops after one page. It also loads and sorts the whole
	// collection per call, so paging saves the server nothing. The sort is not
	// stable and orders on CreatedAt, a block height, so rows sharing a height can
	// change relative order between calls — which makes offset paging able to skip
	// a row outright. One generous request plus a Total check is the only shape
	// that is both correct and cheap against that server.
	// A row costs roughly a kilobyte on the wire, so this stays well inside gRPC's
	// 4 MiB default. Asking for the whole set instead would fail the call outright
	// once the set grew, taking the poll down rather than returning a short list.
	pendingOutboundLimit = 1000

	chainConfigPageSize = 200
	chainConfigMaxPages = 20
)

// GetAllPendingOutbounds retrieves pending outbound transactions from Push Chain.
//
// Read newest-first. An outbound only leaves the pending set once a quorum vote
// terminalizes it, so a row that cannot reach one stays at the head of an
// oldest-first list forever and would hide every newer outbound behind it — on
// every chain, since this query is not chain-scoped. New outbounds always arrive
// at the newest end, so reading that end cannot be starved.
//
// This is discovery only and does not set signing priority. The event store
// hands work to the signer ordered by block_height ASC, so older outbounds are
// still signed first; reading newest-first only decides what reaches the store
// to be ordered in the first place.
func (c *Client) GetAllPendingOutbounds(ctx context.Context) ([]*uexecutortypes.PendingOutboundEntry, []*uexecutortypes.OutboundTx, error) {
	resp, err := retryWithRoundRobin(
		len(c.uexecutorClients),
		&c.rr,
		func(idx int) (*uexecutortypes.QueryAllPendingOutboundsResponse, error) {
			return c.uexecutorClients[idx].AllPendingOutbounds(ctx, &uexecutortypes.QueryAllPendingOutboundsRequest{
				Pagination: &query.PageRequest{Limit: pendingOutboundLimit, Reverse: true},
			})
		},
		"GetAllPendingOutbounds",
		c.logger,
	)
	if err != nil {
		return nil, nil, err
	}

	// Below the limit this read is the whole set and the direction is irrelevant.
	// Above it, the direction is the point: anything older is already known
	// locally, so continuing to re-read it would achieve nothing while the newer
	// rows went unsigned.
	if resp.Pagination != nil && resp.Pagination.Total > pendingOutboundLimit {
		c.logger.Warn().
			Uint64("total", resp.Pagination.Total).
			Uint64("limit", pendingOutboundLimit).
			Msg("pending outbound set exceeds one request; only the newest are read, older rows must already be known locally")
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
