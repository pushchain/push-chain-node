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

	"github.com/rollchains/pchain/universalClient/chains/common"
	"github.com/rollchains/pchain/universalClient/config"
	"github.com/rollchains/pchain/universalClient/db"
	"github.com/rollchains/pchain/universalClient/store"
	uregistrytypes "github.com/rollchains/pchain/x/uregistry/types"
	"gorm.io/gorm"
)

// MethodExtractionInfo caches discovered positions for each method
type MethodExtractionInfo struct {
	ReceiverInstructionIndex int
	AmountEventPosition      int
	LastVerified            time.Time
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

	return &GatewayHandler{
		parentClient: parentClient,
		config:       config,
		appConfig:    appConfig,
		logger:       logger.With().Str("component", "solana_gateway_handler").Logger(),
		tracker:      tracker,
		gatewayAddr:  gatewayAddr,
		database:     database,
		methodCache:  make(map[string]*MethodExtractionInfo),
	}, nil
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
	var lastBlock store.LastObservedBlock
	result := h.database.Client().Where("chain_id = ?", h.config.Chain).First(&lastBlock)
	
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			// No record found, get latest slot
			h.logger.Info().Msg("no last processed slot found, starting from latest")
			return h.GetLatestBlock(ctx)
		}
		return 0, fmt.Errorf("failed to get last processed slot: %w", result.Error)
	}
	
	// Found a record, use it
	if lastBlock.Block < 0 {
		return h.GetLatestBlock(ctx)
	}
	
	h.logger.Info().
		Int64("slot", lastBlock.Block).
		Msg("resuming from last processed slot")
	
	return uint64(lastBlock.Block), nil
}

// UpdateLastProcessedSlot updates the last processed slot in the database
func (h *GatewayHandler) UpdateLastProcessedSlot(slotNumber uint64) error {
	var lastBlock store.LastObservedBlock
	
	// Try to find existing record
	result := h.database.Client().Where("chain_id = ?", h.config.Chain).First(&lastBlock)
	
	if result.Error != nil && result.Error != gorm.ErrRecordNotFound {
		return fmt.Errorf("failed to query last processed slot: %w", result.Error)
	}
	
	if result.Error == gorm.ErrRecordNotFound {
		// Create new record
		lastBlock = store.LastObservedBlock{
			ChainID: h.config.Chain,
			Block:   int64(slotNumber),
		}
		if err := h.database.Client().Create(&lastBlock).Error; err != nil {
			return fmt.Errorf("failed to create last processed slot record: %w", err)
		}
	} else {
		// Update existing record only if new slot is higher
		if int64(slotNumber) > lastBlock.Block {
			lastBlock.Block = int64(slotNumber)
			if err := h.database.Client().Save(&lastBlock).Error; err != nil {
				return fmt.Errorf("failed to update last processed slot: %w", err)
			}
		}
	}
	
	return nil
}

// WatchGatewayEvents starts watching for gateway events from a specific slot
func (h *GatewayHandler) WatchGatewayEvents(ctx context.Context, fromSlot uint64) (<-chan *common.GatewayEvent, error) {
	eventChan := make(chan *common.GatewayEvent)

	go func() {
		defer close(eventChan)

		// Use configured polling interval or default to 5 seconds
		pollingInterval := 5 * time.Second
		if h.appConfig != nil && h.appConfig.EventPollingInterval > 0 {
			pollingInterval = h.appConfig.EventPollingInterval
		}

		// Poll for new transactions periodically
		ticker := time.NewTicker(pollingInterval)
		defer ticker.Stop()

		currentSlot := fromSlot

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Get latest slot
				var latestSlot uint64
				err := h.parentClient.executeWithFailover(ctx, "get_latest_slot", func(client *rpc.Client) error {
					var innerErr error
					latestSlot, innerErr = client.GetSlot(ctx, rpc.CommitmentFinalized)
					return innerErr
				})
				if err != nil {
					h.logger.Error().Err(err).Msg("failed to get latest slot")
					continue
				}

				if currentSlot >= latestSlot {
					continue
				}

				// Get signatures for the gateway program
				var signatures []*rpc.TransactionSignature
				err = h.parentClient.executeWithFailover(ctx, "get_signatures_for_address", func(client *rpc.Client) error {
					var innerErr error
					signatures, innerErr = client.GetSignaturesForAddress(
						ctx,
						h.gatewayAddr,
					)
					return innerErr
				})
				if err != nil {
					h.logger.Error().Err(err).Msg("failed to get signatures")
					continue
				}

				// Process signatures
				for _, sig := range signatures {
					if sig.Slot < currentSlot {
						continue
					}

					// Get transaction details
					var tx *rpc.GetTransactionResult
					err = h.parentClient.executeWithFailover(ctx, "get_transaction", func(client *rpc.Client) error {
						var innerErr error
						tx, innerErr = client.GetTransaction(
							ctx,
							sig.Signature,
							&rpc.GetTransactionOpts{
								Encoding: solana.EncodingBase64,
							},
						)
						return innerErr
					})
					if err != nil {
						h.logger.Error().
							Err(err).
							Str("signature", sig.Signature.String()).
							Msg("failed to get transaction")
						continue
					}

					// Parse gateway event from transaction
					event := h.parseGatewayEvent(tx, sig.Signature.String(), sig.Slot)
					if event != nil {
						// Track transaction for confirmations
						if err := h.tracker.TrackTransaction(
							h.config.Chain,
							event.TxHash,
							event.BlockNumber,
							event.Method,
							event.EventID,
							nil,
						); err != nil {
							h.logger.Error().Err(err).
								Str("tx_hash", event.TxHash).
								Msg("failed to track transaction")
						}

						select {
						case eventChan <- event:
						case <-ctx.Done():
							return
						}
					}
				}

				// First verify all pending transactions for reorgs (Solana-specific)
				if err := h.verifyPendingTransactions(ctx); err != nil {
					h.logger.Error().Err(err).Msg("failed to verify pending transactions for reorgs")
				}

				// Then update confirmations for remaining valid transactions
				if err := h.tracker.UpdateConfirmations(h.config.Chain, latestSlot); err != nil {
					h.logger.Error().Err(err).Msg("failed to update confirmations")
				}

				// Update last processed slot in database
				if err := h.UpdateLastProcessedSlot(latestSlot); err != nil {
					h.logger.Error().Err(err).Msg("failed to update last processed slot")
				}

				currentSlot = latestSlot
			}
		}
	}()

	return eventChan, nil
}

