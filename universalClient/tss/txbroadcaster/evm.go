package txbroadcaster

import (
	"context"
	"math/big"

	"github.com/pushchain/push-chain-node/universalClient/chains/common"
	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/pushchain/push-chain-node/universalClient/tss/coordinator"
	"github.com/pushchain/push-chain-node/universalClient/tss/txflow"
)

// broadcastOutboundEVM broadcasts a signed EVM outbound transaction.
//
// All validators produce the same signed tx (deterministic TSS output), so the
// tx hash is known before broadcasting (computed from the assembled signed tx).
//
// Flow:
//  1. Build and broadcast the signed tx (tx hash is always returned, even on error)
//  2. Success → BROADCASTED with tx hash
//  3. Error: tx already on chain (mined by another node, or "already known") → BROADCASTED
//  4. Error otherwise: check finalized nonce on chain:
//     - nonce consumed → BROADCASTED with tx hash (resolver will REVERT)
//     - nonce NOT consumed → keep SIGNED, retry next tick
func (b *Broadcaster) broadcastOutboundEVM(ctx context.Context, event *store.Event, data *txflow.SignedOutboundData, chainID string) {
	log := b.logger.With().Str("event_id", event.EventID).Str("chain", chainID).Logger()

	client, err := b.chains.GetClient(chainID)
	if err != nil {
		log.Warn().Err(err).Msg("failed to get chain client")
		return
	}
	builder, err := client.GetTxBuilder()
	if err != nil {
		log.Warn().Err(err).Msg("failed to get tx builder")
		return
	}

	signingReq, signature, err := txflow.DecodeSigningData(data.SigningData)
	if err != nil {
		log.Warn().Err(err).Msg("failed to decode signing data")
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
		log.Warn().Err(broadcastErr).Msg("failed to assemble tx, will retry next tick")
		return
	}

	// First: is the tx already mined on chain (e.g., another node broadcast it)?
	// "already known" RPC errors fall into this bucket — the broadcast effectively
	// succeeded, and once the tx mines we can promote without waiting for the
	// nonce check.
	if found, _, _, _, vErr := builder.VerifyBroadcastedTx(ctx, txHash); vErr == nil && found {
		log.Debug().Err(broadcastErr).Str("tx_hash", txHash).
			Msg("broadcast errored but tx is on chain, marking BROADCASTED")
		b.markBroadcasted(event, chainID, txHash)
		return
	}

	tssAddress := ""
	if b.getTSSAddress != nil {
		var addrErr error
		tssAddress, addrErr = b.getTSSAddress(ctx)
		if addrErr != nil {
			log.Warn().Err(addrErr).Msg("failed to get TSS address for nonce check, will retry next tick")
			return
		}
	}

	b.checkNonceAndMarkBroadcasted(ctx, event, builder, chainID, txHash, tssAddress, data.SigningData.Nonce, broadcastErr)
}

// broadcastFundMigrationEVM broadcasts a signed EVM fund migration transaction.
// Same nonce-consumed recovery pattern as outbound, but uses old TSS address for nonce check.
func (b *Broadcaster) broadcastFundMigrationEVM(ctx context.Context, event *store.Event, data *txflow.SignedFundMigrationData, chainID string) {
	log := b.logger.With().Str("event_id", event.EventID).Str("chain", chainID).Logger()

	oldTSSAddr, err := coordinator.DeriveEVMAddressFromPubkey(data.OldTssPubkey)
	if err != nil {
		log.Warn().Err(err).Msg("failed to derive old TSS address")
		return
	}
	currentTSSAddr, err := coordinator.DeriveEVMAddressFromPubkey(data.CurrentTssPubkey)
	if err != nil {
		log.Warn().Err(err).Msg("failed to derive new TSS address")
		return
	}

	client, err := b.chains.GetClient(chainID)
	if err != nil {
		log.Warn().Err(err).Msg("failed to get chain client")
		return
	}
	builder, err := client.GetTxBuilder()
	if err != nil {
		log.Warn().Err(err).Msg("failed to get tx builder")
		return
	}

	signingReq, signature, err := txflow.DecodeSigningData(data.SigningData)
	if err != nil {
		log.Warn().Err(err).Msg("failed to decode signing data")
		return
	}

	gasPrice := new(big.Int)
	gasPrice.SetString(data.GasPrice, 10)

	l1GasFee := new(big.Int)
	l1GasFee.SetString(data.L1GasFee, 10)

	migrationData := &common.FundMigrationData{
		From:     oldTSSAddr,
		To:       currentTSSAddr,
		GasPrice: gasPrice,
		GasLimit: data.GasLimit,
		L1GasFee: l1GasFee,
	}

	txHash, broadcastErr := builder.BroadcastFundMigrationTx(ctx, signingReq, migrationData, signature)

	if broadcastErr == nil {
		b.markBroadcasted(event, chainID, txHash)
		return
	}

	if txHash == "" {
		log.Warn().Err(broadcastErr).Msg("failed to assemble fund migration tx, will retry next tick")
		return
	}

	if found, _, _, _, vErr := builder.VerifyBroadcastedTx(ctx, txHash); vErr == nil && found {
		log.Debug().Err(broadcastErr).Str("tx_hash", txHash).
			Msg("fund migration broadcast errored but tx is on chain, marking BROADCASTED")
		b.markBroadcasted(event, chainID, txHash)
		return
	}

	// Use old TSS address for nonce check since that's the sender
	b.checkNonceAndMarkBroadcasted(ctx, event, builder, chainID, txHash, oldTSSAddr, data.SigningData.Nonce, broadcastErr)
}

// checkNonceAndMarkBroadcasted checks if a nonce has been consumed on-chain
// despite a broadcast error. If consumed, the tx landed (possibly via another
// node) and we mark BROADCASTED. Otherwise keep SIGNED for retry.
func (b *Broadcaster) checkNonceAndMarkBroadcasted(
	ctx context.Context,
	event *store.Event,
	builder common.TxBuilder,
	chainID, txHash, signerAddr string,
	eventNonce uint64,
	broadcastErr error,
) {
	log := b.logger.With().Str("event_id", event.EventID).Str("chain", chainID).Logger()

	verdict, finalizedNonce := txflow.CheckNonce(ctx, builder, signerAddr, eventNonce)
	if verdict == txflow.NonceConsumed {
		log.Debug().Err(broadcastErr).Str("tx_hash", txHash).
			Uint64("event_nonce", eventNonce).Uint64("finalized_nonce", finalizedNonce).
			Msg("broadcast failed but tx already on chain, marking BROADCASTED")
		b.markBroadcasted(event, chainID, txHash)
		return
	}

	log.Debug().Err(broadcastErr).Msg("broadcast failed, will retry next tick")
}
