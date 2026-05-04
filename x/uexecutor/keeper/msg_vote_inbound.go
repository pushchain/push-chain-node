package keeper

import (
	"context"
	"fmt"

	errors "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// VoteInbound is for uvalidators for voting on synthetic asset inbound bridging.
// After ballot finalization, a UniversalTx is always created on-chain regardless of
// whether the inbound passes execution validation. This ensures the user can always
// query what happened to their cross-chain tx instead of having funds silently stuck
// in the gateway contract.
func (k Keeper) VoteInbound(ctx context.Context, universalValidator sdk.ValAddress, inbound types.Inbound) error {
	k.Logger().Info("vote inbound received",
		"validator", universalValidator.String(),
		"source_chain", inbound.SourceChain,
		"tx_hash", inbound.TxHash,
		"tx_type", inbound.TxType.String(),
		"sender", inbound.Sender,
	)

	// Check inbound enabled before any state changes
	enabled, err := k.uregistryKeeper.IsChainInboundEnabled(ctx, inbound.SourceChain)
	if err != nil {
		return errors.Wrap(err, "failed to check inbound enabled")
	}
	if !enabled {
		k.Logger().Warn("vote inbound rejected: chain inbound disabled", "source_chain", inbound.SourceChain)
		return fmt.Errorf("inbound is disabled for chain %s", inbound.SourceChain)
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Step 1: Derive UTX key from the original inbound data (source_chain:tx_hash:log_index)
	universalTxKey := types.GetInboundUniversalTxKey(inbound)
	found, err := k.HasUniversalTx(ctx, universalTxKey)
	if err != nil {
		return errors.Wrap(err, "failed to check UniversalTx")
	}
	if found {
		k.Logger().Warn("vote inbound rejected: utx already exists", "utx_key", universalTxKey)
		return fmt.Errorf("universal tx with key %s already exists", universalTxKey)
	}

	// use a temporary context to not commit any ballot state change in case of error
	tmpCtx, commit := sdkCtx.CacheContext()

	// Step 2: Record this validator's vote in the per-utx PendingInbounds entry
	// (variant-aware audit trail). Each unique Inbound payload becomes its own
	// variant; multiple variants per utx_key indicate validator divergence.
	ballotKey, err := types.GetInboundBallotKey(inbound)
	if err != nil {
		return errors.Wrap(err, "failed to derive inbound ballot key")
	}
	if err := k.RecordInboundVote(tmpCtx, inbound, universalValidator.String(), ballotKey); err != nil {
		return err
	}

	// Step 3: Vote on inbound ballot (uses the original inbound data as-is for the ballot key,
	// so UVs that observe different field data will correctly produce different votes)
	isFinalized, _, err := k.VoteOnInboundBallot(tmpCtx, universalValidator, inbound)
	if err != nil {
		return errors.Wrap(err, "failed to vote on inbound ballot")
	}

	commit()

	// Voting not finalized yet
	if !isFinalized {
		k.Logger().Debug("vote inbound recorded, ballot not yet finalized",
			"validator", universalValidator.String(),
			"utx_key", universalTxKey,
		)
		return nil
	}

	// --- Ballot finalized: always create UTX from here on ---
	k.Logger().Info("inbound ballot finalized, creating utx", "utx_key", universalTxKey, "source_chain", inbound.SourceChain)

	// Normalize inbound after finalization: strip irrelevant fields, decode raw_payload.
	// If normalization/decode fails, create UTX with failed PCTx + revert.
	if normalizeErr := inbound.NormalizeForTxType(); normalizeErr != nil {
		k.Logger().Warn("inbound normalization failed after ballot finalization",
			"utx_key", universalTxKey,
			"error", normalizeErr.Error(),
		)
		utx := types.UniversalTx{Id: universalTxKey, InboundTx: &inbound}
		if createErr := k.CreateUniversalTx(ctx, universalTxKey, utx); createErr != nil {
			return createErr
		}
		if removeErr := k.RemovePendingInbound(ctx, inbound); removeErr != nil {
			return removeErr
		}
		if handleErr := k.handleFailedInboundValidation(sdkCtx, utx, normalizeErr); handleErr != nil {
			return handleErr
		}
		return nil
	}

	utx := types.UniversalTx{
		Id:         universalTxKey,
		InboundTx:  &inbound,
		PcTx:       nil,
		OutboundTx: nil,
	}

	// Step 5: Create the UniversalTx — this must succeed for any further processing
	if err := k.CreateUniversalTx(ctx, universalTxKey, utx); err != nil {
		return err
	}

	k.Logger().Info("utx created",
		"utx_key", universalTxKey,
		"source_chain", inbound.SourceChain,
		"tx_type", inbound.TxType.String(),
		"amount", inbound.Amount,
	)

	// Step 6: Remove from pending inbound set
	if err := k.RemovePendingInbound(ctx, inbound); err != nil {
		return err
	}

	// Step 7: Validate execution prerequisites.
	// If validation fails, record a failed PCTx and schedule revert (for non-isCEA)
	// instead of failing the vote — so the UTX is always visible on-chain.
	if validationErr := inbound.ValidateForExecution(); validationErr != nil {
		k.Logger().Warn("inbound validation failed, scheduling revert",
			"utx_key", universalTxKey,
			"error", validationErr.Error(),
			"is_cea", inbound.IsCEA,
		)
		if handleErr := k.handleFailedInboundValidation(sdkCtx, utx, validationErr); handleErr != nil {
			return handleErr
		}
		return nil
	}

	// Step 8: Execute the inbound
	k.Logger().Info("dispatching inbound execution",
		"utx_key", universalTxKey,
		"tx_type", inbound.TxType.String(),
	)
	if err := k.ExecuteInbound(ctx, utx); err != nil {
		return err
	}

	return nil
}
