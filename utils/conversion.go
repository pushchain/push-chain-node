package utils

import (
	"encoding/hex"
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
