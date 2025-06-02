package usv

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"

	"github.com/ethereum/go-ethereum/accounts/abi"
)

const VerifyEd25519Method = "verifyEd25519"

func (p Precompile) VerifyEd25519(
	method *abi.Method,
	args []interface{},
) ([]byte, error) {

	pubKey, ok := args[0].([]byte)
	if !ok {
		return nil, fmt.Errorf("invalid pubKey type")
	}

	// Convert the message bytes to a bytes32 slice
	msgRaw, ok := args[1].([32]byte)
	if !ok {
		return nil, fmt.Errorf("invalid msg type")
	}
	msg := msgRaw[:]

	signature, ok := args[2].([]byte)
	if !ok {
		return nil, fmt.Errorf("invalid signature type")
	}

	pubKeyBytes, err := getSolanaPubKeyFromAddress(pubKey)
	if err != nil {
		return nil, fmt.Errorf("failed to parse pubKey: %w", err)
	}

	if len(pubKeyBytes) != ed25519.PublicKeySize || len(signature) != ed25519.SignatureSize {
		return nil, fmt.Errorf("invalid params")
	}

	msgStr := "0x" + hex.EncodeToString(msg) // Convert the message to a hex string
	msgBytes := []byte(msgStr)               // Convert the message string to original signed bytes

	ok = ed25519.Verify(pubKeyBytes, msgBytes, signature)

	// ✨ Pack the result into EVM ABI-encoded bytes
	return method.Outputs.Pack(ok)
}

func getSolanaPubKeyFromAddress(pubKey []byte) (ed25519.PublicKey, error) {
	return ed25519.PublicKey(pubKey), nil
}
