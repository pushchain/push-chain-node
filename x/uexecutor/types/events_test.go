package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// A PC20 export's outbound_created event must carry is_pc20 + the source token,
// so a relayer can settle it (build the wrapper mint) straight from the event —
// asset_addr is empty for PC20, so those two fields are how the source is
// conveyed.
func TestNewOutboundCreatedEvent_CarriesPC20Fields(t *testing.T) {
	evt, err := types.NewOutboundCreatedEvent(types.OutboundCreatedEvent{
		TxID:                "0xabc",
		DestinationChain:    "eip155:11155111",
		IsPc20:              true,
		Pc20ContractAddress: "0xdAC17F958D2ee523a2206206994597C13D831ec7",
	})
	require.NoError(t, err)

	attrs := map[string]string{}
	for _, a := range evt.Attributes {
		attrs[a.Key] = a.Value
	}
	require.Equal(t, "true", attrs["is_pc20"])
	require.Equal(t, "0xdAC17F958D2ee523a2206206994597C13D831ec7", attrs["pc20_contract_address"])
}

func TestNewOutboundCreatedEvent_PRC20HasNoPC20Fields(t *testing.T) {
	evt, err := types.NewOutboundCreatedEvent(types.OutboundCreatedEvent{
		TxID:      "0xabc",
		AssetAddr: "0x9999999999999999999999999999999999999999",
	})
	require.NoError(t, err)

	attrs := map[string]string{}
	for _, a := range evt.Attributes {
		attrs[a.Key] = a.Value
	}
	require.Equal(t, "false", attrs["is_pc20"])
	require.Empty(t, attrs["pc20_contract_address"])
}
