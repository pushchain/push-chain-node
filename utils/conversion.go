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
	parts := strings.Split(chainID, "-")
	last := parts[len(parts)-1]

	// Ensure numeric
	if _, ok := new(big.Int).SetString(last, 10); !ok {
		return "", fmt.Errorf("invalid EVM chain id in tendermint chain-id: %s", chainID)
	}

	return last, nil
}
