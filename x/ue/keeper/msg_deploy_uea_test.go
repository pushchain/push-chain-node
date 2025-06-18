package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rollchains/pchain/x/ue/types"
)

func TestKeeper_DeployUEA(t *testing.T) {
	f := SetupTest(t)
	require := require.New(t)

	evmFrom := f.evmAddrs[0] // Assuming SetupTest gives you EVM-compatible address
	validUA := &types.UniversalAccount{
		Owner: "0x1234567890abcdef1234567890abcdef12345678",
		Chain: "eip155:11155111",
	}
	validTxHash := "0xabc123"

	t.Run("fail; CallFactoryToDeployUEA fails", func(t *testing.T) {
		// This UA is assumed to make the CallFactoryToDeployUEA fail in current setup
		ua := &types.UniversalAccount{
			Owner: "0x0", // malformed or invalid
			Chain: "ethereum",
		}

		_, err := f.k.DeployUEA(f.ctx, evmFrom, ua, validTxHash)
		require.Error(err)
	})

	t.Run("success; returns UEA bytes", func(t *testing.T) {
		// Uses the valid mocked setup (MockUTVKeeper passes, EVM call returns dummy bytes)
		ret, err := f.k.DeployUEA(f.ctx, evmFrom, validUA, validTxHash)
		require.NoError(err)
		require.Equal([]byte{0x01, 0x02}, ret) // as per MockEVMKeeper
	})
}
