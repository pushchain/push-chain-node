package svm

import (
	"context"
	"fmt"
	"math/big"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/rs/zerolog"
)

// RPCClient provides SVM-specific RPC operations
type RPCClient struct {
	clients []*rpc.Client
	index   uint64
	mu      sync.RWMutex
	logger  zerolog.Logger
}

// NewRPCClient creates a new SVM RPC client from RPC URLs and validates genesis hash
func NewRPCClient(rpcURLs []string, expectedGenesisHash string, logger zerolog.Logger) (*RPCClient, error) {
	if len(rpcURLs) == 0 {
		return nil, fmt.Errorf("no RPC URLs provided")
	}

	log := logger.With().Str("component", "svm_rpc_client").Logger()
	clients := make([]*rpc.Client, 0, len(rpcURLs))

	// Create a temporary context for initial connection and genesis hash verification
	// Use longer timeout for genesis hash verification (30 seconds)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for i, url := range rpcURLs {
		client := rpc.New(url)

		// Verify connection by checking health
		health, err := client.GetHealth(ctx)
		if err != nil {
			log.Warn().Err(err).Int("index", i).Msg("failed to connect to RPC endpoint, skipping")
			continue
		}

		if health != "ok" {
			log.Warn().
				Int("index", i).
				Str("health", health).
				Msg("node is not healthy, skipping")
			continue
		}

		// Verify genesis hash if configured (with timeout handling)
		if expectedGenesisHash != "" {
			genesisHash, err := client.GetGenesisHash(ctx)
			if err != nil {
				// If genesis hash verification fails (timeout or error), log warning but still add client
				// This allows the system to continue even if verification is slow/unavailable
				log.Warn().
					Err(err).
					Int("index", i).
					Str("expected_genesis_hash", expectedGenesisHash).
					Msg("failed to verify genesis hash (timeout or error), proceeding with client anyway")
				clients = append(clients, client)
				continue
			}

			actualHash := genesisHash.String()
			if len(actualHash) > len(expectedGenesisHash) {
				actualHash = actualHash[:len(expectedGenesisHash)]
			}

			if actualHash != expectedGenesisHash {
				log.Warn().
					Int("index", i).
					Str("expected_genesis_hash", expectedGenesisHash).
					Str("actual_genesis_hash", genesisHash.String()).
					Msg("genesis hash mismatch, skipping")
				continue
			}
		}

		clients = append(clients, client)
		log.Debug().Int("index", i).Msg("RPC client added to pool")
	}

	if len(clients) == 0 {
		return nil, fmt.Errorf("failed to connect to any valid RPC endpoints")
	}

	return &RPCClient{
		clients: clients,
		logger:  log,
	}, nil
}

// executeWithFailover executes a function with round-robin failover
func (rc *RPCClient) executeWithFailover(ctx context.Context, operation string, fn func(*rpc.Client) error) error {
	rc.mu.RLock()
	clients := rc.clients
	rc.mu.RUnlock()

	if len(clients) == 0 {
		return fmt.Errorf("no RPC clients available for %s", operation)
	}

	maxAttempts := len(clients)
	// Snapshot start index once per call so concurrent callers can't share
	// counter advances and retry the same failing endpoint.
	startIndex := atomic.AddUint64(&rc.index, 1) - 1
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if ctx != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
		}

		client := clients[(startIndex+uint64(attempt))%uint64(len(clients))]

		if client == nil {
			continue
		}

		err := fn(client)
		if err == nil {
			return nil
		}
		lastErr = err

		rc.logger.Warn().
			Str("operation", operation).
			Int("attempt", attempt+1).
			Err(err).
			Msg("operation failed, trying next endpoint")
	}

	if lastErr != nil {
		return fmt.Errorf("operation %s failed after trying %d endpoints: %w", operation, maxAttempts, lastErr)
	}
	return fmt.Errorf("operation %s failed after trying %d endpoints", operation, maxAttempts)
}

// IsHealthy checks if any RPC in the pool is healthy by pinging it
func (rc *RPCClient) IsHealthy(ctx context.Context) bool {
	rc.mu.RLock()
	hasClients := len(rc.clients) > 0
	rc.mu.RUnlock()

	if !hasClients {
		return false
	}

	_, err := rc.GetLatestSlot(ctx)
	return err == nil
}

