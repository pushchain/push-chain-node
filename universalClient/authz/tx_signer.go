package authz

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	"github.com/pushchain/push-chain-node/universalClient/constant"
	"github.com/pushchain/push-chain-node/universalClient/keys"
	"github.com/rs/zerolog"
)

// TxSigner handles AuthZ transaction signing for Universal Validator
type TxSigner struct {
	keys          keys.UniversalValidatorKeys
	clientCtx     client.Context
	txConfig      client.TxConfig
	log           zerolog.Logger
	sequenceMutex sync.Mutex // Mutex to synchronize transaction signing
	lastSequence  uint64     // Track the last used sequence
}

// NewTxSigner creates a new transaction signer
func NewTxSigner(
	keys keys.UniversalValidatorKeys,
	clientCtx client.Context,
	log zerolog.Logger,
) *TxSigner {
	return &TxSigner{
		keys:      keys,
		clientCtx: clientCtx,
		txConfig:  clientCtx.TxConfig,
		log:       log,
	}
}

// SignAndBroadcastAuthZTx signs and broadcasts an AuthZ transaction
func (ts *TxSigner) SignAndBroadcastAuthZTx(
	ctx context.Context,
	msgs []sdk.Msg,
	memo string,
	gasLimit uint64,
	feeAmount sdk.Coins,
) (*sdk.TxResponse, error) {
	// Lock to prevent concurrent sequence issues
	ts.sequenceMutex.Lock()
	defer ts.sequenceMutex.Unlock()

	ts.log.Info().
		Int("msg_count", len(msgs)).
		Str("memo", memo).
		Msg("Creating AuthZ transaction")

	// Wrap messages with AuthZ
	authzMsgs, err := ts.wrapMessagesWithAuthZ(msgs)
	if err != nil {
		return nil, fmt.Errorf("failed to wrap messages with AuthZ: %w", err)
	}

	// Try up to 2 times in case of sequence mismatch
	maxAttempts := 2
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		// Create and sign transaction
		txBuilder, err := ts.createTxBuilder(authzMsgs, memo, gasLimit, feeAmount)
		if err != nil {
			return nil, fmt.Errorf("failed to create tx builder: %w", err)
		}

		// Sign the transaction with sequence management
		if err := ts.signTxWithSequence(ctx, txBuilder); err != nil {
			return nil, fmt.Errorf("failed to sign transaction: %w", err)
		}

		// Encode transaction
		txBytes, err := ts.clientCtx.TxConfig.TxEncoder()(txBuilder.GetTx())
		if err != nil {
			return nil, fmt.Errorf("failed to encode transaction: %w", err)
		}

		// Broadcast transaction
		res, err := ts.broadcastTransaction(ctx, txBytes)
		if err != nil {
			// Check if error is due to sequence mismatch
			if strings.Contains(err.Error(), "account sequence mismatch") && attempt < maxAttempts {
				ts.log.Warn().
					Err(err).
					Uint64("current_sequence", ts.lastSequence).
					Int("attempt", attempt).
					Msg("Sequence mismatch detected, forcing refresh and retrying")
				// Force refresh sequence on next attempt
				ts.lastSequence = 0 // This will force a refresh from chain
				continue            // Retry
			}
			// For other errors or final attempt, increment and return error
			ts.lastSequence++
			ts.log.Debug().
				Uint64("new_sequence", ts.lastSequence).
				Msg("Incremented sequence after broadcast error")
			return nil, fmt.Errorf("failed to broadcast transaction: %w", err)
		}

		// Success - break out of retry loop
		return ts.handleBroadcastSuccess(res)
	}

	return nil, fmt.Errorf("failed to broadcast transaction after %d attempts", maxAttempts)
}

