package vote

import (
	"context"
	"fmt"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/rs/zerolog"

	utsstypes "github.com/pushchain/push-chain-node/x/utss/types"
)

// TxSigner defines the interface for signing and broadcasting transactions.
// Defined locally to avoid import cycles with core package.
type TxSigner interface {
	SignAndBroadcastAuthZTx(ctx context.Context, msgs []sdk.Msg, memo string, gasLimit uint64, feeAmount sdk.Coins) (*sdk.TxResponse, error)
}

// Handler handles voting on TSS key processes
type Handler struct {
	txSigner TxSigner
	log      zerolog.Logger
	granter  string // operator address who granted AuthZ permissions
}

// NewHandler creates a new TSS vote handler
func NewHandler(txSigner TxSigner, log zerolog.Logger, granter string) *Handler {
	return &Handler{
		txSigner: txSigner,
		log:      log.With().Str("component", "tss_vote_handler").Logger(),
		granter:  granter,
	}
}

// VoteTssKeyProcess votes on a completed TSS key process.
// Returns vote tx hash on success, error on failure.
func (h *Handler) VoteTssKeyProcess(ctx context.Context, tssPubKey string, keyID string, processId uint64) (string, error) {
	h.log.Info().
		Str("tss_pubkey", tssPubKey).
		Str("key_id", keyID).
		Msg("starting TSS key process vote")

	// Validate inputs
	if h.txSigner == nil {
		return "", fmt.Errorf("txSigner is nil - cannot sign transactions")
	}

	if h.granter == "" {
		return "", fmt.Errorf("granter address is empty - AuthZ not properly configured")
	}

	// Create MsgVoteTssKeyProcess
	msg := &utsstypes.MsgVoteTssKeyProcess{
		Signer:    h.granter, // The granter (operator) is the signer
		TssPubkey: tssPubKey,
		KeyId:     keyID,
		ProcessId: processId,
	}

	h.log.Debug().
		Str("msg_signer", msg.Signer).
		Str("tss_pubkey", msg.TssPubkey).
		Str("key_id", msg.KeyId).
		Msg("created MsgVoteTssKeyProcess message")

	// Wrap message for AuthZ execution
	msgs := []sdk.Msg{msg}

	// Configure gas and fees - using same values as other vote handlers
	gasLimit := uint64(500000000)
	feeAmount, err := sdk.ParseCoinsNormalized("500000000000000upc")
	if err != nil {
		return "", fmt.Errorf("failed to parse fee amount: %w", err)
	}

	memo := fmt.Sprintf("Vote on TSS key process: %s", keyID)

	h.log.Debug().
		Uint64("gas_limit", gasLimit).
		Str("fee_amount", feeAmount.String()).
		Str("memo", memo).
		Msg("prepared transaction parameters, calling SignAndBroadcastAuthZTx")

	// Create timeout context for the AuthZ transaction (30 second timeout)
	voteCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Sign and broadcast the AuthZ transaction
	h.log.Info().
		Str("key_id", keyID).
		Msg("calling SignAndBroadcastAuthZTx")

	txResp, err := h.txSigner.SignAndBroadcastAuthZTx(
		voteCtx,
		msgs,
		memo,
		gasLimit,
		feeAmount,
	)

	h.log.Debug().
		Str("key_id", keyID).
		Bool("success", err == nil).
		Msg("SignAndBroadcastAuthZTx completed")

	if err != nil {
		h.log.Error().
			Str("key_id", keyID).
			Err(err).
			Msg("SignAndBroadcastAuthZTx failed")
		return "", fmt.Errorf("failed to broadcast TSS vote transaction: %w", err)
	}

	h.log.Debug().
		Str("key_id", keyID).
		Str("response_tx_hash", txResp.TxHash).
		Uint32("response_code", txResp.Code).
		Msg("received transaction response, checking status")

	if txResp.Code != 0 {
		h.log.Error().
			Str("key_id", keyID).
			Str("response_tx_hash", txResp.TxHash).
			Uint32("response_code", txResp.Code).
			Str("raw_log", txResp.RawLog).
			Msg("TSS vote transaction was rejected by blockchain")
		return "", fmt.Errorf("TSS vote transaction failed with code %d: %s", txResp.Code, txResp.RawLog)
	}

	h.log.Info().
		Str("tx_hash", txResp.TxHash).
		Str("key_id", keyID).
		Str("tss_pubkey", tssPubKey).
		Int64("gas_used", txResp.GasUsed).
		Msg("successfully voted on TSS key process")

	return txResp.TxHash, nil
}
