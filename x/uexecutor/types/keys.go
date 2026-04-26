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

	ChainMetaKey   = collections.NewPrefix(6)
	ChainMetasName = "chain_metas"

	PendingOutboundsKey  = collections.NewPrefix(7)
	PendingOutboundsName = "pending_outbounds"
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

func GetOutboundBallotKey(
	utxId string,
	outboundIndex string,
	observedTx OutboundObservation,
) (string, error) {

	bz, err := observedTx.Marshal()
	if err != nil {
		return "", err
	}

	data := append([]byte(utxId+":"+outboundIndex+":"), bz...)
	hash := sha256.Sum256(data)

	return hex.EncodeToString(hash[:]), nil
}

// GetOutboundRevertId generates a deterministic outbound ID for an inbound-revert
// outbound. sourceChain is the CAIP-2 identifier of the chain the inbound came from
// (e.g. "eip155:1"), mirroring the UTX key convention.
func GetOutboundRevertId(sourceChain string, inboundTxHash string) string {
	data := fmt.Sprintf("%s:%s:REVERT", sourceChain, inboundTxHash)
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

// GetRescueFundsOutboundId generates a deterministic outbound ID for a rescue-funds
// outbound. pushChainCaip is the CAIP-2 identifier of Push Chain (e.g. "eip155:2240"),
// mirroring the convention used by GetPcUniversalTxKey. This ID is also used as the
// subTxId on the source-chain gateway call, providing replay protection.
func GetRescueFundsOutboundId(pushChainCaip string, pcTxHash string, logIndex string) string {
	data := fmt.Sprintf("%s:%s:%s:RESCUE", pushChainCaip, pcTxHash, logIndex)
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}
