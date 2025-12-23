package utils

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
)

func HexToBytes(s string) ([]byte, error) {
	return hex.DecodeString(strings.TrimPrefix(s, "0x"))
}

func StringToBigInt(s string) *big.Int {
	bi, _ := new(big.Int).SetString(s, 10)
	return bi
}

// Returns evm chainId, e.g. push-chain-42101 -> 42101
func ExtractEvmChainID(chainID string) (string, error) {
	parts := strings.Split(chainID, "_")
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid chain-id format: %s", chainID)
	}

	idPart := parts[1]
	idParts := strings.Split(idPart, "-")
	if len(idParts) < 1 {
		return "", fmt.Errorf("invalid chain-id format: %s", chainID)
	}

	evmChainID := idParts[0]

	// Ensure numeric
	if _, ok := new(big.Int).SetString(evmChainID, 10); !ok {
		return "", fmt.Errorf("invalid EVM chain id in tendermint chain-id: %s", chainID)
	}

	return evmChainID, nil
}
