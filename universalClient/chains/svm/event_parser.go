package svm

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
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
	decoder     *EventDecoder // Add event decoder
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
		decoder:     NewEventDecoder(logger),
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

	// Extract transaction details including logIndex, txType, gasAmount, and revertRecipient
	sender, receiver, amount, gasAmount, data, verificationData, bridgeToken, logIndex, txType, revertRecipient, err := ep.extractTransactionDetails(tx)
	if err != nil {
		ep.logger.Warn().
			Err(err).
			Str("tx_hash", signature).
			Uint64("slot", slot).
			Msg("failed to extract transaction details - some fields may be empty")
		// Don't return nil - we can still create an event with partial data
	}

	// Create payload similar to EVM
	payload := common.TxWithFundsPayload{
		SourceChain:         ep.config.Chain,
		LogIndex:            logIndex,
		Sender:              sender,
		Recipient:           receiver,
		BridgeToken:         bridgeToken,
		BridgeAmount:        amount,
		GasAmount:           gasAmount,
		Data:                data,
		VerificationData:    verificationData,
		RevertFundRecipient: revertRecipient,
		TxType:              txType,
	}

	// Marshal payload to JSON
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		ep.logger.Warn().
			Err(err).
			Str("tx_hash", signature).
			Msg("failed to marshal payload")
		payloadBytes = []byte{} // Use empty payload if marshaling fails
	}

	event := &common.GatewayEvent{
		ChainID:          ep.config.Chain,
		TxHash:           signature,
		BlockNumber:      slot,
		Method:           methodName,
		EventID:          methodID,
		ConfirmationType: confirmationType,
		Payload:          payloadBytes, // Set the payload for vote handler
	}

	// Debug logging for tracking
	ep.logger.Debug().
		Str("tx_hash", signature).
		Str("method", methodName).
		Int("payload_size", len(payloadBytes)).
		Str("sender", sender).
		Str("receiver", receiver).
		Str("amount", amount).
		Msg("parsed Solana gateway event with payload")

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
	// Log all messages first for debugging
	for i, log := range tx.Meta.LogMessages {
		ep.logger.Debug().
			Int("log_index", i).
			Str("log_message", log).
			Msg("processing log message")
	}

	// Iterate through configured gateway methods
	for _, method := range ep.config.GatewayMethods {
		methodNameLower := strings.ToLower(method.Name)

		// Check each log message for this method
		for _, log := range tx.Meta.LogMessages {
			logLower := strings.ToLower(log)

			var matched bool

			// Flexible matching based on method name patterns
			if strings.Contains(methodNameLower, "send") && strings.Contains(methodNameLower, "funds") {
				// Match variations like "send_funds", "SendFunds", "SendTxWithFunds"
				matched = strings.Contains(logLower, "send") && strings.Contains(logLower, "funds") &&
					!strings.Contains(logLower, "add") // Exclude add_funds

				if matched {
					ep.logger.Debug().
						Str("log", log).
						Str("method_name", method.Name).
						Msg("matched send funds pattern")
				}
			} else if strings.Contains(methodNameLower, "add") && strings.Contains(methodNameLower, "funds") {
				// Skip add_funds - we're only interested in send_funds/deposit
				continue
			}
			// Add more method patterns as needed

			if matched {
				// Derive confirmation type from config
				var confirmationType string
				if method.ConfirmationType == 2 { // CONFIRMATION_TYPE_FAST
					confirmationType = "FAST"
				} else {
					confirmationType = "STANDARD" // Default to STANDARD
				}

				ep.logger.Info().
					Str("method", method.Name).
					Str("method_id", method.Identifier).
					Str("event_id", method.EventIdentifier).
					Str("confirmation_type", confirmationType).
					Str("original_log", log).
					Msg("detected gateway method from config")

				// Return values from config
				return method.Identifier, method.Name, confirmationType
			}
		}
	}

	ep.logger.Debug().
		Msg("no recognized method found in transaction logs")
	return "", "", ""
}

