package common

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncodeUint256Result(t *testing.T) {
	out, err := EncodeUint256Result(big.NewInt(1_000_000))
	require.NoError(t, err)
	require.Len(t, out, 32)
	assert.Equal(t, big.NewInt(1_000_000), new(big.Int).SetBytes(out))

	out, err = EncodeUint256Result(nil)
	require.NoError(t, err)
	assert.True(t, bytes.Equal(out, make([]byte, 32)))

	_, err = EncodeUint256Result(big.NewInt(-1))
	assert.Error(t, err)
}

func TestEncodeBytes32Result(t *testing.T) {
	var v [32]byte
	v[31] = 0xff
	out, err := EncodeBytes32Result(v)
	require.NoError(t, err)
	require.Len(t, out, 32)
	assert.Equal(t, v[:], out)
}
