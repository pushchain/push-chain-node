package evm

import (
	"testing"

	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func packEvmEnvelope(t *testing.T, queryType, refType uint8, blockNumber uint64, payload []byte) []byte {
	t.Helper()
	data, err := evmEnvelopeArgs.Pack(rawEvmEnvelope{
		QueryType: queryType,
		BlockRef: struct {
			RefType     uint8
			BlockNumber uint64
		}{refType, blockNumber},
		Payload: payload,
	})
	require.NoError(t, err)
	return data
}

func TestDecodeEvmQueryEnvelope(t *testing.T) {
	target := ethcommon.HexToAddress("0x1111111111111111111111111111111111111111")
	payload, err := addressArgs.Pack(target)
	require.NoError(t, err)

	env, err := decodeEvmQueryEnvelope(packEvmEnvelope(t, uint8(evmQueryAccountBalance), 0, 1234, payload))
	require.NoError(t, err)
	assert.Equal(t, evmQueryAccountBalance, env.QueryType)
	assert.Equal(t, evmBlockRefAtNumber, env.RefType)
	assert.Equal(t, uint64(1234), env.BlockNumber)

	decoded, err := decodeAccountBalancePayload(env.Payload)
	require.NoError(t, err)
	assert.Equal(t, target, decoded)
}

func TestDecodeEvmQueryEnvelope_Invalid(t *testing.T) {
	_, err := decodeEvmQueryEnvelope([]byte{0x01, 0x02})
	assert.Error(t, err)

	// unknown query type
	_, err = decodeEvmQueryEnvelope(packEvmEnvelope(t, 9, 0, 0, nil))
	assert.Error(t, err)

	// unknown block ref type
	_, err = decodeEvmQueryEnvelope(packEvmEnvelope(t, 0, 7, 0, nil))
	assert.Error(t, err)
}

func TestDecodeEvmPayloads(t *testing.T) {
	token := ethcommon.HexToAddress("0x2222222222222222222222222222222222222222")
	owner := ethcommon.HexToAddress("0x3333333333333333333333333333333333333333")

	erc20Payload, err := addressPairArgs.Pack(token, owner)
	require.NoError(t, err)
	gotToken, gotOwner, err := decodeERC20BalancePayload(erc20Payload)
	require.NoError(t, err)
	assert.Equal(t, token, gotToken)
	assert.Equal(t, owner, gotOwner)

	callData := []byte{0xde, 0xad, 0xbe, 0xef}
	callPayload, err := addressBytesArgs.Pack(token, callData)
	require.NoError(t, err)
	gotTarget, gotData, err := decodeContractCallPayload(callPayload)
	require.NoError(t, err)
	assert.Equal(t, token, gotTarget)
	assert.Equal(t, callData, gotData)

	slot := [32]byte{0x0a}
	slotPayload, err := addressBytes32Args.Pack(token, slot)
	require.NoError(t, err)
	gotAddr, gotSlot, err := decodeStorageSlotPayload(slotPayload)
	require.NoError(t, err)
	assert.Equal(t, token, gotAddr)
	assert.Equal(t, ethcommon.Hash(slot), gotSlot)
}
