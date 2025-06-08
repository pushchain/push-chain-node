package keeper

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/big"
	"strings"

	"github.com/decred/base58"
	svmrpc "github.com/rollchains/pchain/utils/rpc/svm"
	"github.com/rollchains/pchain/x/ue/types"
)

// verifySVMInteraction verifies user interacted with locker by checking tx sent by ownerKey to locker contract
func (k Keeper) verifySVMInteraction(ctx context.Context, ownerKey, txHash string, chainConfig types.ChainConfig) error {
	// Get transaction details
	tx, err := svmrpc.GetTransaction(ctx, chainConfig.PublicRpcUrl, txHash)
	if err != nil {
		return fmt.Errorf("failed to fetch transaction: %w", err)
	}

	// Verify transaction status
	if tx.Meta.Err != nil {
		return fmt.Errorf("transaction failed with error: %v", tx.Meta.Err)
	}

	fmt.Println("tx.Transaction.Message.AccountKeys", tx.Transaction.Message.AccountKeys)
	// Verify sender address (first account in AccountKeys)
	if len(tx.Transaction.Message.AccountKeys) == 0 {
		return fmt.Errorf("no accounts found in transaction")
	}
	sender := tx.Transaction.Message.AccountKeys[0]
	fmt.Println("sender", sender)
	fmt.Println("ownerKey (input)", ownerKey)

	// Convert Solana address to hex format for comparison
	senderBytes := base58.Decode(sender)
	if senderBytes == nil {
		return fmt.Errorf("failed to decode Solana address: %s", sender)
	}
	senderHex := fmt.Sprintf("0x%x", senderBytes)
	fmt.Println("senderHex", senderHex)

	if senderHex != ownerKey {
		return fmt.Errorf("transaction sender %s (hex: %s) does not match ownerKey %s", sender, senderHex, ownerKey)
	}

	// Verify program ID and instructions
	if len(tx.Transaction.Message.Instructions) == 0 {
		return fmt.Errorf("no instructions found in transaction")
	}

	// Check if any instruction calls the locker contract
	foundLockerCall := false
	for _, instruction := range tx.Transaction.Message.Instructions {
		// Verify program ID index is valid
		if instruction.ProgramIDIndex < 0 || instruction.ProgramIDIndex >= len(tx.Transaction.Message.AccountKeys) {
			return fmt.Errorf("invalid program ID index: %d", instruction.ProgramIDIndex)
		}

		programID := tx.Transaction.Message.AccountKeys[instruction.ProgramIDIndex]
		fmt.Println("program ID", programID)

		if strings.EqualFold(programID, chainConfig.LockerContractAddress) {
			foundLockerCall = true

			// Verify the instruction has the required accounts
			if len(instruction.Accounts) == 0 {
				return fmt.Errorf("instruction calling locker contract has no accounts")
			}

			// Verify the instruction has data
			if instruction.Data == "" {
				return fmt.Errorf("instruction calling locker contract has no data")
			}

			// Verify instruction data format
			// The first 8 bytes should be the instruction discriminator for "add_funds"
			// Anchor uses first 8 bytes of SHA-256("global:add_funds")
			if len(instruction.Data) < 8 {
				return fmt.Errorf("instruction data too short")
			}

			// Decode the entire instruction data first
			instructionDataBytes := base58.Decode(instruction.Data)
			if instructionDataBytes == nil {
				return fmt.Errorf("failed to decode instruction data")
			}

			// Check if we have enough bytes
			if len(instructionDataBytes) < 8 {
				return fmt.Errorf("instruction data too short: got %d bytes, need at least 8", len(instructionDataBytes))
			}

			// Take first 8 bytes as discriminator
			actualDiscriminator := instructionDataBytes[:8]

			// Calculate the discriminator for "global:add_funds" using SHA-256
			hash := sha256.Sum256([]byte("global:add_funds"))
			expectedDiscriminator := hash[:8]

			if !bytes.Equal(actualDiscriminator, expectedDiscriminator) {
				return fmt.Errorf("invalid instruction discriminator: expected %x, got %x", expectedDiscriminator, actualDiscriminator)
			}

			break
		}
	}

	if !foundLockerCall {
		return fmt.Errorf("no instruction found calling the locker contract %s", chainConfig.LockerContractAddress)
	}

	return nil
}

