package types

import (
	"fmt"
	"strings"

	"cosmossdk.io/collections"
)

var (
	// ChainConfigKey defines the key for storing chain configurations
	ChainConfigKey = collections.NewPrefix(1)
)

// String returns the string representation of NetworkType
// func (nt NetworkType) String() string {
// 	switch nt {
// 	case NetworkTypeMainnet:
// 		return "mainnet"
// 	case NetworkTypeTestnet:
// 		return "testnet"
// 	case NetworkTypeDevnet:
// 		return "devnet"
// 	case NetworkTypeLocalnet:
// 		return "localnet"
// 	default:
// 		return "unspecified"
// 	}
// }

// ParseNetworkType converts a string to NetworkType
func ParseNetworkType(s string) (NetworkType, error) {
	switch s := strings.ToLower(s); s {
	case "mainnet":
		return NetworkTypeMainnet, nil
	case "testnet":
		return NetworkTypeTestnet, nil
	case "devnet":
		return NetworkTypeDevnet, nil
	case "localnet":
		return NetworkTypeLocalnet, nil
	case "unspecified", "":
		return NetworkTypeUnspecified, nil
	default:
		return NetworkTypeUnspecified, fmt.Errorf("invalid network type: %s", s)
	}
}

// ParseVmType converts a string to VmType
func ParseVmType(s string) (VmType, error) {
	switch strings.ToUpper(s) {
	case "EVM":
		return VmTypeEvm, nil
	case "SVM":
		return VmTypeSvm, nil
	case "WASM":
		return VmTypeWasm, nil
	case "UNSPECIFIED", "":
		return VmTypeUnspecified, nil
	default:
		return VmTypeUnspecified, fmt.Errorf("invalid VM type: %s", s)
	}
}

// ChainConfigData represents the internal storage for external chain configuration
type ChainConfigData struct {
	// ChainId is the unique identifier for the chain
	ChainId string `json:"chain_id" yaml:"chain_id"`

	// ChainName is a human-readable name for the chain
	ChainName string `json:"chain_name" yaml:"chain_name"`

	// CaipPrefix is the CAIP-2 identifier for the chain, e.g., "eip155:11155111"
	CaipPrefix string `json:"caip_prefix" yaml:"caip_prefix"`

	// LockerContractAddress is the address of the fee locker contract on the external chain
	LockerContractAddress string `json:"locker_contract_address" yaml:"locker_contract_address"`

	// UsdcAddress is the address of the USDC token contract on the external chain
	UsdcAddress string `json:"usdc_address" yaml:"usdc_address"`

	// PublicRpcUrl is the default RPC URL for the chain (can be overridden by validator config)
	PublicRpcUrl string `json:"public_rpc_url" yaml:"public_rpc_url"`

	// NetworkType identifies the type of network (mainnet, testnet, localnet, devnet)
	NetworkType NetworkType `json:"network_type" yaml:"network_type"`

	// VmType identifies the virtual machine type (EVM, SVM, etc.)
	VmType VmType `json:"vm_type" yaml:"vm_type"`
}

// NewChainConfigData creates a new ChainConfigData instance
func NewChainConfigData(
	chainId string,
	chainName string,
	caipPrefix string,
	lockerContractAddress string,
	usdcAddress string,
	publicRpcUrl string,
	networkType NetworkType,
	vmType VmType,
) ChainConfigData {
	return ChainConfigData{
		ChainId:               chainId,
		ChainName:             chainName,
		CaipPrefix:            caipPrefix,
		LockerContractAddress: lockerContractAddress,
		UsdcAddress:           usdcAddress,
		PublicRpcUrl:          publicRpcUrl,
		NetworkType:           networkType,
		VmType:                vmType,
	}
}

// Validate performs basic validation of the chain configuration
func (cc ChainConfigData) Validate() error {
	if strings.TrimSpace(cc.ChainId) == "" {
		return fmt.Errorf("chain ID cannot be empty")
	}
	if strings.TrimSpace(cc.ChainName) == "" {
		return fmt.Errorf("chain name cannot be empty")
	}
	if strings.TrimSpace(cc.CaipPrefix) == "" {
		return fmt.Errorf("CAIP prefix cannot be empty")
	}
	// CAIP prefix should follow the CAIP-2 format: namespace:reference
	parts := strings.Split(cc.CaipPrefix, ":")
	if len(parts) < 2 {
		return fmt.Errorf("invalid CAIP prefix format, expected 'namespace:reference', got %s", cc.CaipPrefix)
	}
	if strings.TrimSpace(cc.LockerContractAddress) == "" {
		return fmt.Errorf("locker contract address cannot be empty")
	}
	if strings.TrimSpace(cc.UsdcAddress) == "" {
		return fmt.Errorf("USDC address cannot be empty")
	}
	if strings.TrimSpace(cc.PublicRpcUrl) == "" {
		return fmt.Errorf("public RPC URL cannot be empty")
	}
	if cc.NetworkType == NetworkTypeUnspecified {
		return fmt.Errorf("network type cannot be unspecified")
	}
	if cc.VmType == VmTypeUnspecified {
		return fmt.Errorf("VM type cannot be unspecified")
	}

	return nil
}

// ToProto converts ChainConfigData to protobuf ChainConfig
func (cc ChainConfigData) ToProto() ChainConfig {
	return ChainConfig{
		ChainId:               cc.ChainId,
		ChainName:             cc.ChainName,
		CaipPrefix:            cc.CaipPrefix,
		LockerContractAddress: cc.LockerContractAddress,
		UsdcAddress:           cc.UsdcAddress,
		PublicRpcUrl:          cc.PublicRpcUrl,
		NetworkType:           NetworkType(cc.NetworkType),
		VmType:                VmType(cc.VmType),
	}
}

// FromProto converts protobuf ChainConfig to ChainConfigData
func ChainConfigDataFromProto(config ChainConfig) ChainConfigData {
	return ChainConfigData{
		ChainId:               config.ChainId,
		ChainName:             config.ChainName,
		CaipPrefix:            config.CaipPrefix,
		LockerContractAddress: config.LockerContractAddress,
		UsdcAddress:           config.UsdcAddress,
		PublicRpcUrl:          config.PublicRpcUrl,
		NetworkType:           NetworkType(config.NetworkType),
		VmType:                VmType(config.VmType),
	}
}

// DefaultEthereumSepoliaConfig returns the default Ethereum Sepolia testnet configuration
func DefaultEthereumSepoliaConfig() ChainConfigData {
	return ChainConfigData{
		ChainId:               "11155111",
		ChainName:             "Ethereum Sepolia",
		CaipPrefix:            "eip155:11155111",
		LockerContractAddress: "0x57235d27c8247CFE0E39248c9c9F22BD6EB054e1", // Replace with actual locker contract address
		UsdcAddress:           "0x7169D38820dfd117C3FA1f22a697dBA58d90BA06", // Replace with actual USDC address on Sepolia
		PublicRpcUrl:          "https://rpc.sepolia.org",
		NetworkType:           NetworkTypeTestnet,
		VmType:                VmTypeEvm,
	}
}