// extractTransactionDetails extracts sender, receiver, amount, gasAmount, data, verificationData, bridgeToken, logIndex, txType, and revertRecipient from transaction
func (ep *EventParser) extractTransactionDetails(tx *rpc.GetTransactionResult) (string, string, string, string, string, string, string, uint, uint, string, error) {
	if tx.Transaction == nil {
		return "", "", "", "", "", "", "", 0, 0, "", fmt.Errorf("transaction data is nil")
	}

	// Log all Program data entries for debugging
	programDataCount := 0
	for i, log := range tx.Meta.LogMessages {
		if strings.HasPrefix(log, "Program data: ") {
			programDataCount++
			ep.logger.Debug().
				Int("log_index", i).
				Str("program_data", strings.TrimPrefix(log, "Program data: ")).
				Msg("found Program data log entry")
		}
	}

	ep.logger.Info().
		Int("total_logs", len(tx.Meta.LogMessages)).
		Int("program_data_logs", programDataCount).
		Msg("scanning transaction logs for events")

	// First, try to extract from Program data logs (events)
	eventData := ep.extractEventDataFromLogs(tx.Meta.LogMessages)
	if eventData != nil {
		if eventData.EventType != "TxWithFunds" {
			ep.logger.Debug().
				Str("event_type", eventData.EventType).
				Msg("ignoring non TxWithFunds event data from Program logs")
			// treat as no event data so we can fall back to instruction parsing
			eventData = nil
		}
	}

	if eventData != nil {
		ep.logger.Info().
			Str("event_source", "program_data_logs").
			Str("event_type", eventData.EventType).
			Bool("has_data", eventData.Data != "").
			Bool("has_verification", eventData.VerificationData != "").
			Msg("successfully extracted event data from Program logs")

		ep.logger.Debug().
			Str("event_type", eventData.EventType).
			Str("sender", eventData.Sender).
			Str("recipient", eventData.Recipient).
			Uint64("amount", eventData.BridgeAmount).
			Uint8("tx_type", eventData.TxType).
			Msg("extracted data from event logs")

		// Convert to return format
		amount := fmt.Sprintf("%d", eventData.BridgeAmount)
		gasAmount := fmt.Sprintf("%d", eventData.GasAmount)

		// For AddFunds events, recipient might be empty
		recipient := eventData.Recipient
		if recipient == "" && eventData.EventType == "AddFunds" {
			// For AddFunds, the sender is adding funds to their own account
			recipient = eventData.Sender
		}

		return eventData.Sender,
			recipient,
			amount,
			gasAmount,
			eventData.Data,
			eventData.VerificationData,
			eventData.BridgeToken,
			eventData.LogIndex,
			uint(eventData.TxType),
			eventData.RevertRecipient,
			nil
	}

	// Fallback to instruction parsing if no event data found
	ep.logger.Warn().
		Int("logs_checked", len(tx.Meta.LogMessages)).
		Msg("no TxWithFunds/AddFunds event data found in Program logs, falling back to instruction parsing")

	// Decode the transaction
	decodedTx, err := ep.decodeTransaction(tx.Transaction)
	if err != nil {
		return "", "", "", "", "", "", "", 0, 0, "", fmt.Errorf("failed to decode transaction: %w", err)
	}

	var sender, receiver, amount, data, verificationData, bridgeToken string
	var logIndex, txType uint

	// Get sender (fee payer)
	if len(decodedTx.Message.AccountKeys) > 0 {
		sender = decodedTx.Message.AccountKeys[0].String()
	}

	// Process instructions
	for idx, instruction := range decodedTx.Message.Instructions {
		// Get program ID
		programIdx := instruction.ProgramIDIndex
		if int(programIdx) >= len(decodedTx.Message.AccountKeys) {
			continue
		}
		programID := decodedTx.Message.AccountKeys[programIdx]

		// Check if it's our gateway program
		if programID.Equals(ep.gatewayAddr) {
			// Set logIndex to the instruction index
			logIndex = uint(idx)
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
				if parsedData.data != "" {
					data = parsedData.data
				}
				if parsedData.verificationData != "" {
					verificationData = parsedData.verificationData
				}
				if parsedData.bridgeToken != "" {
					bridgeToken = parsedData.bridgeToken
				}
				// Use the txType from parsed instruction data
				txType = parsedData.txType
			} else {
				// Only default to 0 if parsing failed
				txType = 0
			}
		}
	}

	// Extract from balance changes if not found
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

	// Final logging to show what was extracted
	ep.logger.Info().
		Str("extraction_method", "fallback_instruction_parsing").
		Str("sender", sender).
		Str("receiver", receiver).
		Str("amount", amount).
		Str("data", data).
		Str("verificationData", verificationData).
		Str("bridgeToken", bridgeToken).
		Uint("logIndex", logIndex).
		Uint("txType", txType).
		Bool("data_empty", data == "").
		Bool("verification_empty", verificationData == "").
		Msg("final extracted transaction details")

	return sender, receiver, amount, "", data, verificationData, bridgeToken, logIndex, txType, "", nil
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
	sender           string
	receiver         string
	amount           string
	data             string
	verificationData string
	bridgeToken      string
	txType           uint
}