// handleBroadcastSuccess processes a successful broadcast
func (ts *TxSigner) handleBroadcastSuccess(res *sdk.TxResponse) (*sdk.TxResponse, error) {
	// Always increment sequence after successful broadcast
	// Even if the transaction fails on-chain, the sequence was consumed
	ts.lastSequence++
	ts.log.Debug().
		Uint64("new_sequence", ts.lastSequence).
		Str("tx_hash", res.TxHash).
		Msg("Incremented sequence after broadcast")

	// Always increment sequence after broadcast (whether successful or not)
	// The chain has consumed this sequence number regardless of tx success
	ts.lastSequence++
	ts.log.Debug().
		Uint64("new_sequence", ts.lastSequence).
		Msg("Incremented sequence after broadcast")

	// Check if transaction was successful
	if res.Code != 0 {
		ts.log.Error().
			Str("tx_hash", res.TxHash).
			Uint32("code", res.Code).
			Str("raw_log", res.RawLog).
			Uint64("sequence_used", ts.lastSequence-1).
			Msg("Transaction failed on chain but sequence was consumed")
		return res, fmt.Errorf("transaction failed with code %d: %s", res.Code, res.RawLog)
	}

	ts.log.Info().
		Str("tx_hash", res.TxHash).
		Int64("gas_used", res.GasUsed).
		Uint64("sequence_used", ts.lastSequence-1).
		Msg("Transaction broadcasted and executed successfully")

	return res, nil
}

// WrapMessagesWithAuthZ wraps messages with AuthZ MsgExec
func (ts *TxSigner) wrapMessagesWithAuthZ(msgs []sdk.Msg) ([]sdk.Msg, error) {
	if len(msgs) == 0 {
		return nil, fmt.Errorf("no messages to wrap")
	}

	// Validate that all messages are allowed
	for i, msg := range msgs {
		msgType := sdk.MsgTypeURL(msg)
		if !isAllowedMsgType(msgType) {
			return nil, fmt.Errorf("message type %s at index %d is not allowed for AuthZ", msgType, i)
		}
	}

	// Get hot key address for grantee
	hotKeyAddr, err := ts.keys.GetAddress()
	if err != nil {
		return nil, fmt.Errorf("failed to get hot key address: %w", err)
	}

	ts.log.Debug().
		Str("grantee", hotKeyAddr.String()).
		Int("msg_count", len(msgs)).
		Msg("Wrapping messages with AuthZ")

	// Create MsgExec
	msgExec := authz.NewMsgExec(hotKeyAddr, msgs)

	return []sdk.Msg{&msgExec}, nil
}

// CreateTxBuilder creates a transaction builder with the given parameters
func (ts *TxSigner) createTxBuilder(
	msgs []sdk.Msg,
	memo string,
	gasLimit uint64,
	feeAmount sdk.Coins,
) (client.TxBuilder, error) {
	txBuilder := ts.txConfig.NewTxBuilder()

	// Set messages
	if err := txBuilder.SetMsgs(msgs...); err != nil {
		return nil, fmt.Errorf("failed to set messages: %w", err)
	}

	// Set memo
	txBuilder.SetMemo(memo)

	// Set gas limit
	txBuilder.SetGasLimit(gasLimit)

	// Set fee amount
	txBuilder.SetFeeAmount(feeAmount)

	return txBuilder, nil
}