// parseGatewayEvent parses a transaction into a GatewayEvent
func (h *GatewayHandler) parseGatewayEvent(tx *rpc.GetTransactionResult, signature string, slot uint64) *common.GatewayEvent {
	if tx == nil || tx.Meta == nil {
		return nil
	}

	// Check if transaction involves gateway program
	foundGateway := false
	for _, log := range tx.Meta.LogMessages {
		if strings.Contains(log, h.gatewayAddr.String()) {
			foundGateway = true
			break
		}
	}

	if !foundGateway {
		return nil
	}

	// Look for known method calls in logs
	var methodName string
	var methodID string
	
	for _, log := range tx.Meta.LogMessages {
		// Check for add_funds method
		if strings.Contains(log, "add_funds") || strings.Contains(log, "AddFunds") {
			methodID = "84ed4c39500ab38a" // Solana add_funds identifier
			// Find method name from config
			for _, method := range h.config.GatewayMethods {
				if method.Identifier == methodID {
					methodName = method.Name
					break
				}
			}
			break
		}
	}

	if methodID == "" {
		return nil
	}

	// Use the new dynamic extraction method to get sender, receiver, and amount
	sender, receiver, amount, err := h.extractTransactionDetails(tx)
	if err != nil {
		h.logger.Debug().
			Err(err).
			Str("tx_hash", signature).
			Msg("failed to extract transaction details")
		// Don't return nil here - we can still create an event with partial data
	}

	event := &common.GatewayEvent{
		ChainID:     h.config.Chain,
		TxHash:      signature,
		BlockNumber: slot,
		Method:      methodName,
		EventID:     methodID,
		Sender:      sender,
		Receiver:    receiver,
		Amount:      amount,
	}

	// Log the extracted information with more detail
	h.logger.Info().
		Str("tx_hash", signature).
		Str("method", methodName).
		Str("sender", sender).
		Str("receiver", receiver).
		Str("amount", amount).
		Uint64("slot", slot).
		Msg("successfully parsed Solana gateway event")

	return event
}

// GetTransactionConfirmations returns the number of confirmations for a transaction
func (h *GatewayHandler) GetTransactionConfirmations(ctx context.Context, txHash string) (uint64, error) {
	// Parse signature
	sig, err := solana.SignatureFromBase58(txHash)
	if err != nil {
		return 0, fmt.Errorf("invalid transaction hash: %w", err)
	}

	// Get transaction status
	var statuses *rpc.GetSignatureStatusesResult
	err = h.parentClient.executeWithFailover(ctx, "get_signature_statuses", func(client *rpc.Client) error {
		var innerErr error
		statuses, innerErr = client.GetSignatureStatuses(ctx, false, sig)
		return innerErr
	})
	if err != nil {
		return 0, fmt.Errorf("failed to get signature status: %w", err)
	}

	if len(statuses.Value) == 0 || statuses.Value[0] == nil {
		return 0, fmt.Errorf("transaction not found")
	}

	status := statuses.Value[0]
	
	// Map Solana confirmation status to confirmation count
	// Solana uses different confirmation levels rather than counts
	switch status.ConfirmationStatus {
	case rpc.ConfirmationStatusProcessed:
		return 1, nil
	case rpc.ConfirmationStatusConfirmed:
		return 5, nil // Approximately equivalent to "fast" confirmations
	case rpc.ConfirmationStatusFinalized:
		return 12, nil // Approximately equivalent to "standard" confirmations
	default:
		return 0, nil
	}
}

