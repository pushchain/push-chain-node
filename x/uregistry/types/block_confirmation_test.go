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
			name: "valid - fast < slow",
			config: types.BlockConfirmation{
				FastInbound: 3,
				SlowInbound: 10,
			},
			expectErr: false,
		},
		{
			name: "valid - fast == slow",
			config: types.BlockConfirmation{
				FastInbound: 5,
				SlowInbound: 5,
			},
			expectErr: false,
		},
		{
			name: "valid - fast = 0, slow > 0",
			config: types.BlockConfirmation{
				FastInbound: 0,
				SlowInbound: 5,
			},
			expectErr: false,
		},
		{
			name: "invalid - fast > slow (slow = 0)",
			config: types.BlockConfirmation{
				FastInbound: 2,
				SlowInbound: 0,
			},
			expectErr: true,
			errMsg:    "fast_inbound cannot be greater than slow_inbound confirmations",
		},
		{
			name: "invalid - fast > slow",
			config: types.BlockConfirmation{
				FastInbound: 10,
				SlowInbound: 5,
			},
			expectErr: true,
			errMsg:    "fast_inbound cannot be greater than slow_inbound confirmations",
		},
		{
			name: "valid - both zero",
			config: types.BlockConfirmation{
				FastInbound: 0,
				SlowInbound: 0,
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
