package types

import (
	"crypto/sha256"
	"encoding/hex"
	fmt "fmt"

	"cosmossdk.io/collections"
)

var (
	// ParamsKey saves the current module params.
	ParamsKey = collections.NewPrefix(0)

	// ParamsName is the name of the params collection.
	ParamsName = "params"

	// Old storage in V1
	ChainConfigsKey  = collections.NewPrefix(1) // ChainConfigsKey saves the current module chainConfigs collection prefix
	ChainConfigsName = "chain_configs"          // ChainConfigsName is the name of the chainConfigs collection.

	InboundsKey  = collections.NewPrefix(2)
	InboundsName = "inbound_synthetics"

	UniversalTxKey  = collections.NewPrefix(3)
	UniversalTxName = "universal_tx"

	ModuleAccountNonceKey  = collections.NewPrefix(4)
	ModuleAccountNonceName = "module_account_nonce"

	GasPricesKey  = collections.NewPrefix(5)
	GasPricesName = "gas_prices"
)

const (
	ModuleName = "uexecutor"

	StoreKey = ModuleName

	QuerierRoute = ModuleName
)

func GetInboundUniversalTxKey(inbound Inbound) string {
	data := fmt.Sprintf("%s:%s:%s", inbound.SourceChain, inbound.TxHash, inbound.LogIndex)
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:]) // hash[:] converts [32]byte → []byte
}

func GetInboundBallotKey(inbound Inbound) (string, error) {
	bz, err := inbound.Marshal()
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(bz), nil
}

func GetPcUniversalTxKey(pcCaip string, pc PCTx) string {
	data := fmt.Sprintf("%s:%s", pcCaip, pc.TxHash)
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}
