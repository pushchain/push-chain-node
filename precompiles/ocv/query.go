package ocv

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	uetypes "github.com/pushchain/push-chain-node/x/ue/types"
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

	fmt.Printf("[OCV] VerifyTxHash called with chainNamespace=%s, chainId=%s, owner=%s, payloadHash=%s, txHash=%s\n",
		chainNamespace, chainId, ownerHex, payloadHash, txHash)

	fmt.Printf("[OCV] Delegating verification to UTV module for gas efficiency\n")

	// Convert to UE module format
	universalAccountId := uetypes.UniversalAccountId{
		ChainNamespace: chainNamespace,
		ChainId:        chainId,
		Owner:          ownerHex,
	}

	// Build full chain caip2: "namespace:chain"
	chainCaip2 := fmt.Sprintf("%s:%s", universalAccountId.ChainNamespace, universalAccountId.ChainId)

	// Delegate all verification to UTV module (much more gas efficient)
	verifiedPayload, err := p.utvKeeper.VerifyAndGetPayloadHash(ctx, universalAccountId.Owner, txHash, chainCaip2)
	if err != nil {
		fmt.Printf("[OCV] Verification failed: %v\n", err)
		return method.Outputs.Pack(false)
	}

	if verifiedPayload != payloadHash {
		fmt.Printf("[OCV] Payload mismatch: expected %s, got %s\n", payloadHash, verifiedPayload)
		return method.Outputs.Pack(false)
	}

	fmt.Printf("[OCV] ✅ Verification successful via UTV module\n")

	return method.Outputs.Pack(true)
}
