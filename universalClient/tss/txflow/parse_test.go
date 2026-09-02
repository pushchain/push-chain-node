package txflow

import (
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/pushchain/push-chain-node/universalClient/tss/coordinator"
)

const (
	keyAHex = "4c0883a69102937d6231471b5dbb6204fe5129617082792ae468d01a3f362318"
	keyBHex = "8a1f9a8f9c8b7d6e5f4a3b2c1d0e9f8a7b6c5d4e3f2a1b0c9d8e7f6a5b4c3d2e"
)

func signerAddr(t *testing.T, keyHex string) string {
	t.Helper()
	key, err := crypto.HexToECDSA(keyHex)
	require.NoError(t, err)
	addr, err := coordinator.DeriveEVMAddressFromPubkey(hex.EncodeToString(crypto.CompressPubkey(&key.PublicKey)))
	require.NoError(t, err)
	return addr
}

// outboundEvent builds a SIGNED outbound payload signed by keyHex, matching what
// sessionManager persists.
func outboundEvent(t *testing.T, keyHex string, nonce uint64) *store.Event {
	t.Helper()
	key, err := crypto.HexToECDSA(keyHex)
	require.NoError(t, err)
	hash := crypto.Keccak256([]byte("signing hash"))
	sig, err := crypto.Sign(hash, key)
	require.NoError(t, err)

	b, err := json.Marshal(map[string]any{
		"tx_id": "tx-1", "utx_id": "utx-1", "destination_chain": "eip155:1",
		"signing_data": map[string]any{
			"nonce":        nonce,
			"signature":    hex.EncodeToString(sig),
			"signing_hash": hex.EncodeToString(hash),
		},
	})
	require.NoError(t, err)
	return &store.Event{EventData: b}
}

func TestRecoverOutboundSigner(t *testing.T) {
	t.Run("recovers the address that signed", func(t *testing.T) {
		signer, nonce, ok := RecoverOutboundSigner(outboundEvent(t, keyAHex, 5))
		require.True(t, ok)
		assert.Equal(t, signerAddr(t, keyAHex), signer)
		assert.Equal(t, uint64(5), nonce)
	})

	// The point of the change: two keys are two EOAs with unrelated nonce
	// sequences, so the recovered signer has to follow the key that signed rather
	// than whichever key is current.
	t.Run("different keys recover to different addresses", func(t *testing.T) {
		a, _, okA := RecoverOutboundSigner(outboundEvent(t, keyAHex, 5))
		b, _, okB := RecoverOutboundSigner(outboundEvent(t, keyBHex, 5))
		require.True(t, okA)
		require.True(t, okB)
		assert.NotEqual(t, a, b)
		assert.Equal(t, signerAddr(t, keyBHex), b)
	})

	// Every failure has to report ok=false. A wrong address would be worse than
	// no address: it produces a confident answer about the wrong nonce domain.
	t.Run("unusable payloads report failure", func(t *testing.T) {
		cases := map[string]*store.Event{
			"not json":        {EventData: []byte("{")},
			"no signing data": {EventData: []byte(`{"tx_id":"tx-1"}`)},
			"short signature": {EventData: []byte(`{"signing_data":{"nonce":5,"signature":"deadbeef","signing_hash":"` +
				hex.EncodeToString(crypto.Keccak256([]byte("h"))) + `"}}`)},
			"bad hex": {EventData: []byte(`{"signing_data":{"nonce":5,"signature":"zz","signing_hash":"zz"}}`)},
			"short hash": {EventData: []byte(`{"signing_data":{"nonce":5,"signature":"` +
				hex.EncodeToString(make([]byte, 65)) + `","signing_hash":"00"}}`)},
			"unrecoverable signature": {EventData: []byte(`{"signing_data":{"nonce":5,"signature":"` +
				hex.EncodeToString(make([]byte, 65)) + `","signing_hash":"` +
				hex.EncodeToString(crypto.Keccak256([]byte("h"))) + `"}}`)},
		}
		for name, ev := range cases {
			t.Run(name, func(t *testing.T) {
				_, _, ok := RecoverOutboundSigner(ev)
				assert.False(t, ok)
			})
		}
	})

	// Same signature, different persisted nonce: the nonce is read from the
	// payload, the domain from the signature. They are independent.
	t.Run("nonce comes from the payload", func(t *testing.T) {
		_, nonce, ok := RecoverOutboundSigner(outboundEvent(t, keyAHex, 99))
		require.True(t, ok)
		assert.Equal(t, uint64(99), nonce)
	})
}
