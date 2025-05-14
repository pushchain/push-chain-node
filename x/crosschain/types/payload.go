package types

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/rollchains/pchain/util"
)

type AbiCrossChainPayload struct {
	Target               common.Address
	Value                *big.Int
	Data                 []byte
	GasLimit             *big.Int
	MaxFeePerGas         *big.Int
	MaxPriorityFeePerGas *big.Int
	Nonce                *big.Int
	Deadline             *big.Int
}

func NewAbiCrossChainPayload(proto *CrossChainPayload) (AbiCrossChainPayload, error) {
	data, err := util.HexToBytes(proto.Data)
	if err != nil {
		return AbiCrossChainPayload{}, err
	}
	return AbiCrossChainPayload{
		Target:               common.HexToAddress(proto.Target),
		Value:                util.StringToBigInt(proto.Value),
		Data:                 data,
		GasLimit:             util.StringToBigInt(proto.GasLimit),
		MaxFeePerGas:         util.StringToBigInt(proto.MaxFeePerGas),
		MaxPriorityFeePerGas: util.StringToBigInt(proto.MaxPriorityFeePerGas),
		Nonce:                util.StringToBigInt(proto.Nonce),
		Deadline:             util.StringToBigInt(proto.Deadline),
	}, nil
}
