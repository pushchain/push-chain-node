package types

import (
	"fmt"
	"strings"

	"cosmossdk.io/collections"
)

var (
	// ParamsKey saves the current module params.
	ParamsKey = collections.NewPrefix(0)

	// ParamsName is the name of the params collection.
	ParamsName = "params"

	// VerifiedInboundTxsKey saves the verified transactions collection prefix
	VerifiedInboundTxsKeyPrefix = collections.NewPrefix(1)

	// VerifiedInboundTxsName is the name of the verified transactions collection.
	VerifiedInboundTxsName = "verified_inbound_txs"
)

const (
	ModuleName = "utv"

	StoreKey = ModuleName

	QuerierRoute = ModuleName
)

// GetVerifiedTxStorageKey returns the storage key for a verified transaction hash using the format "chain:txHash".
func GetVerifiedInboundTxStorageKey(chain, txHash string) string {
	// Normalize to lowercase and strip whitespace
	return fmt.Sprintf("%s:%s", strings.ToLower(chain), strings.ToLower(strings.TrimSpace(txHash)))
}
