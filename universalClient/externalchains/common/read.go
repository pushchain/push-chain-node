package common

import (
	"context"
	"fmt"
	"math/big"

	"github.com/pushchain/push-chain-node/universalClient/uread"
)

// ChainReader executes an external read request against one chain.
// Implemented by chains/evm.Client and chains/svm.Client.
type ChainReader interface {
	ExecuteRead(ctx context.Context, req *uread.ReadRequest) (*uread.ReadResult, error)
}

// ReadStoreResolver resolves a CAIP-2 chain ID to that chain's event store, so
// READ_REQUEST events can be routed into the target chain's own database.
// Implemented by externalchains.Chains.
type ReadStoreResolver interface {
	GetStore(chainID string) (*ChainStore, error)
}

// CAIP2 joins a ReadSpec domain (chainNamespace, chainId) into the CAIP-2 key
// used by the chains registry, e.g. ("eip155", "1") -> "eip155:1".
func CAIP2(chainNamespace, chainID string) (string, error) {
	if chainNamespace == "" || chainID == "" {
		return "", fmt.Errorf("empty chain namespace or id")
	}
	return chainNamespace + ":" + chainID, nil
}

// EncodeUint256Result canonically encodes a balance/amount as abi.encode(uint256).
func EncodeUint256Result(v *big.Int) ([]byte, error) {
	if v == nil {
		v = big.NewInt(0)
	}
	if v.Sign() < 0 || v.BitLen() > 256 {
		return nil, fmt.Errorf("value out of uint256 range")
	}
	out := make([]byte, 32)
	v.FillBytes(out)
	return out, nil
}

// EncodeBytes32Result canonically encodes a storage slot value as abi.encode(bytes32).
func EncodeBytes32Result(v [32]byte) ([]byte, error) {
	out := make([]byte, 32)
	copy(out, v[:])
	return out, nil
}
