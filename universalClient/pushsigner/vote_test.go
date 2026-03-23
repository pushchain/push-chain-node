package pushsigner

import (
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVoteConstants(t *testing.T) {
	t.Run("default gas limit", func(t *testing.T) {
		assert.Equal(t, uint64(500000000), defaultGasLimit)
	})

	t.Run("default fee amount is valid", func(t *testing.T) {
		coins, err := sdk.ParseCoinsNormalized(defaultFeeAmount)
		require.NoError(t, err)
		assert.False(t, coins.IsZero())
	})

	t.Run("default vote timeout", func(t *testing.T) {
		assert.Equal(t, 30*time.Second, defaultVoteTimeout)
	})
}
