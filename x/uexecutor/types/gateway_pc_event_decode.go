package types

import (
	"encoding/hex"
	"fmt"
	"math/big"

	evmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

type UniversalTxWithdrawEvent struct {
	Sender          string   // 0x... address
	ChainId         string   // destination chain (CAIP-2 string)
	Token           string   // 0x... ERC20 or zero address for native
	Target          string   // 0x-hex encoded bytes (non-EVM recipient)
	Amount          *big.Int // amount of Token to bridge
	GasToken        string   // 0x... token used to pay gas fee
	GasFee          *big.Int // amount of GasToken paid to relayer
	GasLimit        *big.Int // gas limit for destination execution
	Payload         string   // 0x-hex calldata
	ProtocolFee     *big.Int // fee kept by protocol
	RevertRecipient string   // where funds go on full revert
	TxType          TxType   // ← single source of truth from proto
}

func DecodeUniversalTxWithdrawFromLog(log *evmtypes.Log) (*UniversalTxWithdrawEvent, error) {
	if len(log.Topics) == 0 || log.Topics[0] != UniversalTxWithdrawEventSig {
		return nil, fmt.Errorf("not a UniversalTxWithdraw event")
	}
	if len(log.Topics) < 3 {
		return nil, fmt.Errorf("insufficient topics")
	}

	event := &UniversalTxWithdrawEvent{
		Sender: common.HexToAddress(log.Topics[1]).Hex(),
		Token:  common.HexToAddress(log.Topics[2]).Hex(),
	}

	// Define types exactly once
	stringType, _ := abi.NewType("string", "", nil)
	bytesType, _ := abi.NewType("bytes", "", nil)
	uint256Type, _ := abi.NewType("uint256", "", nil)
	addressType, _ := abi.NewType("address", "", nil)
	uint8Type, _ := abi.NewType("uint8", "", nil)

	arguments := abi.Arguments{
		{Type: stringType},  // chainId
		{Type: bytesType},   // target
		{Type: uint256Type}, // amount
		{Type: addressType}, // gasToken
		{Type: uint256Type}, // gasFee
		{Type: uint256Type}, // gasLimit
		{Type: bytesType},   // payload
		{Type: uint256Type}, // protocolFee
		{Type: addressType}, // revertRecipient
		{Type: uint8Type},   // txType ← now included!
	}

	values, err := arguments.Unpack(log.Data)
	if err != nil {
		fmt.Println(err)
		return nil, fmt.Errorf("failed to unpack UniversalTxWithdraw: %w", err)
	}

	if len(values) != 10 {
		return nil, fmt.Errorf("unexpected number of unpacked values: %d", len(values))
	}

	i := 0
	event.ChainId = values[i].(string)
	i++
	event.Target = "0x" + hex.EncodeToString(values[i].([]byte))
	i++
	event.Amount = values[i].(*big.Int)
	i++
	event.GasToken = values[i].(common.Address).Hex()
	i++
	event.GasFee = values[i].(*big.Int)
	i++
	event.GasLimit = values[i].(*big.Int)
	i++
	event.Payload = "0x" + hex.EncodeToString(values[i].([]byte))
	i++
	event.ProtocolFee = values[i].(*big.Int)
	i++
	event.RevertRecipient = values[i].(common.Address).Hex()
	i++
	event.TxType = SolidityTxTypeToProto(values[i].(uint8))

	return event, nil
}
