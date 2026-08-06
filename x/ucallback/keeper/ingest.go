package keeper

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"

	"github.com/pushchain/push-chain-node/x/ucallback/types"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
)

// IngestReadRequests records a UniversalRead for every ReadRequested event in the
// receipt.
//
// The two-part filter — log.Address must be UniversalCallback, topic0 must be
// ReadRequested — is what makes the decoded event trustworthy. Any contract can
// emit a log with the same topic0; only the system contract's address makes it
// ours. Dropping the address check would let anyone mint read requests.
func (k Keeper) IngestReadRequests(ctx context.Context, receipt *evmtypes.MsgEthereumTxResponse) error {
	if receipt == nil || len(receipt.Logs) == 0 {
		return nil
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	callbackAddr := strings.ToLower(uregistrytypes.SYSTEM_CONTRACTS["UNIVERSAL_CALLBACK"].Address)

	for _, lg := range receipt.Logs {
		if lg.Removed {
			continue
		}
		if strings.ToLower(lg.Address) != callbackAddr {
			continue
		}
		if len(lg.Topics) == 0 ||
			!strings.EqualFold(lg.Topics[0], types.ReadRequestedEventSig.Hex()) {
			continue
		}

		event, err := types.DecodeReadRequestedFromLog(lg)
		if err != nil {
			return fmt.Errorf("failed to decode ReadRequested (tx %s log %d): %w",
				receipt.Hash, lg.Index, err)
		}

		if err := k.recordReadRequest(ctx, sdkCtx, event, receipt.Hash, lg.Index); err != nil {
			return err
		}
	}

	return nil
}

// recordReadRequest writes one decoded event as a PENDING UniversalRead.
func (k Keeper) recordReadRequest(
	ctx context.Context,
	sdkCtx sdk.Context,
	event *types.ReadRequestedEvent,
	txHash string,
	logIndex uint64,
) error {
	// requestId is derived on-chain from an incrementing nonce, so a repeat means
	// the same log was replayed rather than a genuine second request. Keep the
	// first record: it is the one validators may already be working from.
	if k.HasUniversalRead(ctx, event.RequestID) {
		k.Logger().Debug("read request already recorded, skipping",
			"request_id", event.RequestID, "tx_hash", txHash)
		return nil
	}

	ur := types.UniversalRead{
		Id:     event.RequestID,
		Status: types.UniversalReadStatus_UNIVERSAL_READ_STATUS_PENDING,
		Request: &types.ReadRequest{
			RequestId:              event.RequestID,
			DestinationChain:       event.DestinationChain(),
			Owner:                  event.Owner,
			Query:                  event.Query,
			MinConfirmations:       uint32(event.MinConfirmations),
			DestinationBlockHeight: event.BlockNumber,
			ExpiryBlockHeight:      event.ExpiryPushChainHeight,
			// Not carried by the event — the height at which we observed it is the
			// only honest answer, and it is what expiry is measured against.
			CreatedAtHeight:   uint64(sdkCtx.BlockHeight()),
			CallbackTarget:    event.CallbackTarget,
			OriginalFunder:    event.OriginalFunder,
			FeesDeposited:     bigOrZero(event.FeesDeposited),
			MaxFee:            bigOrZero(event.MaxFee),
			RequestedTxHash:   txHash,
			RequestedLogIndex: logIndex,
		},
	}

	if err := k.SetUniversalRead(ctx, ur); err != nil {
		return fmt.Errorf("failed to record read request %s: %w", event.RequestID, err)
	}

	k.Logger().Info("read request recorded",
		"request_id", event.RequestID,
		"destination_chain", ur.Request.DestinationChain,
		"expiry_height", ur.Request.ExpiryBlockHeight,
		"tx_hash", txHash,
		"log_index", logIndex,
	)

	return nil
}

// bigOrZero renders a *big.Int as a decimal string, tolerating nil. The proto
// carries these as strings because they are uint256 values that do not fit any
// protobuf integer type.
//
// Takes *big.Int rather than a String()-bearing interface on purpose: a nil
// *big.Int boxed into an interface is not itself nil, so the guard would miss it
// and String() would be called on a nil receiver.
func bigOrZero(v *big.Int) string {
	if v == nil {
		return "0"
	}
	return v.String()
}
