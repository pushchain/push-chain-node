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
	// Check inbound enabled before any state changes
	enabled, err := k.uregistryKeeper.IsChainInboundEnabled(ctx, inbound.SourceChain)
	if err != nil {
		return errors.Wrap(err, "failed to check inbound enabled")
	}
	if !enabled {
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
		return fmt.Errorf("universal tx with key %s already exists", universalTxKey)
	}

	// use a temporary context to not commit any ballot state change in case of error
	tmpCtx, commit := sdkCtx.CacheContext()

	// Step 2: Add inbound synthetic to pending set - adds if not present, else does nothing
	if err := k.AddPendingInbound(tmpCtx, inbound); err != nil {
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
		return nil
	}

	// --- Ballot finalized: always create UTX from here on ---

	// Normalize inbound after finalization: strip fields irrelevant for this TxType
	// before persisting, so the stored UTX only contains fields used during execution.
	inbound.NormalizeForTxType()

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

	// Step 6: Remove from pending inbound set
	if err := k.RemovePendingInbound(ctx, inbound); err != nil {
		return err
	}

	// Step 7: Validate execution prerequisites.
	// If validation fails, record a failed PCTx and schedule revert (for non-isCEA)
	// instead of failing the vote — so the UTX is always visible on-chain.
	if validationErr := inbound.ValidateForExecution(); validationErr != nil {
		k.handleFailedInboundValidation(sdkCtx, utx, validationErr)
		return nil
	}

	// Step 8: Execute the inbound
	if err := k.ExecuteInbound(ctx, utx); err != nil {
		return err
	}

	return nil
}
