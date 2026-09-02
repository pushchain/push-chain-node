package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// TestDefaultParams_MaxGaslessTxGas pins the shipped default. Gasless txs pay
// no fee, so this cap is the only bound on the gas one of them can declare.
func TestDefaultParams_MaxGaslessTxGas(t *testing.T) {
	params := types.DefaultParams()

	require.Equal(t, uint64(100_000_000), params.MaxGaslessTxGas)
	require.Equal(t, types.DefaultMaxGaslessTxGas, params.MaxGaslessTxGas)
	require.NoError(t, params.ValidateBasic())
}

// TestParams_ValidateBasic_RejectsZeroCap: a zero cap would reject every
// gasless tx, which would stop the universal validators from voting.
func TestParams_ValidateBasic_RejectsZeroCap(t *testing.T) {
	params := types.DefaultParams()
	params.MaxGaslessTxGas = 0

	err := params.ValidateBasic()

	require.Error(t, err)
	require.Contains(t, err.Error(), "max_gasless_tx_gas")
}

// TestParams_ValidateBasic_AcceptsGovernanceChosenCaps: the cap has to be
// movable in both directions by proposal.
func TestParams_ValidateBasic_AcceptsGovernanceChosenCaps(t *testing.T) {
	for _, cap := range []uint64{1, 21_000, 30_000_000, 100_000_000, 500_000_000} {
		params := types.DefaultParams()
		params.MaxGaslessTxGas = cap

		require.NoError(t, params.ValidateBasic(), "cap %d must be settable", cap)
	}
}