// IsConfirmed checks if a transaction has enough confirmations
func (h *GatewayHandler) IsConfirmed(ctx context.Context, txHash string, mode string) (bool, error) {
	// Check in tracker first
	confirmed, err := h.tracker.IsConfirmed(txHash, mode)
	if err == nil {
		return confirmed, nil
	}

	// Fallback to chain query
	confirmations, err := h.GetTransactionConfirmations(ctx, txHash)
	if err != nil {
		return false, err
	}

	required := h.tracker.GetRequiredConfirmations(mode)
	return confirmations >= required, nil
}

// GetConfirmationTracker returns the confirmation tracker
func (h *GatewayHandler) GetConfirmationTracker() *common.ConfirmationTracker {
	return h.tracker
}

// verifyTransactionExistence checks if a Solana transaction still exists on chain (reorg detection)
func (h *GatewayHandler) verifyTransactionExistence(
	ctx context.Context,
	tx *store.GatewayTransaction,
) (bool, error) {
	sig, err := solana.SignatureFromBase58(tx.TxHash)
	if err != nil {
		h.logger.Error().
			Err(err).
			Str("tx_hash", tx.TxHash).
			Msg("invalid Solana signature format")
		return false, err
	}

	// Get transaction status to check if it still exists
	var statuses *rpc.GetSignatureStatusesResult
	err = h.parentClient.executeWithFailover(ctx, "get_signature_statuses", func(client *rpc.Client) error {
		var innerErr error
		statuses, innerErr = client.GetSignatureStatuses(ctx, false, sig)
		return innerErr
	})
	if err != nil {
		// Transaction not found - likely reorganized out
		h.logger.Warn().
			Str("tx_hash", tx.TxHash).
			Uint64("original_slot", tx.BlockNumber).
			Err(err).
			Msg("Solana transaction not found on chain - marking as reorged")
		
		tx.Status = "reorged"
		tx.Confirmations = 0
		return false, nil
	}

	// Check if status exists
	if len(statuses.Value) == 0 || statuses.Value[0] == nil {
		h.logger.Warn().
			Str("tx_hash", tx.TxHash).
			Uint64("original_slot", tx.BlockNumber).
			Msg("Solana transaction status not found - marking as reorged")
		
		tx.Status = "reorged"
		tx.Confirmations = 0
		return false, nil
	}

	status := statuses.Value[0]

	// Check if transaction has an error (which means it was included but failed)
	if status.Err != nil {
		h.logger.Warn().
			Str("tx_hash", tx.TxHash).
			Uint64("original_slot", tx.BlockNumber).
			Interface("error", status.Err).
			Msg("Solana transaction failed - marking as failed")
		
		tx.Status = "failed"
		tx.Confirmations = 0
		return false, nil
	}

	// Check if transaction moved to a different slot due to reorg
	if status.Slot != tx.BlockNumber {
		h.logger.Warn().
			Str("tx_hash", tx.TxHash).
			Uint64("original_slot", tx.BlockNumber).
			Uint64("new_slot", status.Slot).
			Msg("Solana transaction moved to different slot due to reorg - updating slot number")
		
		// Update slot number and reset status
		tx.BlockNumber = status.Slot
		tx.Status = "pending"
		tx.Confirmations = 0
		return false, nil
	}

	return true, nil
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
					amountValue := binary.LittleEndian.Uint64(decoded[cached.AmountEventPosition:cached.AmountEventPosition+8])
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
				testValue := binary.LittleEndian.Uint64(decoded[pos:pos+8])
				
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

// verifyPendingTransactions checks all pending/fast_confirmed transactions for reorgs
func (h *GatewayHandler) verifyPendingTransactions(ctx context.Context) error {
	var pendingTxs []store.GatewayTransaction
	
	// Get all transactions that need verification
	err := h.database.Client().
		Where("chain_id = ? AND status IN (?)", h.config.Chain, []string{"pending", "fast_confirmed"}).
		Find(&pendingTxs).Error
	if err != nil {
		return fmt.Errorf("failed to fetch pending transactions for verification: %w", err)
	}

	h.logger.Debug().
		Str("chain_id", h.config.Chain).
		Int("pending_count", len(pendingTxs)).
		Msg("verifying Solana transactions for reorgs")
	
	// Verify each transaction
	for _, tx := range pendingTxs {
		exists, err := h.verifyTransactionExistence(ctx, &tx)
		if err != nil {
			h.logger.Error().
				Err(err).
				Str("tx_hash", tx.TxHash).
				Msg("failed to verify Solana transaction existence")
			continue
		}
		
		// If transaction was reorged or moved, save the updated status
		if !exists {
			if err := h.database.Client().Save(&tx).Error; err != nil {
				h.logger.Error().
					Err(err).
					Str("tx_hash", tx.TxHash).
					Msg("failed to update reorged Solana transaction")
			}
		}
	}
	
	return nil
}