// parseGatewayInstruction parses a gateway program instruction
func (ep *EventParser) parseGatewayInstruction(
	instruction solana.CompiledInstruction,
	accountKeys []solana.PublicKey,
) *instructionData {
	if len(instruction.Data) < 16 {
		ep.logger.Debug().
			Int("data_len", len(instruction.Data)).
			Msg("instruction data too short for gateway instruction")
		return nil
	}

	// Log instruction data for debugging
	ep.logger.Debug().
		Int("data_len", len(instruction.Data)).
		Str("data_hex", hex.EncodeToString(instruction.Data[:16])).
		Int("num_accounts", len(instruction.Accounts)).
		Msg("parsing gateway instruction")

	// Process any gateway instruction (not checking for specific discriminator)
	// This allows processing of send_funds and other gateway methods
	result := &instructionData{}

	// Parse amount (bytes 8-16)
	if len(instruction.Data) >= 16 {
		amount := uint64(0)
		for i := 0; i < 8; i++ {
			amount |= uint64(instruction.Data[8+i]) << (8 * i)
		}
		result.amount = fmt.Sprintf("%d", amount)
	}

	// Parse dynamic data fields after byte 16
	// Solana uses Borsh serialization for send_funds instruction:
	// [discriminator(8)] [amount(8)] [data_len(4)] [data_bytes...] [verification_bytes...]
	// The data field is a Vec<u8> with a 4-byte length prefix
	// For simple transfers, data_len is typically 0 (empty data field)
	hasPayloadData := false
	if len(instruction.Data) > 16 {
		// Check if we have at least 4 bytes for data length
		if len(instruction.Data) >= 20 {
			// Read data length (4 bytes at position 16)
			dataLen := uint32(0)
			for i := 0; i < 4; i++ {
				dataLen |= uint32(instruction.Data[16+i]) << (8 * i)
			}

			offset := 20 // After discriminator(8) + amount(8) + data_len(4)

			// Extract data field if present
			if dataLen > 0 && offset+int(dataLen) <= len(instruction.Data) {
				dataBytes := instruction.Data[offset : offset+int(dataLen)]
				result.data = "0x" + hex.EncodeToString(dataBytes)
				offset += int(dataLen)
				hasPayloadData = true // Has non-empty data field
			} else {
				// Data field is empty (common case)
				result.data = ""
			}

			// All remaining bytes are verification data (not length-prefixed)
			if offset < len(instruction.Data) {
				verificationBytes := instruction.Data[offset:]
				if len(verificationBytes) > 0 {
					result.verificationData = "0x" + hex.EncodeToString(verificationBytes)
				}
			}
		} else {
			// Fallback: if less than 20 bytes, treat all bytes after amount as verification
			verificationBytes := instruction.Data[16:]
			if len(verificationBytes) > 0 {
				result.verificationData = "0x" + hex.EncodeToString(verificationBytes)
			}
		}
	}

	// Determine txType based on whether there's payload data
	// In Solana contract: 0 = Funds (no payload), 1 = Message (with payload)
	if hasPayloadData {
		result.txType = 1 // Message type (has payload)
	} else {
		result.txType = 0 // Funds type (no payload)
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

	// Get bridge token from instruction accounts (typically third account for SPL token mint)
	// In Solana, if bridging SPL tokens, the mint address would be in the accounts
	if len(instruction.Accounts) > 2 {
		tokenIdx := instruction.Accounts[2]
		if int(tokenIdx) < len(accountKeys) {
			result.bridgeToken = accountKeys[tokenIdx].String()
		}
	}

	ep.logger.Info().
		Str("sender", result.sender).
		Str("receiver", result.receiver).
		Str("amount", result.amount).
		Str("data", result.data).
		Str("verificationData", result.verificationData).
		Str("bridgeToken", result.bridgeToken).
		Uint("txType", result.txType).
		Bool("hasData", result.data != "").
		Bool("hasVerification", result.verificationData != "").
		Bool("has_payload", hasPayloadData).
		Msg("parsed gateway instruction (fallback mode)")

	return result
}

// extractEventDataFromLogs extracts event data from Program data logs
func (ep *EventParser) extractEventDataFromLogs(logs []string) *ParsedEventData {
	for i, log := range logs {
		if !strings.HasPrefix(log, "Program data: ") {
			continue
		}

		eventData := strings.TrimPrefix(log, "Program data: ")
		decoded, err := base64.StdEncoding.DecodeString(eventData)
		if err != nil {
			ep.logger.Warn().
				Err(err).
				Str("log", log).
				Msg("failed to decode Program data")
			continue
		}

		// Try to decode as an event
		parsedEvent, err := ep.decoder.DecodeEventData(decoded)
		if err != nil {
			// Not a recognized event, continue
			ep.logger.Debug().
				Err(err).
				Int("log_index", i).
				Msg("could not decode as event")
			continue
		}

		// Set the log index for traceability
		parsedEvent.LogIndex = uint(i)

		if parsedEvent.EventType == "AddFunds" {
			ep.logger.Debug().
				Int("log_index", i).
				Msg("skipping AddFunds event in favor of TxWithFunds")
			continue
		}

		if parsedEvent.EventType != "TxWithFunds" {
			ep.logger.Debug().
				Str("event_type", parsedEvent.EventType).
				Int("log_index", i).
				Msg("unrecognized event type, continuing scan")
			continue
		}

		ep.logger.Info().
			Str("event_type", parsedEvent.EventType).
			Str("sender", parsedEvent.Sender).
			Str("recipient", parsedEvent.Recipient).
			Uint64("amount", parsedEvent.BridgeAmount).
			Uint8("tx_type", parsedEvent.TxType).
			Str("data", parsedEvent.Data).
			Str("verification_data", parsedEvent.VerificationData).
			Int("log_index", i).
			Msg("successfully extracted event data from logs")

		return parsedEvent
	}

	return nil
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
