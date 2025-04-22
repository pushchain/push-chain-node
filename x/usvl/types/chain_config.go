package types

import "fmt"

type ChainConfig struct {
	// ChainId of the chain
	ChainID string `json:"chainId"`
	// Chain name, ethereum-mainnet, ethereum-sepolia..
	ChainName string `json:"chainName"`
	// Chain rpc url
	ChainRpcURL string `json:"chainRpcUrl"`
	//Chain wss socket
	ChainWssSocket string `json:"chainWssSocket"`
	//Locker Contract Address
	LockerContractAddress string `json:"lockerContractAddress"`
	// Block confirmation number
	BlockConfirmationNumber uint8 `json:"blockConfirmationNumber"`
	//Caip10 prefix
	CAIP10Prefix string `json:"caip10Prefix"`
	
}

func (c *ChainConfig) Validate() ( error) {
	if c.ChainID == "" {
		return  fmt.Errorf("ChainID cannot be empty")
	}
	if c.ChainName == "" {
		return  fmt.Errorf("ChainName cannot be empty")
	}
	if c.ChainRpcURL == "" {
		return  fmt.Errorf("ChainRpcURL cannot be empty")
	}
	if c.ChainWssSocket == "" {
		return  fmt.Errorf("ChainWssSocket cannot be empty")
	}
	if c.LockerContractAddress == "" {
		return  fmt.Errorf("LockerContractAddress cannot be empty")
	}
	if c.BlockConfirmationNumber == 0 {
		return  fmt.Errorf("BlockConfirmationNumber cannot be 0")
	}
	if c.CAIP10Prefix == "" {
		return  fmt.Errorf("CAIP10Prefix cannot be empty")
	}
	return  nil
}

func generateConfigForEthereumSepolia() ChainConfig {
	return ChainConfig{
		ChainID:                 "11155111",
		ChainName:               "ethereum-sepolia",
		ChainRpcURL:             "https://sepolia.infura.io/v3/XXXXXXXXXX",
		ChainWssSocket:          "wss://sepolia.infura.io/ws/v3/XXXXXXXXXX",
		LockerContractAddress:   "0xabcdefabcdefabcdefabcdefabcdefabcdefabcd",
		BlockConfirmationNumber: 12,
		CAIP10Prefix:            "eip155:11155111",
	}
}

func GenerateConfig() []ChainConfig {
	configs := []ChainConfig{generateConfigForEthereumSepolia()}
	return configs
}
