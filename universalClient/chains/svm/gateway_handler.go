package svm

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"strings"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/rs/zerolog"

	"github.com/pushchain/push-chain-node/universalClient/chains/common"
	"github.com/pushchain/push-chain-node/universalClient/config"
	"github.com/pushchain/push-chain-node/universalClient/db"
	"github.com/pushchain/push-chain-node/universalClient/store"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
	"gorm.io/gorm"
)

// MethodExtractionInfo caches discovered positions for each method
type MethodExtractionInfo struct {
	ReceiverInstructionIndex int
	AmountEventPosition      int
	LastVerified             time.Time
}

// GatewayHandler handles gateway operations for Solana chains
type GatewayHandler struct {
	parentClient *Client // Reference to parent client for RPC pool access
	config       *uregistrytypes.ChainConfig
	appConfig    *config.Config
	logger       zerolog.Logger
	tracker      *common.ConfirmationTracker
	gatewayAddr  solana.PublicKey
	database     *db.DB
	methodCache  map[string]*MethodExtractionInfo // Cache for discovered positions

	// Extracted components
	eventParser  *EventParser
	txVerifier   *TransactionVerifier
	eventWatcher *EventWatcher
}

// NewGatewayHandler creates a new Solana gateway handler
func NewGatewayHandler(
	parentClient *Client,
	config *uregistrytypes.ChainConfig,
	database *db.DB,
	appConfig *config.Config,
	logger zerolog.Logger,
) (*GatewayHandler, error) {
	if config.GatewayAddress == "" {
		return nil, fmt.Errorf("gateway address not configured")
	}

	// Parse gateway address
	gatewayAddr, err := solana.PublicKeyFromBase58(config.GatewayAddress)
	if err != nil {
		return nil, fmt.Errorf("invalid gateway address: %w", err)
	}

	// Create confirmation tracker
	tracker := common.NewConfirmationTracker(
		database,
		config.BlockConfirmation,
		logger,
	)

	// Create extracted components
	eventParser := NewEventParser(gatewayAddr, config, logger)
	txVerifier := NewTransactionVerifier(parentClient, config, database, tracker, logger)
	eventWatcher := NewEventWatcher(parentClient, gatewayAddr, eventParser, tracker, txVerifier, appConfig, config.Chain, logger)

	return &GatewayHandler{
		parentClient: parentClient,
		config:       config,
		appConfig:    appConfig,
		logger:       logger.With().Str("component", "svm_gateway_handler").Logger(),
		tracker:      tracker,
		gatewayAddr:  gatewayAddr,
		database:     database,
		methodCache:  make(map[string]*MethodExtractionInfo),
		eventParser:  eventParser,
		txVerifier:   txVerifier,
		eventWatcher: eventWatcher,
	}, nil
}

// SetVoteHandler sets the vote handler on the confirmation tracker
func (h *GatewayHandler) SetVoteHandler(handler common.VoteHandler) {
	if h.tracker != nil {
		h.tracker.SetVoteHandler(handler)
		h.logger.Info().Msg("vote handler set on confirmation tracker")
	}
}

// GetLatestBlock returns the latest slot number
func (h *GatewayHandler) GetLatestBlock(ctx context.Context) (uint64, error) {
	var slot uint64
	err := h.parentClient.executeWithFailover(ctx, "get_latest_slot", func(client *rpc.Client) error {
		var innerErr error
		slot, innerErr = client.GetSlot(ctx, rpc.CommitmentFinalized)
		return innerErr
	})
	if err != nil {
		return 0, fmt.Errorf("failed to get slot: %w", err)
	}
	return slot, nil
}

// GetStartSlot returns the slot to start watching from
func (h *GatewayHandler) GetStartSlot(ctx context.Context) (uint64, error) {
	// Check database for last processed slot
	var chainState store.ChainState
	result := h.database.Client().First(&chainState)

	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			// No record found, get latest slot
			h.logger.Info().Msg("no last processed slot found, starting from latest")
			return h.GetLatestBlock(ctx)
		}
		return 0, fmt.Errorf("failed to get last processed slot: %w", result.Error)
	}

	// Found a record, check if it has a valid slot number
	if chainState.LastBlock <= 0 {
		// If LastBlock is 0 or negative, start from latest slot
		h.logger.Info().
			Uint64("stored_slot", chainState.LastBlock).
			Msg("invalid or zero last slot, starting from latest")
		return h.GetLatestBlock(ctx)
	}

	h.logger.Info().
		Uint64("slot", chainState.LastBlock).
		Msg("resuming from last processed slot")

	return chainState.LastBlock, nil
}

