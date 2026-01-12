package vote

import (
	"context"
	"fmt"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/rs/zerolog"

	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
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

const (
	// Default gas limit for vote transactions
	defaultGasLimit = uint64(500000000)
	// Default fee amount for vote transactions
	defaultFeeAmount = "1000000000000000000upc"
	// Default timeout for vote transactions
	defaultVoteTimeout = 30 * time.Second
)

// validateHandler checks that the handler is properly configured
func (h *Handler) validateHandler() error {
	if h.txSigner == nil {
		return fmt.Errorf("txSigner is nil - cannot sign transactions")
	}
	if h.granter == "" {
		return fmt.Errorf("granter address is empty - AuthZ not properly configured")
	}
	return nil
}

// prepareTxParams prepares gas limit and fee amount for transactions
func (h *Handler) prepareTxParams() (uint64, sdk.Coins, error) {
	feeAmount, err := sdk.ParseCoinsNormalized(defaultFeeAmount)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to parse fee amount: %w", err)
	}
	return defaultGasLimit, feeAmount, nil
}

// broadcastVoteTx handles the common transaction broadcasting logic
// Returns the transaction hash on success, error on failure.
func (h *Handler) broadcastVoteTx(
	ctx context.Context,
	msgs []sdk.Msg,
	memo string,
	logFields map[string]interface{},
	errorPrefix string,
) (string, error) {
	gasLimit, feeAmount, err := h.prepareTxParams()
	if err != nil {
		return "", err
	}

	h.log.Debug().
		Uint64("gas_limit", gasLimit).
		Str("fee_amount", feeAmount.String()).
		Str("memo", memo).
		Fields(logFields).
		Msg("prepared transaction parameters, calling SignAndBroadcastAuthZTx")

	// Create timeout context for the AuthZ transaction
	voteCtx, cancel := context.WithTimeout(ctx, defaultVoteTimeout)
	defer cancel()

	// Sign and broadcast the AuthZ transaction
	logEvent := h.log.Info().Fields(logFields)
	logEvent.Msg("calling SignAndBroadcastAuthZTx")

	txResp, err := h.txSigner.SignAndBroadcastAuthZTx(
		voteCtx,
		msgs,
		memo,
		gasLimit,
		feeAmount,
	)

	h.log.Debug().
		Fields(logFields).
		Bool("success", err == nil).
		Msg("SignAndBroadcastAuthZTx completed")

	if err != nil {
		h.log.Error().
			Fields(logFields).
			Err(err).
			Msg("SignAndBroadcastAuthZTx failed")
		return "", fmt.Errorf("failed to broadcast %s transaction: %w", errorPrefix, err)
	}

	h.log.Debug().
		Fields(logFields).
		Str("response_tx_hash", txResp.TxHash).
		Uint32("response_code", txResp.Code).
		Msg("received transaction response, checking status")

	if txResp.Code != 0 {
		h.log.Error().
			Fields(logFields).
			Str("response_tx_hash", txResp.TxHash).
			Uint32("response_code", txResp.Code).
			Str("raw_log", txResp.RawLog).
			Str("error_prefix", errorPrefix).
			Msg("vote transaction was rejected by blockchain")
		return "", fmt.Errorf("%s transaction failed with code %d: %s", errorPrefix, txResp.Code, txResp.RawLog)
	}

	return txResp.TxHash, nil
}

