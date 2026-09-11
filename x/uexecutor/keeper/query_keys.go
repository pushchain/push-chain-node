package keeper

import (
	"context"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/pushchain/push-chain-node/utils"
	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// maxQueryTxHashLen bounds the tx_hash accepted by the unauthenticated key
// derivation queries. Longest real value is an 88-char base58 Solana signature.
const maxQueryTxHashLen = 128

// InboundKeys derives the canonical UTX id and inbound ballot id for the given
// inbound, applying the same canonicalization the vote path uses. Lets off-chain
// validators read the keys from the chain instead of re-implementing the rules.
func (k Querier) InboundKeys(goCtx context.Context, req *types.QueryInboundKeysRequest) (*types.QueryInboundKeysResponse, error) {
	if req == nil || req.Inbound == nil {
		return nil, status.Error(codes.InvalidArgument, "inbound is required")
	}
	// This endpoint is unauthenticated, reads no state and so consumes no gas.
	// Bound the one field that drives a decode (tx_hash) rather than trusting
	// the caller. The limit is far above any real hash — 88 chars for a base58
	// Solana signature, 66 for 0x-prefixed EVM — so it rejects only garbage.
	// Deliberately not applied to raw_payload / verification_data, which are
	// legitimately long, nor pushed down into utils.Canonicalize*: the vote
	// path must stay lenient (a malformed inbound still has to produce a UTX),
	// and changing shared canonicalization would alter ballot keys.
	if n := len(req.Inbound.TxHash); n > maxQueryTxHashLen {
		return nil, status.Errorf(codes.InvalidArgument,
			"tx_hash too long: %d chars (max %d)", n, maxQueryTxHashLen)
	}

	inbound := *req.Inbound
	inbound.Canonicalize()

	ballotID, err := types.GetInboundBallotKey(inbound)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to derive inbound ballot key: %v", err)
	}

	return &types.QueryInboundKeysResponse{
		UtxId:            types.GetInboundUniversalTxKey(inbound),
		BallotId:         ballotID,
		CanonicalInbound: &inbound,
	}, nil
}

// OutboundBallotKey derives the canonical outbound ballot id for the given
// observation. The observed tx hash is canonicalized against the outbound's
// destination chain, which is looked up from the stored UTX/outbound so the
// caller can't supply the wrong chain.
func (k Querier) OutboundBallotKey(goCtx context.Context, req *types.QueryOutboundBallotKeyRequest) (*types.QueryOutboundBallotKeyResponse, error) {
	if req == nil || req.ObservedTx == nil {
		return nil, status.Error(codes.InvalidArgument, "observed_tx is required")
	}
	if strings.TrimSpace(req.UtxId) == "" || strings.TrimSpace(req.OutboundId) == "" {
		return nil, status.Error(codes.InvalidArgument, "utx_id and outbound_id are required")
	}

	ctx := sdk.UnwrapSDKContext(goCtx)
	// k.Keeper.GetUniversalTx (3 returns), not the shadowing Querier gRPC method.
	utx, found, err := k.Keeper.GetUniversalTx(ctx, req.UtxId)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to load universal tx: %v", err)
	}
	if !found {
		return nil, status.Errorf(codes.NotFound, "universal tx %s not found", req.UtxId)
	}

	var destChain string
	outboundFound := false
	for _, ob := range utx.OutboundTx {
		if ob.Id == req.OutboundId {
			destChain = ob.DestinationChain
			outboundFound = true
			break
		}
	}
	if !outboundFound {
		return nil, status.Errorf(codes.NotFound, "outbound %s not found in universal tx %s", req.OutboundId, req.UtxId)
	}

	// Mirror the canonicalization applied at vote ingress (msg_vote_outbound.go).
	obs := *req.ObservedTx
	obs.TxHash = utils.LenientCanonicalizeTxHash(destChain, obs.TxHash)
	obs.GasFeeUsed = strings.TrimSpace(obs.GasFeeUsed)
	obs.ErrorMsg = strings.TrimSpace(obs.ErrorMsg)

	ballotID, err := types.GetOutboundBallotKey(req.UtxId, req.OutboundId, obs)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to derive outbound ballot key: %v", err)
	}

	return &types.QueryOutboundBallotKeyResponse{
		BallotId:            ballotID,
		CanonicalObservedTx: &obs,
	}, nil
}
