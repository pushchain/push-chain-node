package types

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	evmtypes "github.com/cosmos/evm/x/vm/types"
)

// readRequestedABI is the ABI fragment for UniversalCallback's ReadRequested
// event, transcribed from push-chain-core-contracts:
//
//	src/Interfaces/IUniversalCallback.sol  — the event
//	src/libraries/ReadTypes.sol            — ReadSpec
//	src/libraries/Types.sol                — UniversalAccountId
//
// Held as ABI JSON rather than hand-assembled abi.Type values so that go-ethereum
// derives topic0 for us. A hand-written signature string would be one silent typo
// away from a filter that never matches anything.
const readRequestedABI = `[{
  "type": "event",
  "name": "ReadRequested",
  "anonymous": false,
  "inputs": [
    {"name": "requestId", "type": "uint256", "indexed": true},
    {"name": "readSpec", "type": "tuple", "indexed": false, "components": [
      {"name": "account", "type": "tuple", "components": [
        {"name": "chainNamespace", "type": "string"},
        {"name": "chainId", "type": "string"},
        {"name": "owner", "type": "bytes"}
      ]},
      {"name": "query", "type": "bytes"},
      {"name": "minConfirmations", "type": "uint16"},
      {"name": "blockNumber", "type": "uint64"},
      {"name": "expiryPushChainHeight", "type": "uint64"},
      {"name": "maxFee", "type": "uint256"}
    ]},
    {"name": "callbackTarget", "type": "address", "indexed": true},
    {"name": "originalFunder", "type": "address", "indexed": true},
    {"name": "feesDeposited", "type": "uint256", "indexed": false}
  ]
}]`

var (
	readRequestedEvent abi.Event
	// ReadRequestedEventSig is topic0 for ReadRequested, derived from the ABI above.
	ReadRequestedEventSig common.Hash
)

func init() {
	parsed, err := abi.JSON(strings.NewReader(readRequestedABI))
	if err != nil {
		panic(fmt.Sprintf("ucallback: bad ReadRequested ABI: %v", err))
	}
	ev, ok := parsed.Events["ReadRequested"]
	if !ok {
		panic("ucallback: ReadRequested missing from parsed ABI")
	}
	readRequestedEvent = ev
	ReadRequestedEventSig = ev.ID
}

// ReadRequestedEvent is a decoded ReadRequested log.
//
// RequestID keeps the raw 32-byte topic hex rather than a decimal string: it is
// handed straight back to the contract as a uint256 on fulfil/expire, and the hex
// form round-trips without a base conversion in the middle.
type ReadRequestedEvent struct {
	RequestID      string
	CallbackTarget string
	OriginalFunder string

	ChainNamespace string
	ChainID        string
	Owner          []byte

	Query                 []byte
	MinConfirmations      uint16
	BlockNumber           uint64
	ExpiryPushChainHeight uint64
	MaxFee                *big.Int

	FeesDeposited *big.Int
}

// DestinationChain returns the CAIP-2 identifier the event's account refers to,
// e.g. "eip155:11155111". The contract emits namespace and id separately; every
// other module keys chains by the joined form.
func (e *ReadRequestedEvent) DestinationChain() string {
	return e.ChainNamespace + ":" + e.ChainID
}

// unpackTarget mirrors the non-indexed argument layout of ReadRequested. Field
// order and types must match the ABI above exactly; go-ethereum maps by position
// within each tuple, not by name.
type unpackTarget struct {
	ReadSpec struct {
		Account struct {
			ChainNamespace string
			ChainId        string
			Owner          []byte
		}
		Query                 []byte
		MinConfirmations      uint16
		BlockNumber           uint64
		ExpiryPushChainHeight uint64
		MaxFee                *big.Int
	}
	FeesDeposited *big.Int
}

// DecodeReadRequestedFromLog decodes a ReadRequested log.
//
// The caller is responsible for having checked log.Address — this function only
// validates the topic layout, so on its own it would happily decode a forged event
// from an arbitrary contract.
func DecodeReadRequestedFromLog(log *evmtypes.Log) (*ReadRequestedEvent, error) {
	if log == nil {
		return nil, fmt.Errorf("nil log")
	}
	if len(log.Topics) != 4 {
		return nil, fmt.Errorf("ReadRequested expects 4 topics, got %d", len(log.Topics))
	}
	if !strings.EqualFold(log.Topics[0], ReadRequestedEventSig.Hex()) {
		return nil, fmt.Errorf("not a ReadRequested event")
	}

	var out unpackTarget
	values, err := readRequestedEvent.Inputs.NonIndexed().Unpack(log.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to unpack ReadRequested: %w", err)
	}
	if err := readRequestedEvent.Inputs.NonIndexed().Copy(&out, values); err != nil {
		return nil, fmt.Errorf("failed to map ReadRequested fields: %w", err)
	}

	return &ReadRequestedEvent{
		RequestID:      strings.ToLower(log.Topics[1]),
		CallbackTarget: common.HexToAddress(log.Topics[2]).Hex(),
		OriginalFunder: common.HexToAddress(log.Topics[3]).Hex(),

		ChainNamespace: out.ReadSpec.Account.ChainNamespace,
		ChainID:        out.ReadSpec.Account.ChainId,
		Owner:          out.ReadSpec.Account.Owner,

		Query:                 out.ReadSpec.Query,
		MinConfirmations:      out.ReadSpec.MinConfirmations,
		BlockNumber:           out.ReadSpec.BlockNumber,
		ExpiryPushChainHeight: out.ReadSpec.ExpiryPushChainHeight,
		MaxFee:                out.ReadSpec.MaxFee,

		FeesDeposited: out.FeesDeposited,
	}, nil
}
