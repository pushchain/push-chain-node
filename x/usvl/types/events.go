package types

// Event types for the usvl module
const (
	EventTypeAddChainConfig    = "add_chain_config"
	EventTypeUpdateChainConfig = "update_chain_config"
	EventTypeDeleteChainConfig = "delete_chain_config"
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

// EventDeleteChainConfig is emitted when a chain configuration is deleted
type EventDeleteChainConfig struct {
	ChainId string `yaml:"chain_id"`
}
