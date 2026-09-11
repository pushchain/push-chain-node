package types_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// F-2026-18146: an outbound payload comes from an attacker-controlled gateway
// event and lands in state, so it is capped at admission.
func TestValidateOutboundPayloadBlobSize(t *testing.T) {
	max := types.MaxOutboundPayloadBytes
	require.Equal(t, 128*1024, max)

	for _, tc := range []struct {
		name   string
		blob   string
		wantOK bool
	}{
		{"empty", "", true},
		{"small", "0xdeadbeef", true},
		{"at the cap", strings.Repeat("a", max), true},
		{"one over", strings.Repeat("a", max+1), false},
		{"the published ~2 MiB payload", strings.Repeat("a", 2*1024*1024), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := types.ValidateOutboundPayloadBlobSize("payload", tc.blob)
			if tc.wantOK {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.Contains(t, err.Error(), "payload too large")
			require.Contains(t, err.Error(), "131072")
		})
	}
}

func TestOutboundTx_ValidateSize(t *testing.T) {
	require.NoError(t, (*types.OutboundTx)(nil).ValidateSize())
	require.NoError(t, (&types.OutboundTx{Payload: "0xdeadbeef"}).ValidateSize())

	err := (&types.OutboundTx{Payload: strings.Repeat("a", types.MaxOutboundPayloadBytes+1)}).ValidateSize()
	require.Error(t, err)
	require.Contains(t, err.Error(), "payload too large")
}

// The outbound cap is derived from geth's txpool limit, not from the inbound
// cap. Equal today, but they must stay independently changeable.
func TestOutboundCapIsIndependentOfInboundCap(t *testing.T) {
	require.Equal(t, 128*1024, types.MaxOutboundPayloadBytes)
	require.Equal(t, 128*1024, types.MaxUniversalPayloadBytes)
}
