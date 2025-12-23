// svm_helpers.go
// SVM-specific helper functions used in inbound transaction verification.
package keeper

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"math/big"
	"strings"

	"github.com/decred/base58"
	// "github.com/pushchain/push-chain-node/utils/rpc"
	// svmrpc "github.com/pushchain/push-chain-node/utils/rpc/svm"

	utxverifiertypes "github.com/pushchain/push-chain-node/x/utxverifier/types"
)

func IsValidSVMSender(accountKeys []string, expectedHex string) (string, error) {
	if len(accountKeys) == 0 {
		return "", fmt.Errorf("no accounts found in transaction")
	}

	sender := accountKeys[0]
	senderBytes := base58.Decode(sender)
	if senderBytes == nil {
		return "", fmt.Errorf("failed to decode Solana address: %s", sender)
	}

	senderHex := fmt.Sprintf("0x%x", senderBytes)

	if senderHex != expectedHex {
		return "", fmt.Errorf("transaction sender %s (hex: %s) does not match expected %s", sender, senderHex, expectedHex)
	}

	return senderHex, nil
}

// compareSVMAddresses compares two Solana addresses by decoding them to raw bytes
func compareSVMAddresses(addr1, addr2 string) bool {
	bytes1 := base58.Decode(addr1)
	bytes2 := base58.Decode(addr2)
	return bytes1 != nil && bytes2 != nil && bytes.Equal(bytes1, bytes2)
}

// func IsValidSVMAddFundsInstruction(
// 	instructions []svmrpc.Instruction,
// 	accountKeys []string,
// 	chainConfig uregistrytypes.ChainConfig,
// ) error {
// 	for _, inst := range instructions {
// 		if inst.ProgramIDIndex < 0 || inst.ProgramIDIndex >= len(accountKeys) {
// 			return fmt.Errorf("invalid program ID index: %d", inst.ProgramIDIndex)
// 		}
// 		programID := accountKeys[inst.ProgramIDIndex]
// 		if !compareSVMAddresses(programID, chainConfig.GatewayAddress) {
// 			continue
// 		}

// 		if len(inst.Accounts) == 0 {
// 			return fmt.Errorf("gateway instruction missing accounts")
// 		}
// 		if inst.Data == "" {
// 			return fmt.Errorf("gateway instruction missing data")
// 		}

// 		dataBytes := base58.Decode(inst.Data)
// 		if dataBytes == nil || len(dataBytes) < 8 {
// 			return fmt.Errorf("invalid instruction data format")
// 		}
// 		actual := dataBytes[:8]

// 		var expected []byte
// 		for _, method := range chainConfig.GatewayMethods {
// 			if method.Name == uregistrytypes.GATEWAY_METHOD.SVM.AddFunds {
// 				var err error
// 				expected, err = hex.DecodeString(method.Identifier)
// 				if err != nil {
// 					return fmt.Errorf("invalid expected discriminator: %w", err)
// 				}
// 				break
// 			}
// 		}
// 		if expected == nil {
// 			return fmt.Errorf("add_funds method not found in config")
// 		}
// 		if !bytes.Equal(actual, expected) {
// 			return fmt.Errorf("discriminator mismatch: expected %x, got %x", expected, actual)
// 		}
// 		return nil // ✅ Valid instruction found
// 	}
// 	return fmt.Errorf("no instruction found calling gateway address %s", chainConfig.GatewayAddress)
// }

// // Checks if a given svm tx hash has enough confirmations
// func CheckSVMBlockConfirmations(
// 	ctx context.Context,
// 	txHashBase58 string,
// 	rpcCfg rpc.RpcCallConfig,
// 	requiredConfirmations uint64,
// ) error {
// 	// Fetch transaction receipt
// 	tx, err := svmrpc.SVMGetTransactionBySig(ctx, rpcCfg, txHashBase58)
// 	if err != nil {
// 		return fmt.Errorf("fetch tx failed: %w", err)
// 	}

// 	currentSlot, err := svmrpc.SVMGetCurrentSlot(ctx, rpcCfg)
// 	if err != nil {
// 		return fmt.Errorf("fetch current slot failed: %w", err)
// 	}

// 	confirmations := currentSlot - tx.Slot
// 	if confirmations < uint64(requiredConfirmations) {
// 		return fmt.Errorf("insufficient confirmations: got %d, need %d", confirmations, requiredConfirmations)
// 	}

// 	return nil
// }

// ParseSVMFundsAddedLog parses Solana log messages to extract the FundsAddedEvent
// @param logMessages Program logs from the transaction
// @param expectedDiscriminator First 8 bytes of event identifier
// @return event data or error if not found or corrupted
func ParseSVMFundsAddedEventLog(
	logMessages []string,
	expectedDiscriminator []byte,
) ([]utxverifiertypes.SVMFundsAddedEventData, error) {

	var results []utxverifiertypes.SVMFundsAddedEventData

	for _, log := range logMessages {
		if strings.HasPrefix(log, "Program data: ") {
			encoded := strings.TrimPrefix(log, "Program data: ")

			// Decode base64
			raw, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil || len(raw) < 92 {
				continue
			}

			// Check discriminator match
			if !bytes.Equal(raw[:8], expectedDiscriminator) {
				continue
			}

			eventBytes := raw[8:]

			// Check event length for all fields: 32 + 8 + 16 + 4 + 32 = 92 bytes
			if len(eventBytes) < 92 {
				continue
			}

			usdAmount := readI128LE(eventBytes[40:56])
			usdExponent := int32(binary.LittleEndian.Uint32(eventBytes[56:60]))
			txHashBytes := eventBytes[60:92]

			// Normalize exponent into decimals
			var decimals uint32
			if usdExponent < 0 {
				decimals = uint32(-usdExponent)
				// no scaling needed
			} else {
				decimals = uint32(usdExponent)
				multiplier := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(usdExponent)), nil)
				usdAmount = new(big.Int).Mul(usdAmount, multiplier)
			}

			results = append(results, utxverifiertypes.SVMFundsAddedEventData{
				AmountInUSD: usdAmount,
				Decimals:    decimals,
				PayloadHash: fmt.Sprintf("0x%x", txHashBytes),
			})
		}
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("amount not found with expected discriminator %s", expectedDiscriminator)
	}

	return results, nil
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
