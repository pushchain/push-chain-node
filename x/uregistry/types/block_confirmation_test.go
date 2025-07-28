package types_test

import (
	"testing"

	"github.com/rollchains/pchain/x/uregistry/types"
	"github.com/stretchr/testify/require"
)

func TestBlockConfirmation_ValidateBasic(t *testing.T) {
	tests := []struct {
		name      string
		config    types.BlockConfirmation
		expectErr bool
		errMsg    string
	}{
		{
			name: "valid - fast < standard",
			config: types.BlockConfirmation{
				FastInbound:     3,
				StandardInbound: 10,
			},
			expectErr: false,
		},
		{
			name: "valid - fast == standard",
			config: types.BlockConfirmation{
				FastInbound:     5,
				StandardInbound: 5,
			},
			expectErr: false,
		},
		{
			name: "valid - fast = 0, standard > 0",
			config: types.BlockConfirmation{
				FastInbound:     0,
				StandardInbound: 5,
			},
			expectErr: false,
		},
		{
			name: "invalid - fast > standard (standard = 0)",
			config: types.BlockConfirmation{
				FastInbound:     2,
				StandardInbound: 0,
			},
			expectErr: true,
			errMsg:    "fast_inbound cannot be greater than standard_inbound confirmations",
		},
		{
			name: "invalid - fast > standard",
			config: types.BlockConfirmation{
				FastInbound:     10,
				StandardInbound: 5,
			},
			expectErr: true,
			errMsg:    "fast_inbound cannot be greater than standard_inbound confirmations",
		},
		{
			name: "valid - both zero",
			config: types.BlockConfirmation{
				FastInbound:     0,
				StandardInbound: 0,
			},
			expectErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.config.ValidateBasic()
			if tc.expectErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
