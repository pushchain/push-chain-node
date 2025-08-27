package authz

import (
	"context"
	"fmt"

	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/rollchains/pchain/universalClient/keys"
	"github.com/rs/zerolog"
)

// UniversalValidatorOperations provides high-level operations for Universal Validator
type UniversalValidatorOperations struct {
	keys      keys.UniversalValidatorKeys
	signer    *Signer
	clientCtx client.Context
	txSigner  *TxSigner
	log       zerolog.Logger
}

// NewUniversalValidatorOperations creates a new operations handler
func NewUniversalValidatorOperations(
	keys keys.UniversalValidatorKeys,
	signer *Signer,
	clientCtx client.Context,
	log zerolog.Logger,
) *UniversalValidatorOperations {
	txSigner := NewTxSigner(keys, signer, clientCtx, log)

	return &UniversalValidatorOperations{
		keys:      keys,
		signer:    signer,
		clientCtx: clientCtx,
		txSigner:  txSigner,
		log:       log,
	}
}

// SubmitObserverVote submits a vote on an observed event using AuthZ
// Note: This method is kept for backwards compatibility but now validates against configured message types
func (uvo *UniversalValidatorOperations) SubmitObserverVote(
	ctx context.Context,
	voteMsg sdk.Msg,
) (*sdk.TxResponse, error) {
	uvo.log.Info().
		Str("msg_type", sdk.MsgTypeURL(voteMsg)).
		Msg("Submitting observer vote via AuthZ")

	// Validate message type against allowed types
	msgType := sdk.MsgTypeURL(voteMsg)
	if !IsAllowedMsgType(msgType) {
		return nil, fmt.Errorf("message type %s is not allowed for AuthZ execution", msgType)
	}

	// Use generic submission method
	return uvo.SubmitAuthzTransaction(ctx, []sdk.Msg{voteMsg}, "Observer vote transaction")
}

// SubmitObservation submits an observation using AuthZ
// Note: This method is kept for backwards compatibility but now validates against configured message types
func (uvo *UniversalValidatorOperations) SubmitObservation(
	ctx context.Context,
	observationMsg sdk.Msg,
) (*sdk.TxResponse, error) {
	uvo.log.Info().
		Str("msg_type", sdk.MsgTypeURL(observationMsg)).
		Msg("Submitting observation via AuthZ")

	// Validate message type against allowed types
	msgType := sdk.MsgTypeURL(observationMsg)
	if !IsAllowedMsgType(msgType) {
		return nil, fmt.Errorf("message type %s is not allowed for AuthZ execution", msgType)
	}

	// Use generic submission method
	return uvo.SubmitAuthzTransaction(ctx, []sdk.Msg{observationMsg}, "Observation transaction")
}

// UpdateRegistry updates registry configuration using AuthZ
// Note: This method is kept for backwards compatibility but now validates against configured message types
func (uvo *UniversalValidatorOperations) UpdateRegistry(
	ctx context.Context,
	updateMsg sdk.Msg,
) (*sdk.TxResponse, error) {
	uvo.log.Info().
		Str("msg_type", sdk.MsgTypeURL(updateMsg)).
		Msg("Updating registry via AuthZ")

	// Validate message type against allowed types
	msgType := sdk.MsgTypeURL(updateMsg)
	if !IsAllowedMsgType(msgType) {
		return nil, fmt.Errorf("message type %s is not allowed for AuthZ execution", msgType)
	}

	// Use generic submission method
	return uvo.SubmitAuthzTransaction(ctx, []sdk.Msg{updateMsg}, "Registry update transaction")
}

