package svm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecodeSolanaQueryEnvelope(t *testing.T) {
	data, err := svmEnvelopeArgs.Pack(rawSvmEnvelope{
		QueryType: uint8(solanaQuerySPLTokenAccount),
		SlotRef: struct {
			MinSlot uint64
		}{42},
		Payload: nil,
	})
	require.NoError(t, err)

	env, err := decodeSolanaQueryEnvelope(data)
	require.NoError(t, err)
	assert.Equal(t, solanaQuerySPLTokenAccount, env.QueryType)
	assert.Equal(t, uint64(42), env.MinSlot)
	assert.Empty(t, env.Payload)

	_, err = decodeSolanaQueryEnvelope([]byte{0x00})
	assert.Error(t, err)

	// unknown query type
	bad, err := svmEnvelopeArgs.Pack(rawSvmEnvelope{QueryType: 9})
	require.NoError(t, err)
	_, err = decodeSolanaQueryEnvelope(bad)
	assert.Error(t, err)
}
