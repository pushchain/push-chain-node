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

	// VerifiedTxsKey saves the verified transactions collection prefix
	VerifiedTxsKeyPrefix = collections.NewPrefix(1)

	// VerifiedTxsName is the name of the verified transactions collection.
	VerifiedTxsName = "verified_txs"
)

const (
	ModuleName = "utv"

	StoreKey = ModuleName

	QuerierRoute = ModuleName
)

// GetVerifiedTxStorageKey returns the storage key for a verified transaction hash using the format "chainId:txHash".
func GetVerifiedTxStorageKey(chainId, txHash string) string {
	// Normalize to lowercase and strip whitespace
	return fmt.Sprintf("%s:%s", strings.ToLower(chainId), strings.ToLower(strings.TrimSpace(txHash)))
}
