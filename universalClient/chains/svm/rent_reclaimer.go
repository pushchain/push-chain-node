package svm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/rs/zerolog"
)

// RentReclaimer closes orphaned StoredIxData PDAs to recover rent (~0.002 SOL each).
//
//   - Orphan = PDA created by store_execute_ix_data whose finalize never succeeded
//     (so the program's auto-close path never ran).
//   - Skips PDAs younger than minAge to avoid racing in-flight finalize broadcasts.
type RentReclaimer struct {
	builder  *TxBuilder
	interval time.Duration
	minAge   time.Duration
	logger   zerolog.Logger
}

// Protocol byte widths (Solana / Anchor / Borsh).
const (
	anchorDiscriminatorSize = 8  // Anchor account prefix
	anchorBumpSize          = 1  // PDA bump
	pubkeyByteLen           = 32 // Ed25519 public key
	subTxIDByteLen          = 32 // sub_tx_id (content hash)
	borshVecLenPrefix       = 4  // Borsh Vec<T> length, u32 LE
)

// StoredIxData layout — must match the Rust struct in execute.rs:
//
//	disc(8) | bump(1) | sub_tx_id(32) | store_refund_recipient(32) | ix_data: Vec<u8>(4+N)
const (
	storedIxDataSubTxIDOffset         = anchorDiscriminatorSize + anchorBumpSize
	storedIxDataRefundRecipientOffset = storedIxDataSubTxIDOffset + subTxIDByteLen
	storedIxDataMinLen                = storedIxDataRefundRecipientOffset + pubkeyByteLen + borshVecLenPrefix
)

var storedIxDataAccountDiscriminator = func() []byte {
	h := sha256.Sum256([]byte("account:StoredIxData"))
	out := make([]byte, anchorDiscriminatorSize)
	copy(out, h[:anchorDiscriminatorSize])
	return out
}()

func NewRentReclaimer(builder *TxBuilder, interval, minAge time.Duration, logger zerolog.Logger) *RentReclaimer {
	return &RentReclaimer{
		builder:  builder,
		interval: interval,
		minAge:   minAge,
		logger:   logger.With().Str("component", "svm_rent_reclaimer").Logger(),
	}
}

func (r *RentReclaimer) Start(ctx context.Context) {
	go r.run(ctx)
}

func (r *RentReclaimer) run(ctx context.Context) {
	r.runOnce(ctx)

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.runOnce(ctx)
		}
	}
}

func (r *RentReclaimer) runOnce(ctx context.Context) {
	relayer, err := r.builder.loadRelayerKeypair()
	if err != nil {
		r.logger.Warn().Err(err).Msg("failed to load relayer keypair; skipping sweep")
		return
	}

	candidates, err := r.discoverOrphans(ctx, relayer.PublicKey())
	if err != nil {
		r.logger.Warn().Err(err).Msg("failed to discover orphan PDAs")
		return
	}
	if len(candidates) == 0 {
		r.logger.Debug().Msg("no orphan StoredIxData PDAs found")
		return
	}

	var closed, skipped, failed int
	for _, c := range candidates {
		if ctx.Err() != nil {
			return
		}
		old, err := r.isOldEnough(ctx, c.address)
		if err != nil || !old {
			skipped++
			continue
		}
		if err := r.closeOrphan(ctx, c, relayer); err != nil {
			r.logger.Warn().Err(err).Str("pda", c.address.String()).
				Msg("failed to close orphan PDA")
			failed++
			continue
		}
		closed++
	}
	r.logger.Info().
		Int("discovered", len(candidates)).
		Int("closed", closed).
		Int("skipped_young", skipped).
		Int("failed", failed).
		Msg("rent reclaim sweep complete")
}

type orphanPDA struct {
	address solana.PublicKey
	subTxID [subTxIDByteLen]byte
}