// signTxWithSequence signs a transaction with proper sequence management
func (ts *TxSigner) signTxWithSequence(ctx context.Context, txBuilder client.TxBuilder) error {
	ts.log.Debug().Msg("Starting transaction signing with sequence management")

	// Get account info to refresh sequence if needed
	account, err := ts.getAccountInfo(ctx)
	if err != nil {
		return fmt.Errorf("failed to get account info: %w", err)
	}

	// Always use the chain's sequence to avoid mismatches
	// This ensures we're always in sync with the blockchain
	chainSequence := account.GetSequence()
	if ts.lastSequence != chainSequence {
		ts.log.Info().
			Uint64("chain_sequence", chainSequence).
			Uint64("cached_sequence", ts.lastSequence).
			Msg("Sequence mismatch detected, using chain's sequence")
		ts.lastSequence = chainSequence
	}

	// Get hot key address
	hotKeyAddr, err := ts.keys.GetAddress()
	if err != nil {
		return fmt.Errorf("failed to get hot key address: %w", err)
	}

	// Get private key
	password := ts.keys.GetHotkeyPassword()
	privKey, err := ts.keys.GetPrivateKey(password)
	if err != nil {
		return fmt.Errorf("failed to get private key: %w", err)
	}

	ts.log.Debug().
		Str("signer", hotKeyAddr.String()).
		Uint64("account_number", account.GetAccountNumber()).
		Uint64("sequence", ts.lastSequence).
		Msg("Signing transaction with managed sequence")

	// Create signature data
	sigData := signing.SingleSignatureData{
		SignMode:  signing.SignMode_SIGN_MODE_DIRECT,
		Signature: nil,
	}

	sig := signing.SignatureV2{
		PubKey:   privKey.PubKey(),
		Data:     &sigData,
		Sequence: ts.lastSequence,
	}

	// Set empty signature first to populate SignerInfos
	if err := txBuilder.SetSignatures(sig); err != nil {
		return fmt.Errorf("failed to set signatures: %w", err)
	}

	// Use SDK's SignWithPrivKey helper function for proper signing
	signerData := authsigning.SignerData{
		Address:       hotKeyAddr.String(),
		ChainID:       ts.clientCtx.ChainID,
		AccountNumber: account.GetAccountNumber(),
		Sequence:      ts.lastSequence,
		PubKey:        privKey.PubKey(),
	}

	signV2, err := tx.SignWithPrivKey(
		ctx,
		signing.SignMode_SIGN_MODE_DIRECT,
		signerData,
		txBuilder,
		privKey,
		ts.clientCtx.TxConfig,
		ts.lastSequence,
	)
	if err != nil {
		return fmt.Errorf("failed to sign with private key: %w", err)
	}

	// Set the final signature
	if err := txBuilder.SetSignatures(signV2); err != nil {
		return fmt.Errorf("failed to set final signatures: %w", err)
	}

	ts.log.Info().
		Str("signer", hotKeyAddr.String()).
		Uint64("sequence", ts.lastSequence).
		Msg("Transaction signed successfully with managed sequence")

	return nil
}

// getAccountInfo retrieves account information for the hot key
func (ts *TxSigner) getAccountInfo(ctx context.Context) (client.Account, error) {
	hotKeyAddr, err := ts.keys.GetAddress()
	if err != nil {
		return nil, fmt.Errorf("failed to get hot key address: %w", err)
	}

	ts.log.Debug().
		Str("address", hotKeyAddr.String()).
		Msg("Querying account info from chain")

	// Create auth query client
	authClient := authtypes.NewQueryClient(ts.clientCtx)

	// Query account information
	accountResp, err := authClient.Account(ctx, &authtypes.QueryAccountRequest{
		Address: hotKeyAddr.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query account info: %w", err)
	}

	// Unpack account
	var account sdk.AccountI
	if err := ts.clientCtx.InterfaceRegistry.UnpackAny(accountResp.Account, &account); err != nil {
		return nil, fmt.Errorf("failed to unpack account: %w", err)
	}

	ts.log.Debug().
		Str("address", account.GetAddress().String()).
		Uint64("account_number", account.GetAccountNumber()).
		Uint64("sequence", account.GetSequence()).
		Msg("Retrieved account info")

	return account, nil
}

// broadcastTransaction broadcasts a signed transaction to the chain
func (ts *TxSigner) broadcastTransaction(_ context.Context, txBytes []byte) (*sdk.TxResponse, error) {
	// Use the client context's BroadcastTx method for proper broadcasting
	res, err := ts.clientCtx.BroadcastTx(txBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to broadcast transaction: %w", err)
	}

	// Log the result
	ts.log.Info().
		Str("tx_hash", res.TxHash).
		Uint32("code", res.Code).
		Int64("gas_used", res.GasUsed).
		Int64("gas_wanted", res.GasWanted).
		Msg("Transaction broadcast result")

	return res, nil
}

// checks if a message type is allowed for AuthZ execution
func isAllowedMsgType(msgType string) bool {
	return slices.Contains(constant.SupportedMessages, msgType)
}
