package types_test

import (
	"strings"
	"testing"

	"github.com/pushchain/push-chain-node/x/uexecutor/types"
	"github.com/stretchr/testify/require"
)

// payloadWithSize builds a valid UniversalPayload whose serialized size is
// exactly want bytes, by sizing the (hex) data field to fill the remainder.
func payloadWithSize(t *testing.T, want int) types.UniversalPayload {
	t.Helper()

	// The data field costs one tag byte plus a varint length plus the bytes
	// themselves; every size this test uses falls in the 3-byte varint band.
	const dataOverhead = 1 + 3

	// "0x" plus an even number of hex characters, so the length of the data
	// field is always even — the nonce absorbs the odd byte when needed.
	for _, nonce := range []string{"1", "11"} {
		p := types.UniversalPayload{To: mockHexAddress(), Nonce: nonce}
		dataLen := want - p.Size() - dataOverhead
		if dataLen < 2 || dataLen%2 != 0 {
			continue
		}
		p.Data = "0x" + strings.Repeat("ab", (dataLen-2)/2)
		if p.Size() == want {
			return p
		}
	}

	t.Fatalf("could not build a universal payload of exactly %d bytes", want)
	return types.UniversalPayload{}
}

func TestUniversalPayload_SizeCap(t *testing.T) {
	t.Run("at the cap is accepted", func(t *testing.T) {
		p := payloadWithSize(t, types.MaxUniversalPayloadBytes)
		require.Equal(t, types.MaxUniversalPayloadBytes, p.Size())
		require.NoError(t, p.ValidateSize())
		require.NoError(t, p.ValidateBasic())
	})

	t.Run("one byte over the cap is rejected", func(t *testing.T) {
		p := payloadWithSize(t, types.MaxUniversalPayloadBytes+1)
		require.Equal(t, types.MaxUniversalPayloadBytes+1, p.Size())

		sizeErr := p.ValidateSize()
		basicErr := p.ValidateBasic()

		require.Error(t, sizeErr)
		require.Contains(t, sizeErr.Error(), "universal payload too large")
		require.Contains(t, sizeErr.Error(), "131073 bytes exceeds the 131072 byte limit")

		// ValidateBasic must reject it too — the payload is otherwise valid, so
		// the size check is the only thing that can fail it.
		require.Error(t, basicErr)
		require.Contains(t, basicErr.Error(), "universal payload too large")
	})

	t.Run("nil payload has no size", func(t *testing.T) {
		var p *types.UniversalPayload
		require.NoError(t, p.ValidateSize())
	})
}

func TestValidatePayloadBlobSize(t *testing.T) {
	atCap := strings.Repeat("a", types.MaxUniversalPayloadBytes)
	overCap := atCap + "a"

	require.NoError(t, types.ValidatePayloadBlobSize("raw_payload", atCap))

	err := types.ValidatePayloadBlobSize("raw_payload", overCap)
	require.Error(t, err)
	require.Contains(t, err.Error(), "raw_payload too large")
	require.Contains(t, err.Error(), "131073 bytes exceeds the 131072 byte limit")
}

func TestInbound_SizeCap(t *testing.T) {
	base := func() types.Inbound {
		return types.Inbound{
			SourceChain: "eip155:11155111",
			TxHash:      "0x" + strings.Repeat("11", 32),
			Sender:      mockHexAddress(),
			LogIndex:    "1",
			TxType:      types.TxType_FUNDS_AND_PAYLOAD,
		}
	}

	atCap := strings.Repeat("a", types.MaxUniversalPayloadBytes)
	overCap := atCap + "a"

	t.Run("raw_payload at the cap is accepted", func(t *testing.T) {
		in := base()
		in.RawPayload = atCap
		require.Len(t, in.RawPayload, types.MaxUniversalPayloadBytes)
		require.NoError(t, in.ValidateSize())
		require.NoError(t, in.ValidateBasic())
	})

	t.Run("raw_payload one byte over the cap is rejected", func(t *testing.T) {
		in := base()
		in.RawPayload = overCap
		require.Len(t, in.RawPayload, types.MaxUniversalPayloadBytes+1)

		sizeErr := in.ValidateSize()
		basicErr := in.ValidateBasic()

		require.Error(t, sizeErr)
		require.Contains(t, sizeErr.Error(), "raw_payload too large")
		require.Error(t, basicErr)
		require.Contains(t, basicErr.Error(), "raw_payload too large")
	})

	t.Run("verification_data over the cap is rejected", func(t *testing.T) {
		in := base()
		in.VerificationData = overCap

		err := in.ValidateBasic()
		require.Error(t, err)
		require.Contains(t, err.Error(), "verification_data too large")
	})

	t.Run("embedded universal_payload over the cap is rejected", func(t *testing.T) {
		in := base()
		p := payloadWithSize(t, types.MaxUniversalPayloadBytes+1)
		in.UniversalPayload = &p

		err := in.ValidateBasic()
		require.Error(t, err)
		require.Contains(t, err.Error(), "universal payload too large")
	})

	t.Run("nil inbound has no size", func(t *testing.T) {
		var in *types.Inbound
		require.NoError(t, in.ValidateSize())
	})
}

func TestMsgExecutePayload_SizeCap(t *testing.T) {
	const signer = "push1fgaewhyd9fkwtqaj9c233letwcuey6dgly9gv9"
	ua := &types.UniversalAccountId{
		ChainNamespace: "eip155",
		ChainId:        "11155111",
		Owner:          "0x000000000000000000000000000000000000dead",
	}

	t.Run("payload at the cap is accepted", func(t *testing.T) {
		p := payloadWithSize(t, types.MaxUniversalPayloadBytes)
		msg := &types.MsgExecutePayload{
			Signer:             signer,
			UniversalAccountId: ua,
			UniversalPayload:   &p,
			VerificationData:   "abcdef0123456789",
		}
		require.NoError(t, msg.ValidateBasic())
	})

	t.Run("payload over the cap is rejected", func(t *testing.T) {
		p := payloadWithSize(t, types.MaxUniversalPayloadBytes+1)
		msg := &types.MsgExecutePayload{
			Signer:             signer,
			UniversalAccountId: ua,
			UniversalPayload:   &p,
			VerificationData:   "abcdef0123456789",
		}

		err := msg.ValidateBasic()
		require.Error(t, err)
		require.Contains(t, err.Error(), "universal payload too large")
	})

	t.Run("verificationData over the cap is rejected", func(t *testing.T) {
		p := types.UniversalPayload{To: mockHexAddress(), Data: "0xabcdef"}
		msg := &types.MsgExecutePayload{
			Signer:             signer,
			UniversalAccountId: ua,
			UniversalPayload:   &p,
			VerificationData:   strings.Repeat("ab", types.MaxUniversalPayloadBytes),
		}

		err := msg.ValidateBasic()
		require.Error(t, err)
		require.Contains(t, err.Error(), "verificationData too large")
	})
}
