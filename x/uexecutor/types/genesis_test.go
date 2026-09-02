package types_test

import (
	"testing"

	"github.com/pushchain/push-chain-node/x/uexecutor/types"

	"github.com/stretchr/testify/require"
)

func TestGenesisState_Validate(t *testing.T) {
	tests := []struct {
		desc     string
		genState *types.GenesisState
		valid    bool
	}{
		{
			desc:     "default is valid",
			genState: types.DefaultGenesis(),
			valid:    true,
		},
		{
			// An empty Params leaves max_gasless_tx_gas at 0, which would
			// reject every gasless tx and stop the universal validators from
			// voting. Fail at genesis rather than silently.
			desc:     "empty params are rejected",
			genState: &types.GenesisState{},
			valid:    false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			err := tc.genState.ValidateBasic()
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
