package types

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	evmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

type UniversalTxOutboundEvent struct {
	TxID            string   // 0x... bytes32
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
	GasPrice        *big.Int // gas price on destination chain at time of outbound
}

func DecodeUniversalTxOutboundFromLog(log *evmtypes.Log) (*UniversalTxOutboundEvent, error) {
	if len(log.Topics) == 0 || log.Topics[0] != UniversalTxOutboundEventSig {
		return nil, fmt.Errorf("not a UniversalTxOutbound event")
	}
	if len(log.Topics) < 4 {
		return nil, fmt.Errorf("insufficient topics")
	}

	event := &UniversalTxOutboundEvent{
		TxID:   log.Topics[1],
		Sender: common.HexToAddress(log.Topics[2]).Hex(),
		Token:  common.HexToAddress(log.Topics[3]).Hex(),
	}

	// ABI types
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
		{Type: uint8Type},   // txType
		{Type: uint256Type}, // gasPrice
	}

	values, err := arguments.Unpack(log.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to unpack UniversalTxOutbound: %w", err)
	}

	if len(values) != 11 {
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
	i++
	event.GasPrice = values[i].(*big.Int)

	return event, nil
}

// RescueFundsOnSourceChainEvent holds decoded data from the RescueFundsOnSourceChain
// event emitted by UniversalGatewayPC when a user initiates a rescue on the source chain.
type RescueFundsOnSourceChainEvent struct {
	UniversalTxId  string   // 0x-prefixed bytes32 — the original UTX whose funds are stuck
	PRC20          string   // 0x-prefixed address — PRC20 token whose counterpart is locked
	ChainNamespace string   // source chain namespace (e.g. "eip155")
	Sender         string   // 0x-prefixed address — user who initiated the rescue
	TxType         TxType   // always TxType_RESCUE_FUNDS
	GasFee         *big.Int // gas fee charged (in gas-token units)
	GasPrice       *big.Int // gas price on the source chain
	GasLimit       *big.Int // gas limit used for the rescue execution
}

// DecodeRescueFundsOnSourceChainFromLog decodes a RescueFundsOnSourceChain event log.
//
// Event signature:
//
//	RescueFundsOnSourceChain(bytes32 indexed universalTxId, address indexed prc20,
//	  string chainNamespace, address indexed sender, uint8 txType,
//	  uint256 gasFee, uint256 gasPrice, uint256 gasLimit)
//
// Topics: [sig, universalTxId, prc20, sender]
// Data:   [chainNamespace, txType, gasFee, gasPrice, gasLimit]
func DecodeRescueFundsOnSourceChainFromLog(log *evmtypes.Log) (*RescueFundsOnSourceChainEvent, error) {
	if len(log.Topics) < 4 {
		return nil, fmt.Errorf("RescueFundsOnSourceChain: need 4 topics, got %d", len(log.Topics))
	}
	if strings.ToLower(log.Topics[0]) != strings.ToLower(RescueFundsOnSourceChainEventSig) {
		return nil, fmt.Errorf("not a RescueFundsOnSourceChain event")
	}

	event := &RescueFundsOnSourceChainEvent{
		UniversalTxId: log.Topics[1], // bytes32 as 0x-prefixed hex
		PRC20:         common.HexToAddress(log.Topics[2]).Hex(),
		Sender:        common.HexToAddress(log.Topics[3]).Hex(),
	}

	stringType, _ := abi.NewType("string", "", nil)
	uint8Type, _ := abi.NewType("uint8", "", nil)
	uint256Type, _ := abi.NewType("uint256", "", nil)

	arguments := abi.Arguments{
		{Type: stringType},  // chainNamespace
		{Type: uint8Type},   // txType
		{Type: uint256Type}, // gasFee
		{Type: uint256Type}, // gasPrice
		{Type: uint256Type}, // gasLimit
	}

	values, err := arguments.Unpack(log.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to unpack RescueFundsOnSourceChain data: %w", err)
	}
	if len(values) != 5 {
		return nil, fmt.Errorf("RescueFundsOnSourceChain: unexpected value count %d", len(values))
	}

	event.ChainNamespace = values[0].(string)
	event.TxType = SolidityTxTypeToProto(values[1].(uint8))
	event.GasFee = values[2].(*big.Int)
	event.GasPrice = values[3].(*big.Int)
	event.GasLimit = values[4].(*big.Int)

	return event, nil
}
