package txbroadcaster

import (
	"context"
	"math/big"

	"github.com/pushchain/push-chain-node/universalClient/chains/common"
	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/pushchain/push-chain-node/universalClient/tss/coordinator"
)

// broadcastEVM broadcasts a signed EVM outbound transaction.
//
// All validators produce the same signed tx (deterministic TSS output), so the
// tx hash is known before broadcasting (computed from the assembled signed tx).
//
// Flow:
//  1. Build and broadcast the signed tx (tx hash is always returned, even on error)
//  2. Success → BROADCASTED with tx hash
//  3. Error → check finalized nonce on chain:
//     - nonce consumed (tx landed) → BROADCASTED with tx hash
//     - nonce NOT consumed → keep SIGNED, retry next tick
func (b *Broadcaster) broadcastEVM(ctx context.Context, event *store.Event, data *SignedOutboundData, chainID string) {
	client, err := b.chains.GetClient(chainID)
	if err != nil {
		b.logger.Warn().Err(err).Str("event_id", event.EventID).Msg("failed to get chain client")
		return
	}
	builder, err := client.GetTxBuilder()
	if err != nil {
		b.logger.Warn().Err(err).Str("event_id", event.EventID).Msg("failed to get tx builder")
		return
	}

	signingReq, signature, err := decodeSigningData(data.SigningData)
	if err != nil {
		b.logger.Warn().Err(err).Str("event_id", event.EventID).Msg("failed to decode signing data")
		return
	}

	// Broadcast — tx hash is computed before sending, so it's returned even on RPC error
	outboundData := data.OutboundCreatedEvent
	txHash, broadcastErr := builder.BroadcastOutboundSigningRequest(ctx, signingReq, &outboundData, signature)

	if broadcastErr == nil {
		b.markBroadcasted(event, chainID, txHash)
		return
	}

	// Broadcast failed — check if the tx landed on chain anyway (another node, or "already known")
	if txHash == "" {
		b.logger.Warn().Err(broadcastErr).Str("event_id", event.EventID).Str("chain", chainID).
			Msg("failed to assemble tx, will retry next tick")
		return
	}

	tssAddress := ""
	if b.getTSSAddress != nil {
		var addrErr error
		tssAddress, addrErr = b.getTSSAddress(ctx)
		if addrErr != nil {
			b.logger.Warn().Err(addrErr).Str("event_id", event.EventID).
				Msg("failed to get TSS address for nonce check, will retry next tick")
			return
		}
	}

	b.checkNonceAndMarkBroadcasted(ctx, event, builder, chainID, txHash, tssAddress, data.SigningData.Nonce, broadcastErr)
}

// broadcastFundMigrationEVM broadcasts a signed EVM fund migration transaction.
// Same nonce-consumed recovery pattern as outbound, but uses old TSS address for nonce check.
func (b *Broadcaster) broadcastFundMigrationEVM(ctx context.Context, event *store.Event, data *SignedFundMigrationData, chainID string) {
	oldAddr, err := coordinator.DeriveEVMAddressFromPubkey(data.OldTssPubkey)
	if err != nil {
		b.logger.Warn().Err(err).Str("event_id", event.EventID).Msg("failed to derive old TSS address")
		return
	}
	newAddr, err := coordinator.DeriveEVMAddressFromPubkey(data.CurrentTssPubkey)
	if err != nil {
		b.logger.Warn().Err(err).Str("event_id", event.EventID).Msg("failed to derive new TSS address")
		return
	}

	client, err := b.chains.GetClient(chainID)
	if err != nil {
		b.logger.Warn().Err(err).Str("event_id", event.EventID).Msg("failed to get chain client")
		return
	}
	builder, err := client.GetTxBuilder()
	if err != nil {
		b.logger.Warn().Err(err).Str("event_id", event.EventID).Msg("failed to get tx builder")
		return
	}

	signingReq, signature, err := decodeSigningData(data.SigningData)
	if err != nil {
		b.logger.Warn().Err(err).Str("event_id", event.EventID).Msg("failed to decode signing data")
		return
	}

	gasPrice := new(big.Int)
	gasPrice.SetString(data.GasPrice, 10)

	migrationData := &common.FundMigrationData{
		From:     oldAddr,
		To:       newAddr,
		GasPrice: gasPrice,
		GasLimit: data.GasLimit,
	}

	txHash, broadcastErr := builder.BroadcastFundMigrationTx(ctx, signingReq, migrationData, signature)

	if broadcastErr == nil {
		b.markBroadcasted(event, chainID, txHash)
		return
	}

	if txHash == "" {
		b.logger.Warn().Err(broadcastErr).Str("event_id", event.EventID).Str("chain", chainID).
			Msg("failed to assemble fund migration tx, will retry next tick")
		return
	}

	// Use old TSS address for nonce check since that's the sender
	b.checkNonceAndMarkBroadcasted(ctx, event, builder, chainID, txHash, oldAddr, data.SigningData.Nonce, broadcastErr)
}

// checkNonceAndMarkBroadcasted checks if a nonce has been consumed on-chain despite broadcast error.
// If consumed, the tx landed and we mark BROADCASTED. Otherwise keep SIGNED for retry.
func (b *Broadcaster) checkNonceAndMarkBroadcasted(
	ctx context.Context,
	event *store.Event,
	builder common.TxBuilder,
	chainID, txHash, signerAddr string,
	eventNonce uint64,
	broadcastErr error,
) {
	finalizedNonce, err := builder.GetNextNonce(ctx, signerAddr, true)
	if err == nil && eventNonce < finalizedNonce {
		// Nonce consumed — tx is on chain. Mark BROADCASTED so the resolver can verify it.
		b.logger.Info().Err(broadcastErr).Str("event_id", event.EventID).Str("chain", chainID).
			Str("tx_hash", txHash).
			Uint64("event_nonce", eventNonce).Uint64("finalized_nonce", finalizedNonce).
			Msg("broadcast failed but tx already on chain, marking BROADCASTED")
		b.markBroadcasted(event, chainID, txHash)
		return
	}

	// Nonce not consumed — transient error (RPC down, gas issues, etc.).
	// Keep as SIGNED and retry next tick.
	b.logger.Debug().Err(broadcastErr).Str("event_id", event.EventID).Str("chain", chainID).
		Msg("broadcast failed, will retry next tick")
}
