package types

// Event types for the utv module
const (
	EventTypeAddChainConfig              = "add_chain_config"
	EventTypeUpdateChainConfig           = "update_chain_config"
	EventTypeExternalTransactionVerified = "external_transaction_verified"
)

// EventAddChainConfig is emitted when a chain configuration is added
type EventAddChainConfig struct {
	ChainId    string `yaml:"chain_id"`
	CaipPrefix string `yaml:"caip_prefix"`
}

// EventUpdateChainConfig is emitted when a chain configuration is updated
type EventUpdateChainConfig struct {
	ChainId    string `yaml:"chain_id"`
	CaipPrefix string `yaml:"caip_prefix"`
}

// EventExternalTransactionVerified is emitted when an external transaction is verified
type EventExternalTransactionVerified struct {
	TxHash      string `yaml:"tx_hash"`
	CaipAddress string `yaml:"caip_address"`
	Verified    bool   `yaml:"verified"`
}
