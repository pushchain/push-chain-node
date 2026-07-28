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