// UpdateLastProcessedSlot updates the last processed slot in the database
func (h *GatewayHandler) UpdateLastProcessedSlot(slotNumber uint64) error {
	var chainState store.ChainState

	// Try to find existing record
	result := h.database.Client().First(&chainState)

	if result.Error != nil && result.Error != gorm.ErrRecordNotFound {
		return fmt.Errorf("failed to query last processed slot: %w", result.Error)
	}

	if result.Error == gorm.ErrRecordNotFound {
		// Create new record
		chainState = store.ChainState{
			LastBlock: slotNumber,
		}
		if err := h.database.Client().Create(&chainState).Error; err != nil {
			return fmt.Errorf("failed to create last processed slot record: %w", err)
		}
	} else {
		// Update existing record only if new slot is higher
		if slotNumber > chainState.LastBlock {
			chainState.LastBlock = slotNumber
			if err := h.database.Client().Save(&chainState).Error; err != nil {
				return fmt.Errorf("failed to update last processed slot: %w", err)
			}
		}
	}

	return nil
}

// WatchGatewayEvents starts watching for gateway events from a specific slot
func (h *GatewayHandler) WatchGatewayEvents(ctx context.Context, fromSlot uint64) (<-chan *common.GatewayEvent, error) {
	// Delegate to EventWatcher with callbacks for verification and slot updates
	return h.eventWatcher.WatchEvents(
		ctx,
		fromSlot,
		h.UpdateLastProcessedSlot,
		h.txVerifier.VerifyPendingTransactions,
	)
}

// GetTransactionConfirmations delegates to transaction verifier

func (h *GatewayHandler) GetTransactionConfirmations(ctx context.Context, txHash string) (uint64, error) {
	return h.txVerifier.GetTransactionConfirmations(ctx, txHash)
}

// IsConfirmed delegates to transaction verifier
func (h *GatewayHandler) IsConfirmed(ctx context.Context, txHash string) (bool, error) {
	return h.txVerifier.IsConfirmed(ctx, txHash)
}

// GetConfirmationTracker returns the confirmation tracker
func (h *GatewayHandler) GetConfirmationTracker() *common.ConfirmationTracker {
	return h.tracker
}

// extractTransactionDetails extracts sender, receiver, and amount from a transaction
func (h *GatewayHandler) extractTransactionDetails(tx *rpc.GetTransactionResult) (sender, receiver, amount string, err error) {
	if tx == nil || tx.Meta == nil {
		return "", "", "", fmt.Errorf("invalid transaction result")
	}

	parsedTx, err := tx.Transaction.GetTransaction()
	if err != nil {
		return "", "", "", fmt.Errorf("failed to parse transaction: %w", err)
	}

	// Extract sender - always first account (fee payer in Solana)
	if len(parsedTx.Message.AccountKeys) > 0 {
		sender = parsedTx.Message.AccountKeys[0].String()
	}

	// First, analyze balance changes to understand the transaction
	actualAmount, receiverAddr := h.analyzeTransactionFlow(tx.Meta, parsedTx.Message.AccountKeys)

	// Identify the method being called
	methodID := h.identifyMethod(tx.Meta.LogMessages)
	if methodID == "" {
		// If no method identified, return what we have from balance analysis
		return sender, receiverAddr, fmt.Sprintf("%d", actualAmount), nil
	}

	// Extract receiver dynamically using discovered position
	receiver = h.extractReceiverDynamically(parsedTx, receiverAddr, methodID)
	if receiver == "" {
		receiver = receiverAddr // Fallback to balance-based receiver
	}

	// Extract amount dynamically from event data
	amount = h.extractAmountDynamically(tx.Meta.LogMessages, actualAmount, methodID)
	if amount == "" && actualAmount > 0 {
		amount = fmt.Sprintf("%d", actualAmount) // Fallback to balance-based amount
	}

	return sender, receiver, amount, nil
}

// analyzeTransactionFlow analyzes balance changes to understand the transaction
func (h *GatewayHandler) analyzeTransactionFlow(meta *rpc.TransactionMeta, accountKeys []solana.PublicKey) (amount uint64, receiver string) {
	if meta.PreBalances == nil || meta.PostBalances == nil {
		return 0, ""
	}

	// Calculate actual transfer amount (excluding fee)
	if len(meta.PreBalances) > 0 && len(meta.PostBalances) > 0 {
		senderPre := meta.PreBalances[0]
		senderPost := meta.PostBalances[0]
		totalDecrease := senderPre - senderPost
		actualTransfer := totalDecrease - meta.Fee

		if actualTransfer > 0 {
			amount = actualTransfer
		}
	}

	// Find receiver (account with increase matching the transfer)
	for i := 1; i < len(meta.PreBalances) && i < len(meta.PostBalances); i++ {
		pre := meta.PreBalances[i]
		post := meta.PostBalances[i]

		if post > pre {
			increase := post - pre
			if increase == amount && i < len(accountKeys) {
				receiver = accountKeys[i].String()
				break
			}
		}
	}

	return amount, receiver
}

// identifyMethod identifies which gateway method was called
func (h *GatewayHandler) identifyMethod(logs []string) string {
	for _, log := range logs {
		// Check for add_funds method
		if strings.Contains(log, "add_funds") || strings.Contains(log, "AddFunds") {
			return "add_funds"
		}
		// Add more methods as needed
	}
	return ""
}

