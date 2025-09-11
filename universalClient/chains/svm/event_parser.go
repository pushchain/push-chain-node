package svm

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/rs/zerolog"

	"github.com/pushchain/push-chain-node/universalClient/chains/common"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
)

// EventParser handles parsing of Solana gateway events
type EventParser struct {
	gatewayAddr solana.PublicKey
	config      *uregistrytypes.ChainConfig
	logger      zerolog.Logger
}

// NewEventParser creates a new event parser
func NewEventParser(
	gatewayAddr solana.PublicKey,
	config *uregistrytypes.ChainConfig,
	logger zerolog.Logger,
) *EventParser {
	return &EventParser{
		gatewayAddr: gatewayAddr,
		config:      config,
		logger:      logger.With().Str("component", "svm_event_parser").Logger(),
	}
}

// ParseGatewayEvent parses a transaction into a GatewayEvent
func (ep *EventParser) ParseGatewayEvent(tx *rpc.GetTransactionResult, signature string, slot uint64) *common.GatewayEvent {
	if tx == nil || tx.Meta == nil {
		return nil
	}

	// Check if transaction involves gateway program
	if !ep.isGatewayTransaction(tx) {
		return nil
	}

	// Extract method information
	methodID, methodName, confirmationType := ep.extractMethodInfo(tx)
	if methodID == "" {
		return nil
	}

	// Extract transaction details
	sender, receiver, amount, err := ep.extractTransactionDetails(tx)
	if err != nil {
		ep.logger.Warn().
			Err(err).
			Str("tx_hash", signature).
			Uint64("slot", slot).
			Msg("failed to extract transaction details - some fields may be empty")
		// Don't return nil - we can still create an event with partial data
	}

	event := &common.GatewayEvent{
		ChainID:          ep.config.Chain,
		TxHash:           signature,
		BlockNumber:      slot,
		Method:           methodName,
		EventID:          methodID,
		ConfirmationType: confirmationType,
	}

	ep.logger.Info().
		Str("tx_hash", signature).
		Str("method", methodName).
		Str("sender", sender).
		Str("receiver", receiver).
		Str("amount", amount).
		Uint64("slot", slot).
		Msg("successfully parsed Solana gateway event")

	return event
}

// isGatewayTransaction checks if the transaction involves the gateway program
func (ep *EventParser) isGatewayTransaction(tx *rpc.GetTransactionResult) bool {
	for _, log := range tx.Meta.LogMessages {
		if strings.Contains(log, ep.gatewayAddr.String()) {
			return true
		}
	}
	return false
}

// extractMethodInfo extracts method ID, name and confirmation type from transaction logs
func (ep *EventParser) extractMethodInfo(tx *rpc.GetTransactionResult) (string, string, string) {
	var methodID, methodName, confirmationType string

	for _, log := range tx.Meta.LogMessages {
		// Check for add_funds method
		if strings.Contains(log, "add_funds") || strings.Contains(log, "AddFunds") {
			methodID = "84ed4c39500ab38a" // Solana add_funds identifier
			// Find method name and confirmation type from config
			for _, method := range ep.config.GatewayMethods {
				if method.Identifier == methodID {
					methodName = method.Name
					// Map confirmation type enum to string
					if method.ConfirmationType == 2 { // CONFIRMATION_TYPE_FAST
						confirmationType = "FAST"
					} else {
						confirmationType = "STANDARD" // Default to STANDARD
					}
					break
				}
			}
			break
		}
		// Add more method checks here as needed
	}

	return methodID, methodName, confirmationType
}

