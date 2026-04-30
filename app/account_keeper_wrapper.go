package app

import (
	"time"

	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// accountKeeperWrapper wraps authkeeper.AccountKeeper and adds stub implementations
// for unordered transaction methods not available in cosmos-sdk v0.50.x.
type accountKeeperWrapper struct {
	authkeeper.AccountKeeper
}

func (w accountKeeperWrapper) UnorderedTransactionsEnabled() bool {
	return false
}

func (w accountKeeperWrapper) RemoveExpiredUnorderedNonces(_ sdk.Context) error {
	return nil
}

func (w accountKeeperWrapper) TryAddUnorderedNonce(_ sdk.Context, _ []byte, _ time.Time) error {
	return nil
}
