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
	txPollInterval     = 500 * time.Millisecond
	txConfirmTimeout   = 15 * time.Second
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

	// Poll until the tx is confirmed on chain
	if err := waitForTxConfirmation(voteCtx, signer.pushCore, txResp.TxHash); err != nil {
		log.Error().Str("msg_type", msgType).Str("tx_hash", txResp.TxHash).Err(err).Msg("tx not confirmed on chain")
		return "", fmt.Errorf("tx broadcast but not confirmed: %w", err)
	}

	log.Debug().Str("msg_type", msgType).Str("tx_hash", txResp.TxHash).Msg("vote confirmed on chain")
	return txResp.TxHash, nil
}

// waitForTxConfirmation polls the chain until the tx is found or the context expires.
// It only confirms the tx was included in a block — it does not check the execution result code.
// A tx with code != 0 is still "confirmed" (it landed on chain, the module just rejected the msg).
func waitForTxConfirmation(ctx context.Context, client chainClient, txHash string) error {
	ticker := time.NewTicker(txPollInterval)
	defer ticker.Stop()

	timeout := time.After(txConfirmTimeout)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout:
			return fmt.Errorf("timeout waiting for tx %s after %s", txHash, txConfirmTimeout)
		case <-ticker.C:
			resp, err := client.GetTx(ctx, txHash)
			if err != nil {
				// tx not found yet, keep polling
				continue
			}
			if resp != nil && resp.TxResponse != nil {
				return nil // tx is on chain
			}
		}
	}
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

// voteChainMeta votes on chain metadata (gas price, block height)
func voteChainMeta(
	ctx context.Context,
	signer *Signer,
	log zerolog.Logger,
	granter string,
	chainID string,
	price uint64,
	chainHeight uint64,
) (string, error) {
	msg := &uexecutortypes.MsgVoteChainMeta{
		Signer:          granter,
		ObservedChainId: chainID,
		Price:           price,
		ChainHeight:     chainHeight,
	}
	memo := fmt.Sprintf("Vote chain meta: %s @ price=%d height=%d", chainID, price, chainHeight)
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

// voteFundMigration votes on a fund migration result
func voteFundMigration(
	ctx context.Context,
	signer *Signer,
	log zerolog.Logger,
	granter string,
	migrationID uint64,
	txHash string,
	success bool,
) (string, error) {
	msg := &utsstypes.MsgVoteFundMigration{
		Signer:      granter,
		MigrationId: migrationID,
		TxHash:      txHash,
		Success:     success,
	}
	memo := fmt.Sprintf("Vote fund migration: %d", migrationID)
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
