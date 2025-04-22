package types

import (
	"testing"
)

func TestConfig(t *testing.T) {
	originalConfig := ChainConfig{
		ChainID:                 "1",
		ChainName:               "ethereum-mainnet",
		ChainRpcURL:             "https://mainnet.infura.io/v3/XXXXXXXXXX",
		ChainWssSocket:          "wss://mainnet.infura.io/ws/v3/XXXXXXXXXX",
		LockerContractAddress:   "0x1234567890abcdef1234567890abcdef12345678",
		BlockConfirmationNumber: 12,
		CAIP10Prefix:            "eip155:1",
	}

	err := originalConfig.Validate()
	if err != nil {
		t.Errorf("Validation error: %v", err)
		return
	}

}

func TestEmptyConfig(t *testing.T) {
	// Arrange
	partialConfig := ChainConfig{
		ChainName:               "ethereum-mainnet",
		ChainRpcURL:             "https://mainnet.infura.io/v3/XXXXXXXXXX",
		ChainWssSocket:          "wss://mainnet.infura.io/ws/v3/XXXXXXXXXX",
		LockerContractAddress:   "0x1234567890abcdef1234567890abcdef12345678",
		BlockConfirmationNumber: 12,
	}

	err := partialConfig.Validate()
	if err == nil {
		t.Error("Expected validation to fail, but it succeeded")
		return
	}

}

func TestGenerateConfig(t *testing.T) {
	chainConfigs := GenerateConfig()
	if len(chainConfigs) == 0 {
		t.Error("Expected non-empty config, but got empty")
		return
	}
}
