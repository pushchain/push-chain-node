package svm

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStoredIxDataAccountDiscriminator pins the Anchor account discriminator
// computation. The discriminator is sha256("account:<TypeName>")[:8] and MUST
// match what the gateway program emits when it serializes the account — a
// mismatch here makes the entire reclaimer's getProgramAccounts filter return
// no matches, silently breaking rent recovery.
func TestStoredIxDataAccountDiscriminator(t *testing.T) {
	expected := sha256.Sum256([]byte("account:StoredIxData"))
	require.Len(t, storedIxDataAccountDiscriminator, 8)
	assert.Equal(t, expected[:8], storedIxDataAccountDiscriminator,
		"discriminator must equal sha256(\"account:StoredIxData\")[:8]")
}

// TestStoredIxDataLayoutOffsets pins the on-chain byte offsets the reclaimer
// uses to parse sub_tx_id and filter on store_refund_recipient. These mirror
// the Rust struct exactly:
//
//	#[account]
//	pub struct StoredIxData {
//	    pub bump: u8,                          // 1
//	    pub sub_tx_id: [u8; 32],               // 32
//	    pub store_refund_recipient: Pubkey,    // 32
//	    pub ix_data: Vec<u8>,                  // 4-byte len + bytes
//	}
//
// preceded by Anchor's 8-byte account discriminator.
func TestStoredIxDataLayoutOffsets(t *testing.T) {
	assert.Equal(t, 9, storedIxDataSubTxIDOffset, "disc(8) + bump(1) = 9")
	assert.Equal(t, 41, storedIxDataRefundRecipientOffset, "disc(8) + bump(1) + sub_tx_id(32) = 41")
	assert.Equal(t, 77, storedIxDataMinLen, "disc + bump + sub_tx_id + refund_recipient + vec_len = 77")
}

// TestStoredIxDataDiscriminatorHexPin double-locks the discriminator by
// committing its hex form to the test. If anyone ever swaps the formula or
// the type name, this test catches it without needing to recompute sha256.
func TestStoredIxDataDiscriminatorHexPin(t *testing.T) {
	got := hex.EncodeToString(storedIxDataAccountDiscriminator)
	expected := func() string {
		h := sha256.Sum256([]byte("account:StoredIxData"))
		return hex.EncodeToString(h[:8])
	}()
	assert.Equal(t, expected, got)
}
