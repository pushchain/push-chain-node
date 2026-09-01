package types_test

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	evmtypes "github.com/cosmos/evm/x/vm/types"

	"github.com/pushchain/push-chain-node/x/ucallback/types"
)

// readSpec mirrors the contract's ReadSpec for encoding test fixtures.
type account struct {
	ChainNamespace string
	ChainId        string
	Owner          []byte
}

type readSpec struct {
	Account               account
	Query                 []byte
	MinConfirmations      uint16
	BlockNumber           uint64
	ExpiryPushChainHeight uint64
	MaxFee                *big.Int
	RevertRecipient       common.Address
}

func encodeLog(t *testing.T, spec readSpec, fees *big.Int, requestID, target, funder string) *evmtypes.Log {
	t.Helper()
	// totalPaid = protocolFee + callbackBudget; the split mirrors the contract.
	protocolFee := new(big.Int).Div(fees, big.NewInt(2))
	budget := new(big.Int).Sub(fees, protocolFee)
	data, err := types.ReadRequestedEventInputs().NonIndexed().
		Pack(spec, uint64(250_000), fees, protocolFee, budget)
	require.NoError(t, err)

	return &evmtypes.Log{
		Address: "0x00000000000000000000000000000000000000C2",
		Topics: []string{
			types.ReadRequestedEventSig.Hex(),
			requestID,
			common.HexToHash(target).Hex(),
			common.HexToHash(funder).Hex(),
		},
		Data: data,
	}
}

func sampleSpec() readSpec {
	return readSpec{
		Account: account{
			ChainNamespace: "eip155",
			ChainId:        "11155111",
			Owner:          common.FromHex("0x1111111111111111111111111111111111111111"),
		},
		Query:                 common.FromHex("0xdeadbeef"),
		MinConfirmations:      12,
		BlockNumber:           8_000_123,
		ExpiryPushChainHeight: 900_000,
		MaxFee:                big.NewInt(5_000_000),
		RevertRecipient:       common.HexToAddress("0x4444444444444444444444444444444444444444"),
	}
}

// A round-trip through real ABI encoding — if the ABI fragment in read_event.go
// disagrees with the contract's struct layout, this fails.
func TestDecodeReadRequestedFromLog_RoundTrip(t *testing.T) {
	spec := sampleSpec()
	lg := encodeLog(t, spec, big.NewInt(42_000),
		"0x00000000000000000000000000000000000000000000000000000000000000ab",
		"0x2222222222222222222222222222222222222222",
		"0x3333333333333333333333333333333333333333")

	ev, err := types.DecodeReadRequestedFromLog(lg)
	require.NoError(t, err)

	require.Equal(t, "eip155", ev.ChainNamespace)
	require.Equal(t, "11155111", ev.ChainID)
	require.Equal(t, "eip155:11155111", ev.DestinationChain())
	require.Equal(t, common.FromHex("0x1111111111111111111111111111111111111111"), ev.Owner)
	require.Equal(t, common.FromHex("0xdeadbeef"), ev.Query)
	require.Equal(t, uint16(12), ev.MinConfirmations)
	require.Equal(t, uint64(8_000_123), ev.BlockNumber)
	require.Equal(t, uint64(900_000), ev.ExpiryPushChainHeight)
	require.Equal(t, big.NewInt(5_000_000), ev.MaxFee)
	require.Equal(t, "0x4444444444444444444444444444444444444444", ev.RevertRecipient)

	// the fee split arrives whole, and the two parts must reconstitute the total
	require.Equal(t, big.NewInt(42_000), ev.TotalPaid)
	require.Equal(t, big.NewInt(21_000), ev.ProtocolFee)
	require.Equal(t, big.NewInt(21_000), ev.CallbackBudget)
	require.Equal(t, new(big.Int).Add(ev.ProtocolFee, ev.CallbackBudget), ev.TotalPaid)
	require.Equal(t, uint64(250_000), ev.CallbackGasLimit)

	require.Equal(t, "0x2222222222222222222222222222222222222222", ev.CallbackTarget)
	require.Equal(t, "0x3333333333333333333333333333333333333333", ev.OriginalFunder)
	require.Equal(t,
		"0x00000000000000000000000000000000000000000000000000000000000000ab",
		ev.RequestID, "request id keeps the full 32-byte topic, lowercased")
}

// expectedReadRequestedTopic0 is keccak of
//
//	ReadRequested(uint256,((string,string,bytes),bytes,uint16,uint64,uint64,uint256,address),
//	              address,address,uint64,uint256,uint256,uint256)
//
// derived independently of the ABI fragment. If the two disagree, one of them has
// drifted from the contract — and a wrong topic0 means ingestion silently matches
// nothing rather than failing.
const expectedReadRequestedTopic0 = "0x4eff8080da7bb648f5eed3bfbb21041b583987e36c6808f5483bd6cf9e160160"

// topic0 must be derived, never hand-written: a typo would silently produce a
// filter that matches nothing.
func TestReadRequestedEventSig_IsStable(t *testing.T) {
	require.Equal(t,
		expectedReadRequestedTopic0,
		types.ReadRequestedEventSig.Hex(),
		"topic0 changed — the contract's event signature moved, or the ABI fragment drifted")
}

func TestDecodeReadRequestedFromLog_Rejects(t *testing.T) {
	spec := sampleSpec()
	good := encodeLog(t, spec, big.NewInt(1),
		"0x00000000000000000000000000000000000000000000000000000000000000ab",
		"0x2222222222222222222222222222222222222222",
		"0x3333333333333333333333333333333333333333")

	t.Run("nil log", func(t *testing.T) {
		_, err := types.DecodeReadRequestedFromLog(nil)
		require.Error(t, err)
	})

	t.Run("wrong topic0", func(t *testing.T) {
		bad := *good
		bad.Topics = append([]string{}, good.Topics...)
		bad.Topics[0] = common.HexToHash("0xdead").Hex()
		_, err := types.DecodeReadRequestedFromLog(&bad)
		require.Error(t, err)
	})

	t.Run("too few topics", func(t *testing.T) {
		bad := *good
		bad.Topics = good.Topics[:3]
		_, err := types.DecodeReadRequestedFromLog(&bad)
		require.Error(t, err)
	})

	t.Run("truncated data", func(t *testing.T) {
		bad := *good
		bad.Data = good.Data[:len(good.Data)/2]
		_, err := types.DecodeReadRequestedFromLog(&bad)
		require.Error(t, err)
	})
}

// Empty owner/query are legitimate on some chains; they must not be an error.
func TestDecodeReadRequestedFromLog_EmptyBytes(t *testing.T) {
	spec := sampleSpec()
	spec.Account.Owner = []byte{}
	spec.Query = []byte{}
	spec.MaxFee = big.NewInt(0)

	lg := encodeLog(t, spec, big.NewInt(0),
		"0x0000000000000000000000000000000000000000000000000000000000000001",
		"0x2222222222222222222222222222222222222222",
		"0x3333333333333333333333333333333333333333")

	ev, err := types.DecodeReadRequestedFromLog(lg)
	require.NoError(t, err)
	require.Empty(t, ev.Owner)
	require.Empty(t, ev.Query)
}