// SubmitAuthzTransaction is a generic method for submitting any AuthZ transaction
func (uvo *UniversalValidatorOperations) SubmitAuthzTransaction(
	ctx context.Context,
	msgs []sdk.Msg,
	memo string,
) (*sdk.TxResponse, error) {
	uvo.log.Info().
		Int("msg_count", len(msgs)).
		Str("memo", memo).
		Msg("Submitting AuthZ transaction")

	// Validate all message types
	for i, msg := range msgs {
		msgType := sdk.MsgTypeURL(msg)
		if !IsAllowedMsgType(msgType) {
			return nil, fmt.Errorf("message %d type %s is not allowed", i, msgType)
		}
		uvo.log.Debug().
			Int("msg_index", i).
			Str("msg_type", msgType).
			Msg("Message validated for AuthZ execution")
	}

	// Estimate gas
	gasEstimate, err := uvo.txSigner.EstimateGas(ctx, msgs, memo)
	if err != nil {
		return nil, fmt.Errorf("failed to estimate gas: %w", err)
	}

	// Submit transaction
	return uvo.txSigner.SignAndBroadcastAuthZTx(
		ctx,
		msgs,
		memo,
		gasEstimate,
		nil, // Gas-free transaction
	)
}

// SubmitBatchOperations submits multiple operations in a single AuthZ transaction
// Note: This method now delegates to the generic SubmitAuthzTransaction method
func (uvo *UniversalValidatorOperations) SubmitBatchOperations(
	ctx context.Context,
	msgs []sdk.Msg,
	memo string,
) (*sdk.TxResponse, error) {
	uvo.log.Info().
		Int("msg_count", len(msgs)).
		Str("memo", memo).
		Msg("Submitting batch operations via AuthZ")

	// Use generic submission method
	return uvo.SubmitAuthzTransaction(ctx, msgs, memo)
}

// GetHotKeyInfo returns information about the hot key
func (uvo *UniversalValidatorOperations) GetHotKeyInfo() (*HotKeyInfo, error) {
	addr, err := uvo.keys.GetAddress()
	if err != nil {
		return nil, fmt.Errorf("failed to get hot key address: %w", err)
	}

	record := uvo.keys.GetSignerInfo()
	if record == nil {
		return nil, fmt.Errorf("failed to get signer info")
	}

	pubkey, err := record.GetPubKey()
	if err != nil {
		return nil, fmt.Errorf("failed to get public key: %w", err)
	}

	return &HotKeyInfo{
		Address:         addr.String(),
		PublicKey:       fmt.Sprintf("%x", pubkey.Bytes()),
		KeyName:         record.Name,
		OperatorAddress: uvo.signer.GranterAddress,
		KeyType:         uvo.signer.KeyType.String(),
	}, nil
}

// HotKeyInfo contains information about the hot key
type HotKeyInfo struct {
	Address         string  `json:"address"`
	PublicKey       string  `json:"public_key"`
	KeyName         string  `json:"key_name"`
	OperatorAddress string  `json:"operator_address"`
	KeyType         string  `json:"key_type"`
}

// GetAccountInfo returns account information for the hot key
func (uvo *UniversalValidatorOperations) GetAccountInfo(ctx context.Context) (client.Account, error) {
	return uvo.txSigner.GetAccountInfo(ctx)
}

// ValidateOperationalReadiness checks if the Universal Validator is ready for operations
func (uvo *UniversalValidatorOperations) ValidateOperationalReadiness(ctx context.Context) error {
	// Check hot key access
	hotKeyAddr, err := uvo.keys.GetAddress()
	if err != nil {
		return fmt.Errorf("hot key is not accessible: %w", err)
	}

	// Check keyring access
	signerInfo := uvo.keys.GetSignerInfo()
	if signerInfo == nil {
		return fmt.Errorf("hot key signer info not accessible")
	}

	uvo.log.Info().
		Str("hot_key", hotKeyAddr.String()).
		Str("key_name", signerInfo.Name).
		Str("operator", uvo.signer.GranterAddress).
		Msg("Universal Validator operational readiness validated (basic checks)")

	// TODO: Add chain connectivity checks once gRPC integration is complete
	uvo.log.Warn().Msg("Chain connectivity validation not yet implemented")

	return nil
}