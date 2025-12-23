package types

import (
	"encoding/hex"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/pkg/errors"
)

var (
	stringType, _ = abi.NewType("string", "", nil)

	// ABI arguments layout: (string utxID, string outboundID)
	txIDArgs = abi.Arguments{
		{Type: stringType},
		{Type: stringType},
	}
)

// EncodeOutboundTxIDHex returns hex string of ABI(utxID, outboundID)
func EncodeOutboundTxIDHex(utxID, outboundID string) (string, error) {
	bz, err := encodeOutboundTxID(utxID, outboundID)
	if err != nil {
		return "", err
	}
	return "0x" + hex.EncodeToString(bz), nil
}

// DecodeOutboundTxIDHex decodes a hex string into (utxID, outboundID)
func DecodeOutboundTxIDHex(txIDHex string) (string, string, error) {
	bz, err := hexStringToBytes(txIDHex)
	if err != nil {
		return "", "", err
	}
	return decodeOutboundTxID(bz)
}

// Low-level encoding (bytes)
func encodeOutboundTxID(utxID, outboundID string) ([]byte, error) {
	return txIDArgs.Pack(utxID, outboundID)
}

// Low-level decoding (bytes → strings)
func decodeOutboundTxID(bz []byte) (string, string, error) {
	values, err := txIDArgs.Unpack(bz)
	if err != nil {
		return "", "", errors.Wrap(err, "ABI decode failed")
	}

	utxID := values[0].(string)
	outID := values[1].(string)

	return utxID, outID, nil
}

// Converts "0x…" or "…" into bytes
func hexStringToBytes(input string) ([]byte, error) {
	if input == "" {
		return nil, errors.New("empty tx_id")
	}

	// Normalize 0x prefix
	if strings.HasPrefix(input, "0x") || strings.HasPrefix(input, "0X") {
		input = input[2:]
	}

	bz, err := hex.DecodeString(input)
	if err != nil {
		return nil, errors.Wrap(err, "invalid hex in tx_id")
	}

	return bz, nil
}
