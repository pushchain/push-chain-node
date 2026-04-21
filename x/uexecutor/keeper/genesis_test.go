package keeper_test

import (
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/pushchain/push-chain-node/x/uexecutor/types"
	"github.com/stretchr/testify/require"
)

func TestGenesis(t *testing.T) {
	f := SetupTest(t)

	genesisState := &types.GenesisState{
		Params: types.DefaultParams(),
	}
	f.mockEVMKeeper.EXPECT().SetAccount(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	f.mockEVMKeeper.EXPECT().SetCode(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	f.mockEVMKeeper.EXPECT().SetState(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	f.k.InitGenesis(f.ctx, genesisState)

	got := f.k.ExportGenesis(f.ctx)
	require.NotNil(t, got)
}

func TestGenesisExportImportRoundTrip(t *testing.T) {
	f := SetupTest(t)

	// Setup EVM mock for InitGenesis (fresh genesis deploys contracts)
	f.mockEVMKeeper.EXPECT().SetAccount(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	f.mockEVMKeeper.EXPECT().SetCode(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	f.mockEVMKeeper.EXPECT().SetState(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	// Init with default state
	f.k.InitGenesis(f.ctx, &types.GenesisState{Params: types.DefaultParams()})

	// Populate state: PendingInbounds
	require.NoError(t, f.k.PendingInbounds.Set(f.ctx, "inbound-key-1"))
	require.NoError(t, f.k.PendingInbounds.Set(f.ctx, "inbound-key-2"))

	// Populate state: UniversalTx
	utx1 := types.UniversalTx{
		Id: "utx-1",
		InboundTx: &types.Inbound{
			SourceChain: "eip155:1",
			TxHash:      "0xabc",
			Sender:      "0x1234",
			Amount:      "1000",
		},
	}
	require.NoError(t, f.k.UniversalTx.Set(f.ctx, "utx-key-1", utx1))

	// Populate state: ModuleAccountNonce
	require.NoError(t, f.k.ModuleAccountNonce.Set(f.ctx, 42))

	// Export
	exported := f.k.ExportGenesis(f.ctx)
	require.NotNil(t, exported)
	require.True(t, exported.Exported)
	require.Len(t, exported.PendingInbounds, 2)
	require.Len(t, exported.UniversalTxs, 1)
	require.Equal(t, uint64(42), exported.ModuleAccountNonce)

	// Re-init on fresh fixture with exported state
	// Exported=true means no contract deployment (no EVM mock needed for SetAccount/SetCode)
	f2 := SetupTest(t)
	require.NoError(t, f2.k.InitGenesis(f2.ctx, exported))

	// Export again and compare
	reExported := f2.k.ExportGenesis(f2.ctx)
	require.Equal(t, len(exported.PendingInbounds), len(reExported.PendingInbounds))
	require.Equal(t, len(exported.UniversalTxs), len(reExported.UniversalTxs))
	require.Equal(t, exported.ModuleAccountNonce, reExported.ModuleAccountNonce)
	require.Equal(t, exported.UniversalTxs[0].Key, reExported.UniversalTxs[0].Key)
	require.Equal(t, exported.UniversalTxs[0].Value.Id, reExported.UniversalTxs[0].Value.Id)
}

func TestGenesisExportedSkipsDeployment(t *testing.T) {
	f := SetupTest(t)

	// No EVM mocks set — if InitGenesis tries to deploy, it will panic/fail

	exported := &types.GenesisState{
		Params:   types.DefaultParams(),
		Exported: true, // should skip deployment
	}
	// This should NOT panic even without EVM mocks
	require.NoError(t, f.k.InitGenesis(f.ctx, exported))
}