// extractReceiverDynamically finds receiver using dynamic discovery
func (h *GatewayHandler) extractReceiverDynamically(tx *solana.Transaction, expectedReceiver string, methodID string) string {
	// Check cache first
	if cached, ok := h.methodCache[methodID]; ok && cached.ReceiverInstructionIndex >= 0 {
		// Use cached position
		for _, instruction := range tx.Message.Instructions {
			programIDIndex := instruction.ProgramIDIndex
			if int(programIDIndex) >= len(tx.Message.AccountKeys) {
				continue
			}

			programID := tx.Message.AccountKeys[programIDIndex]
			if !programID.Equals(h.gatewayAddr) {
				continue
			}

			if cached.ReceiverInstructionIndex < len(instruction.Accounts) {
				accountIndex := instruction.Accounts[cached.ReceiverInstructionIndex]
				if int(accountIndex) < len(tx.Message.AccountKeys) {
					return tx.Message.AccountKeys[accountIndex].String()
				}
			}
		}
	}

	// Discover position by finding the expected receiver in instruction accounts
	for _, instruction := range tx.Message.Instructions {
		programIDIndex := instruction.ProgramIDIndex
		if int(programIDIndex) >= len(tx.Message.AccountKeys) {
			continue
		}

		programID := tx.Message.AccountKeys[programIDIndex]
		if !programID.Equals(h.gatewayAddr) {
			continue
		}

		// Find which position has our expected receiver
		for i, accountIdx := range instruction.Accounts {
			if int(accountIdx) < len(tx.Message.AccountKeys) {
				if tx.Message.AccountKeys[accountIdx].String() == expectedReceiver {
					// Found it! Cache this position
					if h.methodCache[methodID] == nil {
						h.methodCache[methodID] = &MethodExtractionInfo{}
					}
					h.methodCache[methodID].ReceiverInstructionIndex = i
					h.methodCache[methodID].LastVerified = time.Now()

					h.logger.Debug().
						Str("method", methodID).
						Int("position", i).
						Msg("discovered receiver position in instruction")

					return expectedReceiver
				}
			}
		}
	}

	return ""
}

// extractAmountDynamically finds amount using dynamic discovery
func (h *GatewayHandler) extractAmountDynamically(logs []string, expectedAmount uint64, methodID string) string {
	// Check cache first
	if cached, ok := h.methodCache[methodID]; ok && cached.AmountEventPosition > 0 {
		// Use cached position
		for _, method := range h.config.GatewayMethods {
			if method.EventIdentifier == "" {
				continue
			}

			for _, log := range logs {
				if !strings.HasPrefix(log, "Program data: ") {
					continue
				}

				base64Data := strings.TrimPrefix(log, "Program data: ")
				decoded, err := base64.StdEncoding.DecodeString(base64Data)
				if err != nil {
					continue
				}

				// Verify event identifier
				if len(decoded) < 8 {
					continue
				}

				eventID := fmt.Sprintf("%x", decoded[0:8])
				if eventID != method.EventIdentifier {
					continue
				}

				// Use cached position
				if len(decoded) >= cached.AmountEventPosition+8 {
					amountValue := binary.LittleEndian.Uint64(decoded[cached.AmountEventPosition : cached.AmountEventPosition+8])
					if amountValue > 0 && amountValue < 1000000000000000 {
						return fmt.Sprintf("%d", amountValue)
					}
				}
			}
		}
	}

	// Discover position by scanning for expected amount
	for _, method := range h.config.GatewayMethods {
		if method.EventIdentifier == "" {
			continue
		}

		for _, log := range logs {
			if !strings.HasPrefix(log, "Program data: ") {
				continue
			}

			base64Data := strings.TrimPrefix(log, "Program data: ")
			decoded, err := base64.StdEncoding.DecodeString(base64Data)
			if err != nil {
				continue
			}

			// Verify this is the right event
			if len(decoded) < 8 {
				continue
			}

			eventID := fmt.Sprintf("%x", decoded[0:8])
			if eventID != method.EventIdentifier {
				continue
			}

			// Scan for the expected amount
			for pos := 8; pos <= len(decoded)-8; pos += 8 {
				testValue := binary.LittleEndian.Uint64(decoded[pos : pos+8])

				if testValue == expectedAmount {
					// Found it! Cache this position
					if h.methodCache[methodID] == nil {
						h.methodCache[methodID] = &MethodExtractionInfo{}
					}
					h.methodCache[methodID].AmountEventPosition = pos
					h.methodCache[methodID].LastVerified = time.Now()

					h.logger.Debug().
						Str("method", methodID).
						Int("position", pos).
						Uint64("amount", testValue).
						Msg("discovered amount position in event data")

					return fmt.Sprintf("%d", testValue)
				}
			}
		}
	}

	return ""
}
