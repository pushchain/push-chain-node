package svm

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"testing"

	solana "github.com/gagliardetto/solana-go"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestEventDecoder_HardcodedTxWithFunds(t *testing.T) {
	logger := zerolog.Nop()
	decoder := NewEventDecoder(logger)

	hexData := "2b1f1f0204ec6bff" +
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" +
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" +
		"15cd5b0700000000" +
		"31d4000000000000" +
		"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc" +
		"03000000" +
		"010203" +
		"01" +
		"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd" +
		"04000000" +
		"aabbccdd" +
		"01" +
		"05000000" +
		"1020304050"

	data, err := hex.DecodeString(hexData)
	require.NoError(t, err)

	event, err := decoder.DecodeEventData(data)
	require.NoError(t, err)
	fmt.Printf("Decoded synthetic event: %+v", event)

	var expectedSender, expectedRecipient, expectedBridgeToken, expectedRevertRecipient solana.PublicKey
	copy(expectedSender[:], mustDecodeHex("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"))
	copy(expectedRecipient[:], mustDecodeHex("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"))
	copy(expectedBridgeToken[:], mustDecodeHex("cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"))
	copy(expectedRevertRecipient[:], mustDecodeHex("dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"))

	require.Equal(t, "TxWithFunds", event.EventType)
	require.Equal(t, expectedSender.String(), event.Sender)
	require.Equal(t, expectedRecipient.String(), event.Recipient)
	require.Equal(t, uint64(123456789), event.BridgeAmount)
	require.Equal(t, uint64(54321), event.GasAmount)
	require.Equal(t, expectedBridgeToken.String(), event.BridgeToken)
	require.Equal(t, "0x010203", event.Data)
	require.Equal(t, expectedRevertRecipient.String(), event.RevertRecipient)
	require.Equal(t, "0xaabbccdd", event.RevertMessage)
	require.Equal(t, uint8(1), event.TxType)
	require.Equal(t, "0x1020304050", event.VerificationData)
}

func mustDecodeHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

func TestEventDecoder_RealGatewayLog(t *testing.T) {
	logger := zerolog.Nop()
	decoder := NewEventDecoder(logger)

	base64Data := "Kx8fAgTsa/8SP4vdKFC3bNfWErqfW0odBaZuOYBQSMzRK3++8/abvAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAGXNHQAAAAC1n2gAAAAAAMvK6eZkUv6Yq88PpR5Vf8jWccf8bOg8BfkpkqPSvxkybwAAAMKQjodgYssvejIXNY/wjfMcHjugWIE3rCpPxBwmqzX+AAAAAAAAAAAaAAAAdGVzdCBwYXlsb2FkIGZvciBmdW5kcytnYXMAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAABAAAAAAAAAAbTVXaZAQAAABI/i90oULds19YSup9bSh0Fpm45gFBIzNErf77z9pu8DgAAAHJldmVydCBtZXNzYWdlAyMAAAB0ZXN0X3NpZ25hdHVyZV9kYXRhX2Zvcl9zcGxfcGF5bG9hZA=="

	raw, err := base64.StdEncoding.DecodeString(base64Data)
	require.NoError(t, err)

	event, err := decoder.DecodeEventData(raw)
	require.NoError(t, err)
	fmt.Printf("Decoded real log event: %+v", event)

	require.Equal(t, "TxWithFunds", event.EventType)
	require.Equal(t, "2EEYH6e1PtCdWzZaag9buJmDDS79gvrm1aQm9yEcgWdR", event.Sender)
	require.Equal(t, "11111111111111111111111111111111", event.Recipient)
	require.Equal(t, uint64(500000000), event.BridgeAmount)
	require.Equal(t, uint64(6856629), event.GasAmount)
	require.Equal(t, "EiXDnrAg9ea2Q6vEPV7E5TpTU1vh41jcuZqKjU5Dc4ZF", event.BridgeToken)
	require.Equal(t, "0xc2908e876062cb2f7a3217358ff08df31c1e3ba0588137ac2a4fc41c26ab35fe00000000000000001a00000074657374207061796c6f616420666f722066756e64732b676173000000000000000000000000000000000000000000000000010000000000000006d355769901000000", event.Data)
	require.Empty(t, event.RevertRecipient)
	require.Empty(t, event.RevertMessage)
	require.Equal(t, uint8(0), event.TxType)
	require.Empty(t, event.VerificationData)
}
