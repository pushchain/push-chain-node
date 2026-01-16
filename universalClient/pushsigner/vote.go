package pushsigner

import (
	"context"
	"fmt"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/rs/zerolog"

	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	utsstypes "github.com/pushchain/push-chain-node/x/utss/types"
)

const (
	defaultGasLimit    = uint64(500000000)
	defaultFeeAmount   = "500000000000000upc"
	defaultVoteTimeout = 30 * time.Second
)

// vote broadcasts a vote transaction with the given message
func vote(
	ctx context.Context,
	signer *Signer,
	log zerolog.Logger,
	msg sdk.Msg,
	memo string,
) (string, error) {
	feeAmount, err := sdk.ParseCoinsNormalized(defaultFeeAmount)
	if err != nil {
		return "", fmt.Errorf("failed to parse fee amount: %w", err)
	}

	if memo == "" {
		memo = fmt.Sprintf("Vote: %s", sdk.MsgTypeURL(msg))
	}

	msgType := sdk.MsgTypeURL(msg)
	log.Debug().
		Str("msg_type", msgType).
		Str("memo", memo).
		Msg("broadcasting vote transaction")

	voteCtx, cancel := context.WithTimeout(ctx, defaultVoteTimeout)
	defer cancel()

	txResp, err := signer.signAndBroadcastAuthZTx(
		voteCtx,
		[]sdk.Msg{msg},
		memo,
		defaultGasLimit,
		feeAmount,
	)
	if err != nil {
		log.Error().Str("msg_type", msgType).Err(err).Msg("failed to broadcast vote")
		return "", fmt.Errorf("failed to broadcast vote: %w", err)
	}

	if txResp.Code != 0 {
		log.Error().
			Str("msg_type", msgType).
			Str("tx_hash", txResp.TxHash).
			Uint32("code", txResp.Code).
			Str("raw_log", txResp.RawLog).
			Msg("vote rejected")
		return "", fmt.Errorf("vote failed with code %d: %s", txResp.Code, txResp.RawLog)
	}

	log.Info().Str("msg_type", msgType).Str("tx_hash", txResp.TxHash).Msg("vote successful")
	return txResp.TxHash, nil
}

// voteInbound votes on an inbound transaction
func voteInbound(
	ctx context.Context,
	signer *Signer,
	log zerolog.Logger,
	granter string,
	inbound *uexecutortypes.Inbound,
) (string, error) {
	msg := &uexecutortypes.MsgVoteInbound{
		Signer:  granter,
		Inbound: inbound,
	}
	memo := fmt.Sprintf("Vote inbound: %s", inbound.TxHash)
	return vote(ctx, signer, log, msg, memo)
}

// voteGasPrice votes on a gas price observation
func voteGasPrice(
	ctx context.Context,
	signer *Signer,
	log zerolog.Logger,
	granter string,
	chainID string,
	price uint64,
	blockNumber uint64,
) (string, error) {
	msg := &uexecutortypes.MsgVoteGasPrice{
		Signer:          granter,
		ObservedChainId: chainID,
		Price:           price,
		BlockNumber:     blockNumber,
	}
	memo := fmt.Sprintf("Vote gas price: %s @ %d", chainID, price)
	return vote(ctx, signer, log, msg, memo)
}

// voteOutbound votes on an outbound transaction observation
func voteOutbound(
	ctx context.Context,
	signer *Signer,
	log zerolog.Logger,
	granter string,
	txID string,
	utxID string,
	observation *uexecutortypes.OutboundObservation,
) (string, error) {
	msg := &uexecutortypes.MsgVoteOutbound{
		Signer:     granter,
		TxId:       txID,
		UtxId:      utxID,
		ObservedTx: observation,
	}
	memo := fmt.Sprintf("Vote outbound: %s", txID)
	return vote(ctx, signer, log, msg, memo)
}

// voteTssKeyProcess votes on a TSS key process
func voteTssKeyProcess(
	ctx context.Context,
	signer *Signer,
	log zerolog.Logger,
	granter string,
	tssPubKey string,
	keyID string,
	processID uint64,
) (string, error) {
	msg := &utsstypes.MsgVoteTssKeyProcess{
		Signer:    granter,
		TssPubkey: tssPubKey,
		KeyId:     keyID,
		ProcessId: processID,
	}
	memo := fmt.Sprintf("Vote TSS key: %s", keyID)
	return vote(ctx, signer, log, msg, memo)
}
