package keeper

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
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
	tx, err := svmrpc.SolGetTransactionBySig(ctx, chainConfig.PublicRpcUrl, txHash)
	if err != nil {
		return fmt.Errorf("failed to fetch transaction: %w", err)
	}

	// Verify transaction status
	if tx.Meta.Err != nil {
		return fmt.Errorf("transaction failed with error: %v", tx.Meta.Err)
	}

	// Verify sender address
	if len(tx.Transaction.Message.AccountKeys) == 0 {
		return fmt.Errorf("no accounts found in transaction")
	}
	sender := tx.Transaction.Message.AccountKeys[0]

	// Convert Solana address to hex format for comparison
	senderBytes := base58.Decode(sender)
	if senderBytes == nil {
		return fmt.Errorf("failed to decode Solana address: %s", sender)
	}
	senderHex := fmt.Sprintf("0x%x", senderBytes)

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
	tx, err := svmrpc.SolGetTransactionBySig(ctx, chainConfig.PublicRpcUrl, txHash)
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

	// Convert Solana address to hex format for comparison
	senderBytes := base58.Decode(sender)
	if senderBytes == nil {
		return "", fmt.Errorf("failed to decode Solana address: %s", sender)
	}
	senderHex := fmt.Sprintf("0x%x", senderBytes)

	if senderHex != ownerKey {
		return "", fmt.Errorf("transaction sender %s (hex: %s) does not match ownerKey %s", sender, senderHex, ownerKey)
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
	currentSlot, err := svmrpc.SolGetCurrentSlot(ctx, chainConfig.PublicRpcUrl)
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

	// FundsAddedEvent discriminator - first 8 bytes of SHA-256("event:FundsAddedEvent")
	FundsAddedDiscriminator := []byte{0x7f, 0x1f, 0x6c, 0xff, 0xbb, 0x13, 0x46, 0x44}

	for _, log := range tx.Meta.LogMessages {
		if strings.HasPrefix(log, "Program data: ") {
			encoded := strings.TrimPrefix(log, "Program data: ")

			// Try to decode the base64 data
			raw, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				continue
			}

			if len(raw) < 100 {
				continue
			}

			// Check discriminator
			if !bytes.Equal(raw[:8], FundsAddedDiscriminator) {
				continue
			}

			// Parse event
			eventBytes := raw[8:]

			// Skip user (32 bytes) and sol_amount (8 bytes)
			// Read usd_equivalent (16 bytes) as i128
			usdAmount = readI128LE(eventBytes[40:56])
			// Read usd_exponent (4 bytes)
			usdExponent = int32(binary.LittleEndian.Uint32(eventBytes[56:60]))
			foundEvent = true
			break
		}
	}

	if !foundEvent {
		return "", fmt.Errorf("FundsAddedEvent not found in transaction logs")
	}

	// Apply the exponent to get the final amount
	if usdExponent < 0 {
		divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(-usdExponent)), nil)
		usdAmount = new(big.Int).Div(usdAmount, divisor)
	} else if usdExponent > 0 {
		multiplier := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(usdExponent)), nil)
		usdAmount = new(big.Int).Mul(usdAmount, multiplier)
	}

	usdReturn := new(big.Int).Mul(usdAmount, big.NewInt(1e6))

	return usdReturn.String(), nil
}

// readI128LE decodes a little-endian i128 value from Anchor logs
func readI128LE(b []byte) *big.Int {
	if len(b) != 16 {
		panic("i128 must be 16 bytes")
	}

	// Interpret as little-endian
	// Copy bytes into 16-byte array
	var le [16]byte
	copy(le[:], b[:16])

	// Convert to big.Int (will be positive)
	i := new(big.Int).SetBytes(reverseBytes(le[:]))

	// Check if it's negative (signed i128)
	if le[15]&0x80 != 0 {
		// 2's complement negative: i - 2^128
		two128 := new(big.Int).Lsh(big.NewInt(1), 128)
		i.Sub(i, two128)
	}
	return i
}

// reverseBytes reverses a byte slice (from little to big-endian)
func reverseBytes(b []byte) []byte {
	out := make([]byte, len(b))
	for i := range b {
		out[i] = b[len(b)-1-i]
	}
	return out
}