// VoteTssKeyProcess votes on a completed TSS key process.
// Returns vote tx hash on success, error on failure.
func (h *Handler) VoteTssKeyProcess(ctx context.Context, tssPubKey string, keyID string, processId uint64) (string, error) {
	h.log.Info().
		Str("tss_pubkey", tssPubKey).
		Str("key_id", keyID).
		Msg("starting TSS key process vote")

	// Validate handler configuration
	if err := h.validateHandler(); err != nil {
		return "", err
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
	memo := fmt.Sprintf("Vote on TSS key process: %s", keyID)

	logFields := map[string]interface{}{
		"key_id": keyID,
	}

	txHash, err := h.broadcastVoteTx(ctx, msgs, memo, logFields, "TSS vote")
	if err != nil {
		return "", err
	}

	h.log.Info().
		Str("tx_hash", txHash).
		Str("key_id", keyID).
		Str("tss_pubkey", tssPubKey).
		Msg("successfully voted on TSS key process")

	return txHash, nil
}

// VoteOutbound votes on an outbound transaction observation.
// txID is the outbound tx ID (abi.encode(utxId, outboundId)).
// isSuccess indicates whether the transaction succeeded.
// For success: txHash and blockHeight must be provided (blockHeight > 0).
// For revert: reason must be provided; txHash and blockHeight are optional (if txHash is provided, blockHeight must be > 0).
func (h *Handler) VoteOutbound(ctx context.Context, txID string, isSuccess bool, txHash string, blockHeight uint64, reason string) (string, error) {
	if isSuccess {
		h.log.Info().
			Str("tx_id", txID).
			Str("tx_hash", txHash).
			Uint64("block_height", blockHeight).
			Msg("starting outbound success vote")
	} else {
		h.log.Info().
			Str("tx_id", txID).
			Str("reason", reason).
			Str("tx_hash", txHash).
			Uint64("block_height", blockHeight).
			Msg("starting outbound revert vote")
	}

	// Validate handler configuration
	if err := h.validateHandler(); err != nil {
		return "", err
	}

	// Validate specific inputs
	if txID == "" {
		return "", fmt.Errorf("txID cannot be empty")
	}

	if isSuccess {
		if txHash == "" {
			return "", fmt.Errorf("txHash cannot be empty for success vote")
		}
		if blockHeight == 0 {
			return "", fmt.Errorf("blockHeight must be > 0 for success vote")
		}
	} else {
		if reason == "" {
			return "", fmt.Errorf("reason cannot be empty for revert vote")
		}
		// If txHash is provided, blockHeight must be > 0
		if txHash != "" && blockHeight == 0 {
			return "", fmt.Errorf("blockHeight must be > 0 when txHash is provided")
		}
	}

	// Create OutboundObservation
	observedTx := uexecutortypes.OutboundObservation{
		Success:     isSuccess,
		BlockHeight: blockHeight,
		TxHash:      txHash,
		ErrorMsg:    reason,
	}

	// Create MsgVoteOutbound
	msg := &uexecutortypes.MsgVoteOutbound{
		Signer:     h.granter,
		TxId:       txID,
		ObservedTx: &observedTx,
	}

	h.log.Debug().
		Str("msg_signer", msg.Signer).
		Str("tx_id", msg.TxId).
		Bool("success", observedTx.Success).
		Str("tx_hash", observedTx.TxHash).
		Uint64("block_height", observedTx.BlockHeight).
		Str("error_msg", observedTx.ErrorMsg).
		Msg("created MsgVoteOutbound message")

	// Wrap message for AuthZ execution
	msgs := []sdk.Msg{msg}

	var memo string
	if isSuccess {
		memo = fmt.Sprintf("Vote outbound success: %s", txID)
	} else {
		memo = fmt.Sprintf("Vote outbound revert: %s - %s", txID, reason)
	}

	logFields := map[string]interface{}{
		"tx_id":      txID,
		"is_success": isSuccess,
	}

	voteTxHash, err := h.broadcastVoteTx(ctx, msgs, memo, logFields, "outbound vote")
	if err != nil {
		return "", err
	}

	if isSuccess {
		h.log.Info().
			Str("tx_hash", voteTxHash).
			Str("tx_id", txID).
			Str("external_tx_hash", txHash).
			Msg("successfully voted on outbound success")
	} else {
		h.log.Info().
			Str("tx_hash", voteTxHash).
			Str("tx_id", txID).
			Str("reason", reason).
			Msg("successfully voted on outbound revert")
	}

	return voteTxHash, nil
}
