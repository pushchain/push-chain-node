package types

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/decred/base58"
	uetypes "github.com/pushchain/push-chain-node/x/ue/types"
)

func NormalizeTxHash(txHash string, vmType uetypes.VM_TYPE) (string, error) {
	txHash = strings.TrimSpace(txHash)

	// Strip "0x" prefix if present
	if strings.HasPrefix(txHash, "0x") {
		txHash = txHash[2:]
	}

	// Decode the hex string into bytes
	decodedBytes, err := hex.DecodeString(txHash)
	if err != nil {
		return "", fmt.Errorf("invalid hex input: %w", err)
	}

	switch vmType {
	case uetypes.VM_TYPE_EVM:
		if len(decodedBytes) != 32 {
			return "", fmt.Errorf("invalid EVM tx hash length: got %d bytes, expected 32", len(decodedBytes))
		}
		// Reconstruct normalized hex with 0x and lowercase
		return "0x" + txHash, nil

	case uetypes.VM_TYPE_SVM:
		if len(decodedBytes) != 64 {
			return "", fmt.Errorf("invalid Solana tx signature: expected 64 bytes, got %d", len(decodedBytes))
		}
		// Encode to base58
		return base58.Encode(decodedBytes), nil

	default:
		return "", fmt.Errorf("unsupported VM type: %s", vmType.String())
	}
}
