package utxhashverifier

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

const VerifyTxHashMethod = "verifyTxHash"

func (p Precompile) VerifyTxHash(
	ctx sdk.Context,
	method *abi.Method,
	args []interface{},
) ([]byte, error) {
	if len(args) != 5 {
		return nil, fmt.Errorf("expected 5 args, got %d", len(args))
	}

	// Parse individual parameters
	chainNamespace, ok := args[0].(string)
	if !ok {
		return nil, fmt.Errorf("invalid chainNamespace type: expected string, got %T", args[0])
	}

	chainId, ok := args[1].(string)
	if !ok {
		return nil, fmt.Errorf("invalid chainId type: expected string, got %T", args[1])
	}

	owner, ok := args[2].([]byte)
	if !ok {
		return nil, fmt.Errorf("invalid owner type: expected []byte, got %T", args[2])
	}

	// Parse payloadHash as bytes32 (will be [32]byte)
	payloadHashBytes32, ok := args[3].([32]byte)
	if !ok {
		return nil, fmt.Errorf("invalid payloadHash type: expected [32]byte, got %T", args[3])
	}

	// Parse txHash as bytes
	txHashBytes, ok := args[4].([]byte)
	if !ok {
		return nil, fmt.Errorf("invalid txHash type: expected []byte, got %T", args[4])
	}

	// Convert bytes32 to hex string
	payloadHash := fmt.Sprintf("0x%x", payloadHashBytes32[:])
	ownerHex := fmt.Sprintf("0x%x", owner)
	txHash := fmt.Sprintf("0x%x", txHashBytes)

	fmt.Printf("[UTxHashVerifier] VerifyTxHash called with chainNamespace=%s, chainId=%s, owner=%s, payloadHash=%s, txHash=%s\n",
		chainNamespace, chainId, ownerHex, payloadHash, txHash)

	fmt.Printf("[UTxHashVerifier] Delegating verification to UtxverifierKeeper moduledule for gas efficiency\n")

	// Convert to Uexecutor module format
	universalAccountId := uexecutortypes.UniversalAccountId{
		ChainNamespace: chainNamespace,
		ChainId:        chainId,
		Owner:          ownerHex,
	}

	// Build full chain caip2: "namespace:chain"
	chainCaip2 := fmt.Sprintf("%s:%s", universalAccountId.ChainNamespace, universalAccountId.ChainId)

	// Delegate all verification to UtxverifierKeeper moduledule (much more gas efficient)
	verifiedPayload, err := p.utxverifierKeeper.VerifyAndGetPayloadHash(ctx, universalAccountId.Owner, txHash, chainCaip2)
	if err != nil {
		fmt.Printf("[UTxHashVerifier] Verification failed: %v\n", err)
		return method.Outputs.Pack(false)
	}

	matched := false
	for _, ph := range verifiedPayload {
		if ph == payloadHash {
			matched = true
			break
		}
	}

	if !matched {
		fmt.Printf("[UTxHashVerifier] Payload mismatch: expected %s, got %v\n", payloadHash, verifiedPayload)
		return method.Outputs.Pack(false)
	}

	fmt.Printf("[UTxHashVerifier] ✅ Verification successful via UtxverifierKeeper moduledule\n")

	return method.Outputs.Pack(true)
}
