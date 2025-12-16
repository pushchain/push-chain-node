package utils

import (
	"github.com/ethereum/go-ethereum/common"
)

const MintModule string = "mint"

type Addresses struct {
	// Contract addresses
	FactoryAddr            common.Address
	UEProxyAddr            common.Address
	EVMImplAddr            common.Address
	HandlerAddr            common.Address
	PRC20USDCAddr          common.Address
	ExternalUSDCAddr       common.Address
	UniversalGatewayPCAddr common.Address

	// Account addresses (hex format)
	DefaultTestAddr string
	CosmosTestAddr  string
	TargetAddr      string
	TargetAddr2     string
}
type TestConfig struct {
	BaseCoinDenom  string
	DefaultCoinAmt int64
}

func GetDefaultAddresses() Addresses {
	return Addresses{
		FactoryAddr:            common.HexToAddress("0x00000000000000000000000000000000000000ea"),
		UEProxyAddr:            common.HexToAddress("0x0000000000000000000000000000000000000e09"),
		EVMImplAddr:            common.HexToAddress("0x0000000000000000000000000000000000000e01"),
		HandlerAddr:            common.HexToAddress("0x00000000000000000000000000000000000000C0"),
		PRC20USDCAddr:          common.HexToAddress("0x0000000000000000000000000000000000000e06"),
		ExternalUSDCAddr:       common.HexToAddress("0x0000000000000000000000000000000000000e07"),
		UniversalGatewayPCAddr: common.HexToAddress("0x00000000000000000000000000000000000000B0"),
		DefaultTestAddr:        "0x778d3206374f8ac265728e18e3fe2ae6b93e4ce4",
		CosmosTestAddr:         "cosmos18pjnzwr9xdnx2vnpv5mxywfnv56xxef5cludl5",
		TargetAddr:             "\x86i\xbe\xd1!\xfe\xfa=\x9c\xf2\x82\x12s\xf4\x89\xe7\x17̩]",
		TargetAddr2:            "0x527F3692F5C53CfA83F7689885995606F93b6164",
	}
}

func GetDefaultTestConfig() TestConfig {
	return TestConfig{
		BaseCoinDenom:  "upc",
		DefaultCoinAmt: 23748000000000,
	}
}
