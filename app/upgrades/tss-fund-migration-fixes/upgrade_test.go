package tssfundmigrationfixes_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	tssfundmigrationfixes "github.com/pushchain/push-chain-node/app/upgrades/tss-fund-migration-fixes"
)

// TestNewUpgrade_Identity verifies the upgrade descriptor carries the expected
// name, wires up a non-nil handler factory, and declares no store additions or
// deletions (the migration is in-place on existing kv keys).
func TestNewUpgrade_Identity(t *testing.T) {
	u := tssfundmigrationfixes.NewUpgrade()

	require.Equal(t, "tss-fund-migration-fixes", u.UpgradeName)
	require.NotNil(t, u.CreateUpgradeHandler, "upgrade must expose a handler factory")
	require.Empty(t, u.StoreUpgrades.Added, "no new KV stores expected")
	require.Empty(t, u.StoreUpgrades.Deleted, "no KV stores deleted")
	require.Empty(t, u.StoreUpgrades.Renamed, "no KV stores renamed")
}