// verifySVMAndGetFunds verifies transaction and extracts locked amount
func (k Keeper) verifySVMAndGetFunds(ctx context.Context, ownerKey, txHash string, chainConfig types.ChainConfig) (string, error) {
	// Step 1: Fetch transaction
	tx, err := svmrpc.GetTransaction(ctx, chainConfig.PublicRpcUrl, txHash)
	if err != nil {
		return "", fmt.Errorf("fetch tx failed: %w", err)
	}

	// Verify transaction status
	if tx.Meta.Err != nil {
		return "", fmt.Errorf("transaction failed with error: %v", tx.Meta.Err)
	}

	// Verify sender address
	if len(tx.Transaction.Message.AccountKeys) == 0 {
		return "", fmt.Errorf("no accounts found in transaction")
	}
	sender := tx.Transaction.Message.AccountKeys[0]
	if !strings.EqualFold(sender, ownerKey) {
		return "", fmt.Errorf("transaction sender %s does not match ownerKey %s", sender, ownerKey)
	}

	// Verify program ID
	if len(tx.Transaction.Message.Instructions) == 0 {
		return "", fmt.Errorf("no instructions found in transaction")
	}
	programID := tx.Transaction.Message.AccountKeys[tx.Transaction.Message.Instructions[0].ProgramIDIndex]
	if !strings.EqualFold(programID, chainConfig.LockerContractAddress) {
		return "", fmt.Errorf("transaction program ID %s does not match locker contract %s", programID, chainConfig.LockerContractAddress)
	}

	// Step 2: Verify confirmations
	currentSlot, err := svmrpc.GetSlot(ctx, chainConfig.PublicRpcUrl)
	if err != nil {
		return "", fmt.Errorf("fetch current slot failed: %w", err)
	}

	confirmations := currentSlot - tx.Slot
	if confirmations < uint64(chainConfig.BlockConfirmation) {
		return "", fmt.Errorf("insufficient confirmations: got %d, need %d", confirmations, chainConfig.BlockConfirmation)
	}

	// Step 3: Parse logs for FundsAddedEvent
	var usdAmount *big.Int
	var usdExponent int32
	foundEvent := false

	for _, log := range tx.Meta.LogMessages {
		if strings.Contains(log, "Program log: "+chainConfig.FundsAddedEventTopic) {
			// The event data is in the next log message
			// Format: "Program data: <base58 encoded event data>"
			eventData := strings.TrimPrefix(log, "Program data: ")
			decodedEvent := base58.Decode(eventData)
			if decodedEvent == nil {
				return "", fmt.Errorf("failed to decode event data")
			}

			// Parse the event data
			// The event data is serialized using Anchor's format
			// We need to skip the discriminator (8 bytes) and then parse the fields
			if len(decodedEvent) < 8 {
				return "", fmt.Errorf("invalid event data length")
			}

			// Skip discriminator
			eventData = string(decodedEvent[8:])

			// Parse fields
			// Format: [user(32)][sol_amount(8)][usd_equivalent(16)][usd_exponent(4)][tx_hash(32)]
			if len(eventData) >= 92 { // 32 + 8 + 16 + 4 + 32
				// Skip user (32 bytes) and sol_amount (8 bytes)
				// Read usd_equivalent (16 bytes)
				usdAmount = new(big.Int).SetBytes([]byte(eventData[40:56]))
				// Read usd_exponent (4 bytes)
				usdExponent = int32(binary.LittleEndian.Uint32([]byte(eventData[56:60])))
				foundEvent = true
				break
			}
		}
	}

	if !foundEvent {
		return "", fmt.Errorf("FundsAddedEvent not found in transaction logs")
	}

	// Calculate final USD amount using the exponent
	// Convert to string with proper decimal places
	usdAmountStr := usdAmount.String()
	if usdExponent < 0 {
		// Add decimal point
		decimals := -int(usdExponent)
		if len(usdAmountStr) > decimals {
			usdAmountStr = usdAmountStr[:len(usdAmountStr)-decimals] + "." + usdAmountStr[len(usdAmountStr)-decimals:]
		} else {
			// Pad with leading zeros
			usdAmountStr = "0." + strings.Repeat("0", decimals-len(usdAmountStr)) + usdAmountStr
		}
	}

	return usdAmountStr, nil
}
