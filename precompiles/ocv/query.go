package ocv

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/accounts/abi"
	uetypes "github.com/rollchains/pchain/x/ue/types"
)

const VerifyTxHashMethod = "verifyTxHash"

func (p Precompile) VerifyTxHash(
	method *abi.Method,
	args []interface{},
) ([]byte, error) {
	if len(args) != 3 {
		return nil, fmt.Errorf("expected 3 args, got %d", len(args))
	}

	// Parse UniversalAccountId from EVM
	universalAccountIdRaw, ok := args[0].(struct {
		ChainNamespace string `json:"chainNamespace"`
		ChainId        string `json:"chainId"`
		Owner          []byte `json:"owner"`
	})
	if !ok {
		return nil, fmt.Errorf("invalid UniversalAccountId type: expected struct, got %T", args[0])
	}

	// Parse payloadHash
	payloadHashBytes, ok := args[1].([]byte)
	if !ok {
		return nil, fmt.Errorf("invalid payloadHash type")
	}

	// Parse txHash as string (standardized for both EVM and Solana)
	txHash, ok := args[2].(string)
	if !ok {
		return nil, fmt.Errorf("invalid txHash type")
	}

	// Convert payloadHash bytes to hex string
	payloadHash := fmt.Sprintf("0x%x", payloadHashBytes)
	ownerHex := fmt.Sprintf("0x%x", universalAccountIdRaw.Owner)

	fmt.Printf("[OCV] VerifyTxHash called with UniversalAccountId: chainNamespace=%s, chainId=%s, owner=%s, payloadHash=%s, txHash=%s\n",
		universalAccountIdRaw.ChainNamespace, universalAccountIdRaw.ChainId, ownerHex, payloadHash, txHash)

	if p.utvKeeper == nil {
		fmt.Printf("[OCV] No UTV keeper\n")
		return method.Outputs.Pack(false)
	}

	fmt.Printf("[OCV] Delegating verification to UTV module for gas efficiency\n")

	// Use background context since precompiles can't easily access current context
	ctx := context.Background()

	// Convert EVM UniversalAccountId to UE module format
	universalAccountId := uetypes.UniversalAccount{
		Chain: fmt.Sprintf("%s:%s", universalAccountIdRaw.ChainNamespace, universalAccountIdRaw.ChainId),
		Owner: ownerHex,
	}

	// Delegate all verification to UTV module (much more gas efficient)
	isValid, err := p.utvKeeper.VerifyTxHashWithPayload(ctx, universalAccountId, payloadHash, txHash)
	if err != nil {
		fmt.Printf("[OCV] Verification failed: %v\n", err)
		return method.Outputs.Pack(false)
	}

	if isValid {
		fmt.Printf("[OCV] ✅ Verification successful via UTV module\n")
	} else {
		fmt.Printf("[OCV] ❌ Verification failed via UTV module\n")
	}

	return method.Outputs.Pack(isValid)
}
