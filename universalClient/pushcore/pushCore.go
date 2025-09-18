package pushcore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
)

// Client is a minimal fan-out client over multiple gRPC endpoints.
// Each call tries endpoints in round-robin order and returns the first success.
type Client struct {
	logger zerolog.Logger
	eps    []uregistrytypes.QueryClient
	conns  []*grpc.ClientConn // owned connections for Close()
	rr     uint32             // round-robin counter
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
		var opts []grpc.DialOption
		if strings.HasPrefix(u, "https://") {
			// Use TLS for HTTPS endpoints
			u = strings.TrimPrefix(u, "https://")
			opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(nil)))
		} else {
			// Use insecure for non-HTTPS endpoints
			opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
		}

		conn, err := grpc.Dial(u, opts...)
		if err != nil {
			c.logger.Warn().Str("url", u).Int("index", i).Err(err).Msg("dial failed; skipping endpoint")
			continue
		}
		c.conns = append(c.conns, conn)
		c.eps = append(c.eps, uregistrytypes.NewQueryClient(conn))
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