// discoverOrphans scans StoredIxData accounts owned by the gateway program
// where store_refund_recipient == our relayer. Finalized commitment naturally
// excludes very-recently-created PDAs.
func (r *RentReclaimer) discoverOrphans(ctx context.Context, relayer solana.PublicKey) ([]orphanPDA, error) {
	var result rpc.GetProgramAccountsResult
	err := r.builder.rpcClient.executeWithFailover(ctx, "get_program_accounts", func(client *rpc.Client) error {
		opts := &rpc.GetProgramAccountsOpts{
			Commitment: rpc.CommitmentFinalized,
			Filters: []rpc.RPCFilter{
				// match account type
				{Memcmp: &rpc.RPCFilterMemcmp{Offset: 0, Bytes: solana.Base58(storedIxDataAccountDiscriminator)}},
				// match refund recipient = us
				{Memcmp: &rpc.RPCFilterMemcmp{Offset: storedIxDataRefundRecipientOffset, Bytes: solana.Base58(relayer.Bytes())}},
			},
		}
		resp, innerErr := client.GetProgramAccountsWithOpts(ctx, r.builder.gatewayAddress, opts)
		if innerErr != nil {
			return innerErr
		}
		result = resp
		return nil
	})
	if err != nil {
		return nil, err
	}

	orphans := make([]orphanPDA, 0, len(result))
	for _, ka := range result {
		if ka == nil || ka.Account == nil {
			continue
		}
		data := ka.Account.Data.GetBinary()
		if len(data) < storedIxDataMinLen {
			continue
		}
		var subTxID [subTxIDByteLen]byte
		copy(subTxID[:], data[storedIxDataSubTxIDOffset:storedIxDataSubTxIDOffset+subTxIDByteLen])
		orphans = append(orphans, orphanPDA{address: ka.Pubkey, subTxID: subTxID})
	}
	return orphans, nil
}

// getSignaturesForAddress page size when probing PDA age — we only need the
// most recent signature to bound age from below.
const signatureAgeProbeLimit = 1

// Default lifecycle params, well above the broadcaster's retry window.
const (
	rentReclaimSweepInterval = 30 * time.Minute
	rentReclaimMinPDAAge     = 10 * time.Minute

	// Floor for the configured minPDAAge. Anything shorter risks racing an
	// in-flight finalize that hasn't landed yet.
	rentReclaimMinPDAAgeFloor = 1 * time.Minute
)

// isOldEnough reports whether the most recent tx touching addr is at least
// minAge old. For StoredIxData PDAs, that's effectively the PDA's age (they
// only ever see one tx — their creating store_execute_ix_data).
func (r *RentReclaimer) isOldEnough(ctx context.Context, addr solana.PublicKey) (bool, error) {
	limit := signatureAgeProbeLimit
	var sigs []*rpc.TransactionSignature
	err := r.builder.rpcClient.executeWithFailover(ctx, "get_signatures_for_address", func(client *rpc.Client) error {
		resp, innerErr := client.GetSignaturesForAddressWithOpts(ctx, addr, &rpc.GetSignaturesForAddressOpts{
			Limit: &limit,
		})
		if innerErr != nil {
			return innerErr
		}
		sigs = resp
		return nil
	})
	if err != nil || len(sigs) == 0 {
		return false, err
	}
	if sigs[0].BlockTime == nil {
		return false, nil
	}
	age := time.Since(time.Unix(int64(*sigs[0].BlockTime), 0))
	return age >= r.minAge, nil
}

// closeOrphan builds and broadcasts an arg-free close_stored_ix_data tx.
func (r *RentReclaimer) closeOrphan(ctx context.Context, o orphanPDA, relayer solana.PrivateKey) error {
	executedSubTxPDA, _, err := solana.FindProgramAddress(
		[][]byte{executedSubTxSeed, o.subTxID[:]},
		r.builder.gatewayAddress,
	)
	if err != nil {
		return fmt.Errorf("derive executed_sub_tx PDA: %w", err)
	}

	accounts := r.builder.buildCloseStoredIxDataAccounts(relayer.PublicKey(), o.address, executedSubTxPDA)
	closeIx := solana.NewInstruction(r.builder.gatewayAddress, accounts, discCloseStoredIxData[:])

	blockhash, err := r.builder.rpcClient.GetRecentBlockhash(ctx)
	if err != nil {
		return fmt.Errorf("get blockhash: %w", err)
	}

	tx, err := solana.NewTransaction(
		[]solana.Instruction{closeIx},
		blockhash,
		solana.TransactionPayer(relayer.PublicKey()),
	)
	if err != nil {
		return fmt.Errorf("build close tx: %w", err)
	}
	if _, err := tx.Sign(func(key solana.PublicKey) *solana.PrivateKey {
		if key.Equals(relayer.PublicKey()) {
			priv := relayer
			return &priv
		}
		return nil
	}); err != nil {
		return fmt.Errorf("sign close tx: %w", err)
	}

	hash, err := r.builder.rpcClient.BroadcastTransaction(ctx, tx)
	if err != nil {
		return fmt.Errorf("broadcast close tx: %w", err)
	}
	r.logger.Info().
		Str("pda", o.address.String()).
		Str("close_tx_hash", hash).
		Str("sub_tx_id", hex.EncodeToString(o.subTxID[:])).
		Msg("orphan StoredIxData PDA closed, rent reclaimed")
	return nil
}
