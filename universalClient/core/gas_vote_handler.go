package core

import (
	"context"
	"fmt"
	"strings"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pushchain/push-chain-node/universalClient/db"
	"github.com/pushchain/push-chain-node/universalClient/keys"
	"github.com/pushchain/push-chain-node/universalClient/store"
	uetypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	"github.com/rs/zerolog"
)

// GasVoteHandler handles voting on gas prices for external chains
type GasVoteHandler struct {
	txSigner TxSignerInterface
	db       *db.DB
	log      zerolog.Logger
	keys     keys.UniversalValidatorKeys
	granter  string // operator address who granted AuthZ permissions
}

// NewGasVoteHandler creates a new gas vote handler
func NewGasVoteHandler(
	txSigner TxSignerInterface,
	db *db.DB,
	log zerolog.Logger,
	keys keys.UniversalValidatorKeys,
	granter string,
) *GasVoteHandler {
	return &GasVoteHandler{
		txSigner: txSigner,
		db:       db,
		log:      log.With().Str("component", "gas_vote_handler").Logger(),
		keys:     keys,
		granter:  granter,
	}
}

// VoteGasPrice votes on an observed gas price and stores the vote transaction
func (gh *GasVoteHandler) VoteGasPrice(
	ctx context.Context,
	chainID string,
	price uint64,
) error {
	gh.log.Info().
		Str("chain_id", chainID).
		Uint64("price", price).
		Msg("starting gas price vote")

	// Prepare vote record (GORM will auto-populate CreatedAt/UpdatedAt)
	// ChainID removed - stored in per-chain database
	voteRecord := &store.GasVoteTransaction{
		GasPrice: price,
		Status:   "pending",
	}

	// Execute vote on blockchain
	voteTxHash, err := gh.executeVote(ctx, chainID, price)
	if err != nil {
		// Store failure record
		voteRecord.Status = "failed"
		voteRecord.ErrorMsg = err.Error()
		if dbErr := gh.db.Client().Create(voteRecord).Error; dbErr != nil {
			gh.log.Error().
				Err(dbErr).
				Str("chain_id", chainID).
				Msg("failed to store vote failure record")
		}

		gh.log.Error().
			Str("chain_id", chainID).
			Err(err).
			Msg("failed to vote on gas price")
		return err
	}

	// Store success record
	voteRecord.Status = "success"
	voteRecord.VoteTxHash = voteTxHash
	if err := gh.db.Client().Create(voteRecord).Error; err != nil {
		gh.log.Error().
			Err(err).
			Str("chain_id", chainID).
			Str("vote_tx_hash", voteTxHash).
			Msg("failed to store vote success record")
		// Don't return error since the vote itself succeeded
	}

	gh.log.Info().
		Str("chain_id", chainID).
		Str("vote_tx_hash", voteTxHash).
		Uint64("price", price).
		Msg("successfully voted on gas price")

	return nil
}

// executeVote executes the MsgVoteGasPrice transaction via AuthZ and returns the vote tx hash
func (gh *GasVoteHandler) executeVote(
	ctx context.Context,
	chainID string,
	price uint64,
) (string, error) {
	gh.log.Debug().
		Str("chain_id", chainID).
		Str("granter", gh.granter).
		Uint64("price", price).
		Msg("creating MsgVoteGasPrice")

	// Validate inputs
	if gh.txSigner == nil {
		return "", fmt.Errorf("txSigner is nil - cannot sign transactions")
	}

	if gh.granter == "" {
		return "", fmt.Errorf("granter address is empty - AuthZ not properly configured")
	}

	// Extract chain reference from CAIP format (e.g., "1" from "eip155:1")
	chainRef := chainID
	if strings.Contains(chainID, ":") {
		parts := strings.Split(chainID, ":")
		if len(parts) == 2 {
			chainRef = parts[1]
		}
	}

	// Create MsgVoteGasPrice
	msg := &uetypes.MsgVoteGasPrice{
		Signer:          gh.granter, // The granter (operator) is the signer
		ObservedChainId: chainRef,   // Use plain chain reference (e.g., "1")
		Price:           price,
		BlockNumber:     0, // Block number not used for gas price voting
	}

	gh.log.Debug().
		Str("chain_id", chainID).
		Str("msg_signer", msg.Signer).
		Msg("created MsgVoteGasPrice message")

	// Wrap message for AuthZ execution
	msgs := []sdk.Msg{msg}

	// Configure gas and fees - using same values as inbound voting
	gasLimit := uint64(500000000)
	feeAmount, err := sdk.ParseCoinsNormalized("500000000000000upc")
	if err != nil {
		return "", fmt.Errorf("failed to parse fee amount: %w", err)
	}

	memo := fmt.Sprintf("Vote on gas price for %s", chainID)

	gh.log.Debug().
		Str("chain_id", chainID).
		Uint64("gas_limit", gasLimit).
		Str("fee_amount", feeAmount.String()).
		Str("memo", memo).
		Msg("prepared transaction parameters, calling SignAndBroadcastAuthZTx")

	// Create timeout context for the AuthZ transaction (30 second timeout)
	voteCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Sign and broadcast the AuthZ transaction
	gh.log.Info().
		Str("chain_id", chainID).
		Msg("calling SignAndBroadcastAuthZTx")

	txResp, err := gh.txSigner.SignAndBroadcastAuthZTx(
		voteCtx,
		msgs,
		memo,
		gasLimit,
		feeAmount,
	)

	gh.log.Debug().
		Str("chain_id", chainID).
		Bool("success", err == nil).
		Msg("SignAndBroadcastAuthZTx completed")

	if err != nil {
		gh.log.Error().
			Str("chain_id", chainID).
			Err(err).
			Msg("SignAndBroadcastAuthZTx failed")
		return "", fmt.Errorf("failed to broadcast gas vote transaction: %w", err)
	}

	gh.log.Debug().
		Str("chain_id", chainID).
		Str("response_tx_hash", txResp.TxHash).
		Uint32("response_code", txResp.Code).
		Msg("received transaction response, checking status")

	if txResp.Code != 0 {
		gh.log.Error().
			Str("chain_id", chainID).
			Str("response_tx_hash", txResp.TxHash).
			Uint32("response_code", txResp.Code).
			Str("raw_log", txResp.RawLog).
			Msg("gas vote transaction was rejected by blockchain")
		return "", fmt.Errorf("gas vote transaction failed with code %d: %s", txResp.Code, txResp.RawLog)
	}

	gh.log.Info().
		Str("tx_hash", txResp.TxHash).
		Str("chain_id", chainID).
		Int64("gas_used", txResp.GasUsed).
		Msg("successfully voted on gas price")

	return txResp.TxHash, nil
}
