package keeper_test

import (
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/rollchains/pchain/x/ue/types"
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