// GetLatestSlot returns the latest slot number
func (rc *RPCClient) GetLatestSlot(ctx context.Context) (uint64, error) {
	var slot uint64
	err := rc.executeWithFailover(ctx, "get_slot", func(client *rpc.Client) error {
		var innerErr error
		slot, innerErr = client.GetSlot(ctx, rpc.CommitmentFinalized)
		return innerErr
	})
	return slot, err
}

// GetRecentBlockhash gets a recent blockhash for transaction building
func (rc *RPCClient) GetRecentBlockhash(ctx context.Context) (solana.Hash, error) {
	var blockhash solana.Hash
	err := rc.executeWithFailover(ctx, "get_recent_blockhash", func(client *rpc.Client) error {
		resp, innerErr := client.GetLatestBlockhash(ctx, rpc.CommitmentFinalized)
		if innerErr != nil {
			return innerErr
		}
		blockhash = resp.Value.Blockhash
		return nil
	})
	return blockhash, err
}

// GetGasPrice fetches the current gas price (prioritization fee) from Solana
func (rc *RPCClient) GetGasPrice(ctx context.Context) (*big.Int, error) {
	// Use executeWithFailover to handle RPC calls with automatic failover
	type prioritizationFee struct {
		Slot              uint64
		PrioritizationFee uint64
	}
	var result []prioritizationFee

	err := rc.executeWithFailover(ctx, "get_gas_price", func(client *rpc.Client) error {
		fees, err := client.GetRecentPrioritizationFees(ctx, nil)
		if err != nil {
			return fmt.Errorf("failed to get recent prioritization fees: %w", err)
		}
		// Convert to our local type
		for _, fee := range fees {
			result = append(result, prioritizationFee{
				Slot:              fee.Slot,
				PrioritizationFee: fee.PrioritizationFee,
			})
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	if len(result) == 0 {
		// No recent fees, return a default minimum
		return big.NewInt(1000), nil // 1000 lamports per compute unit as default
	}

	// Collect all non-zero fees
	var fees []uint64
	for _, fee := range result {
		if fee.PrioritizationFee > 0 {
			fees = append(fees, fee.PrioritizationFee)
		}
	}

	// If no non-zero fees, use default
	if len(fees) == 0 {
		return big.NewInt(1000), nil
	}

	// Calculate median fee
	medianFee := calculateMedian(fees)
	return big.NewInt(int64(medianFee)), nil
}

// calculateMedian calculates the median of a slice of uint64 values
func calculateMedian(fees []uint64) uint64 {
	if len(fees) == 0 {
		return 0
	}

	// Sort the fees
	sort.Slice(fees, func(i, j int) bool {
		return fees[i] < fees[j]
	})

	// Calculate median
	n := len(fees)
	if n%2 == 0 {
		// Even number of elements, take average of middle two
		return (fees[n/2-1] + fees[n/2]) / 2
	}
	// Odd number of elements, take the middle one
	return fees[n/2]
}

// GetSignaturesForAddress gets transaction signatures for an address. If
// `before` is the zero signature, fetching starts from the most recent block;
// otherwise it returns signatures strictly older than `before`, enabling
// backward pagination.
func (rc *RPCClient) GetSignaturesForAddress(ctx context.Context, address solana.PublicKey, before solana.Signature) ([]*rpc.TransactionSignature, error) {
	var opts *rpc.GetSignaturesForAddressOpts
	if !before.IsZero() {
		opts = &rpc.GetSignaturesForAddressOpts{Before: before}
	}
	var signatures []*rpc.TransactionSignature
	err := rc.executeWithFailover(ctx, "get_signatures_for_address", func(client *rpc.Client) error {
		var innerErr error
		signatures, innerErr = client.GetSignaturesForAddressWithOpts(ctx, address, opts)
		return innerErr
	})
	return signatures, err
}

// GetTransaction gets a transaction by signature
func (rc *RPCClient) GetTransaction(ctx context.Context, signature solana.Signature) (*rpc.GetTransactionResult, error) {
	var tx *rpc.GetTransactionResult
	err := rc.executeWithFailover(ctx, "get_transaction", func(client *rpc.Client) error {
		var innerErr error
		maxVersion := uint64(0)
		tx, innerErr = client.GetTransaction(
			ctx,
			signature,
			&rpc.GetTransactionOpts{
				Encoding:                       solana.EncodingBase64,
				MaxSupportedTransactionVersion: &maxVersion,
			},
		)
		return innerErr
	})
	return tx, err
}

// WaitForSignatureConfirmation polls the cluster until the given signature reaches
// the requested commitment level, the on-chain status is an error, or the timeout
// elapses. Returns nil on success.
//
// Used by the ref-finalize flow to gate the second tx (finalize-by-reference) on
// the first tx (store_execute_ix_data) landing on-chain.
func (rc *RPCClient) WaitForSignatureConfirmation(
	ctx context.Context,
	sig solana.Signature,
	commitment rpc.ConfirmationStatusType,
	timeout time.Duration,
) error {
	deadline := time.Now().Add(timeout)
	backoff := 200 * time.Millisecond
	const maxBackoff = 1500 * time.Millisecond

	for {
		var result *rpc.GetSignatureStatusesResult
		execErr := rc.executeWithFailover(ctx, "get_signature_statuses", func(client *rpc.Client) error {
			var innerErr error
			result, innerErr = client.GetSignatureStatuses(ctx, false, sig)
			return innerErr
		})

		if execErr == nil && result != nil && len(result.Value) > 0 && result.Value[0] != nil {
			status := result.Value[0]
			if status.Err != nil {
				return fmt.Errorf("transaction %s failed on-chain: %v", sig.String(), status.Err)
			}
			if confirmationReached(status.ConfirmationStatus, commitment) {
				return nil
			}
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for signature %s to reach %s commitment", sig.String(), commitment)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		if backoff < maxBackoff {
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}

// confirmationReached reports whether `got` meets the bar set by `want`.
// Commitment ordering: processed < confirmed < finalized.
func confirmationReached(got, want rpc.ConfirmationStatusType) bool {
	rank := func(c rpc.ConfirmationStatusType) int {
		switch c {
		case rpc.ConfirmationStatusFinalized:
			return 3
		case rpc.ConfirmationStatusConfirmed:
			return 2
		case rpc.ConfirmationStatusProcessed:
			return 1
		default:
			return 0
		}
	}
	return rank(got) >= rank(want)
}

// BroadcastTransaction broadcasts a signed transaction and returns the transaction signature (hash)
func (rc *RPCClient) BroadcastTransaction(ctx context.Context, tx *solana.Transaction) (string, error) {
	if len(tx.Signatures) == 0 {
		return "", fmt.Errorf("transaction has no signatures")
	}
	txHash := tx.Signatures[0].String()

	err := rc.executeWithFailover(ctx, "send_transaction", func(client *rpc.Client) error {
		_, innerErr := client.SendTransaction(ctx, tx)
		return innerErr
	})
	return txHash, err
}

// SimulateTransaction runs a transaction against the current ledger state without broadcasting.
// Returns the simulation result (logs, error, compute units consumed).
// Skips signature verification so the TSS/relayer signatures don't need to be valid.
func (rc *RPCClient) SimulateTransaction(ctx context.Context, tx *solana.Transaction) (*rpc.SimulateTransactionResult, error) {
	var result *rpc.SimulateTransactionResponse
	err := rc.executeWithFailover(ctx, "simulate_transaction", func(client *rpc.Client) error {
		resp, innerErr := client.SimulateTransactionWithOpts(ctx, tx, &rpc.SimulateTransactionOpts{
			SigVerify: false,
		})
		if innerErr != nil {
			return innerErr
		}
		result = resp
		return nil
	})
	if err != nil {
		return nil, err
	}
	if result == nil || result.Value == nil {
		return nil, fmt.Errorf("empty simulation result")
	}
	return result.Value, nil
}

// GetAccountData fetches account data for a given public key
func (rc *RPCClient) GetAccountData(ctx context.Context, pubkey solana.PublicKey) ([]byte, error) {
	var accountData []byte
	err := rc.executeWithFailover(ctx, "get_account_data", func(client *rpc.Client) error {
		accountInfo, innerErr := client.GetAccountInfo(ctx, pubkey)
		if innerErr != nil {
			return innerErr
		}
		if accountInfo.Value == nil {
			return fmt.Errorf("account not found: %s", pubkey.String())
		}
		accountData = accountInfo.Value.Data.GetBinary()
		return nil
	})
	return accountData, err
}

// Close closes all RPC connections
func (rc *RPCClient) Close() {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	// Solana RPC clients don't have explicit Close, but we clear the slice
	rc.clients = nil
}
