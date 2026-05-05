package types_test

import (
	"testing"

	"github.com/pushchain/push-chain-node/x/uvalidator/types"

	"github.com/stretchr/testify/require"
)

func TestGenesisState_Validate(t *testing.T) {
	const testAdmin = "push1negskcfqu09j5zvpk7nhvacnwyy2mafffy7r6a"

	tests := []struct {
		desc     string
		genState *types.GenesisState
		valid    bool
	}{
		{
			// DefaultParams now returns an empty Admin so the operator MUST
			// explicitly set one in production genesis. The default genesis is
			// therefore intentionally invalid.
			desc:     "default genesis is invalid (admin must be explicitly set)",
			genState: types.DefaultGenesis(),
			valid:    false,
		},
		{
			desc:     "valid genesis state with explicit admin",
			genState: &types.GenesisState{Params: types.Params{Admin: testAdmin}},
			valid:    true,
		},
		{
			desc:     "invalid - empty admin",
			genState: &types.GenesisState{},
			valid:    false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			err := tc.genState.Validate()
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