// extractTransactionDetails extracts sender, receiver and amount from transaction
func (ep *EventParser) extractTransactionDetails(tx *rpc.GetTransactionResult) (string, string, string, error) {
	if tx.Transaction == nil {
		return "", "", "", fmt.Errorf("transaction data is nil")
	}

	// Decode the transaction
	decodedTx, err := ep.decodeTransaction(tx.Transaction)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to decode transaction: %w", err)
	}

	var sender, receiver, amount string

	// Get sender (fee payer)
	if len(decodedTx.Message.AccountKeys) > 0 {
		sender = decodedTx.Message.AccountKeys[0].String()
	}

	// Process instructions
	for _, instruction := range decodedTx.Message.Instructions {
		// Get program ID
		programIdx := instruction.ProgramIDIndex
		if int(programIdx) >= len(decodedTx.Message.AccountKeys) {
			continue
		}
		programID := decodedTx.Message.AccountKeys[programIdx]

		// Check if it's our gateway program
		if programID.Equals(ep.gatewayAddr) {
			// Parse gateway instruction
			if parsedData := ep.parseGatewayInstruction(instruction, decodedTx.Message.AccountKeys); parsedData != nil {
				if parsedData.sender != "" {
					sender = parsedData.sender
				}
				if parsedData.receiver != "" {
					receiver = parsedData.receiver
				}
				if parsedData.amount != "" {
					amount = parsedData.amount
				}
			}
		}
	}

	// Extract from balance changes if not found in instructions
	if amount == "" && tx.Meta != nil && len(tx.Meta.PostBalances) == len(tx.Meta.PreBalances) {
		for i := range tx.Meta.PreBalances {
			diff := int64(tx.Meta.PostBalances[i]) - int64(tx.Meta.PreBalances[i])
			if diff < 0 && i < len(decodedTx.Message.AccountKeys) {
				// This account lost balance (likely the sender)
				if sender == "" {
					sender = decodedTx.Message.AccountKeys[i].String()
				}
				if amount == "" {
					amount = fmt.Sprintf("%d", -diff)
				}
			} else if diff > 0 && i < len(decodedTx.Message.AccountKeys) {
				// This account gained balance (likely the receiver)
				if receiver == "" {
					receiver = decodedTx.Message.AccountKeys[i].String()
				}
			}
		}
	}

	return sender, receiver, amount, nil
}

// decodeTransaction decodes a base64 encoded transaction
func (ep *EventParser) decodeTransaction(encodedTx interface{}) (*solana.Transaction, error) {
	// Handle different encoding types
	var txData []byte

	switch v := encodedTx.(type) {
	case []interface{}:
		if len(v) > 0 {
			if str, ok := v[0].(string); ok {
				data, err := base64.StdEncoding.DecodeString(str)
				if err != nil {
					return nil, fmt.Errorf("failed to decode base64: %w", err)
				}
				txData = data
			}
		}
	case string:
		data, err := base64.StdEncoding.DecodeString(v)
		if err != nil {
			return nil, fmt.Errorf("failed to decode base64: %w", err)
		}
		txData = data
	case *rpc.TransactionResultEnvelope:
		// Handle versioned transaction envelope
		txBytes := v.GetBinary()
		if len(txBytes) == 0 {
			return nil, fmt.Errorf("empty transaction data in envelope")
		}
		txData = txBytes
	default:
		return nil, fmt.Errorf("unsupported transaction encoding type: %T", encodedTx)
	}

	if len(txData) == 0 {
		return nil, fmt.Errorf("empty transaction data")
	}

	// Parse the transaction
	tx, err := solana.TransactionFromBytes(txData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse transaction: %w", err)
	}

	return tx, nil
}

// instructionData holds parsed instruction data
type instructionData struct {
	sender   string
	receiver string
	amount   string
}

// parseGatewayInstruction parses a gateway program instruction
func (ep *EventParser) parseGatewayInstruction(
	instruction solana.CompiledInstruction,
	accountKeys []solana.PublicKey,
) *instructionData {
	if len(instruction.Data) < 16 {
		return nil
	}

	// Check discriminator (first 8 bytes)
	discriminator := instruction.Data[:8]

	// Check for add_funds discriminator
	addFundsDiscriminator := []byte{0x84, 0xed, 0x4c, 0x39, 0x50, 0x0a, 0xb3, 0x8a}
	if !bytesEqual(discriminator, addFundsDiscriminator) {
		return nil
	}

	result := &instructionData{}

	// Parse amount (bytes 8-16)
	if len(instruction.Data) >= 16 {
		amount := uint64(0)
		for i := 0; i < 8; i++ {
			amount |= uint64(instruction.Data[8+i]) << (8 * i)
		}
		result.amount = fmt.Sprintf("%d", amount)
	}

	// Get sender from instruction accounts (typically first account)
	if len(instruction.Accounts) > 0 {
		senderIdx := instruction.Accounts[0]
		if int(senderIdx) < len(accountKeys) {
			result.sender = accountKeys[senderIdx].String()
		}
	}

	// Get receiver from instruction accounts (typically second account)
	if len(instruction.Accounts) > 1 {
		receiverIdx := instruction.Accounts[1]
		if int(receiverIdx) < len(accountKeys) {
			result.receiver = accountKeys[receiverIdx].String()
		}
	}

	return result
}

// bytesEqual compares two byte slices
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
