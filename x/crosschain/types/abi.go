package types

import (
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
)

// FactoryV1ABI contains the ABI for the factory contract
const FactoryV1ABI = `[
	{
		"inputs": [
			{"internalType": "bytes", "name": "userKey", "type": "bytes"},
			{"internalType": "string", "name": "caipString", "type": "string"},
			{"internalType": "enum SmartAccountV1.OwnerType", "name": "ownerType", "type": "uint8"},
			{"internalType": "address", "name": "verifierPrecompile", "type": "address"}
		],
		"name": "deploySmartAccount",
		"outputs": [{"internalType": "address", "name": "", "type": "address"}],
		"stateMutability": "nonpayable",
		"type": "function"
	},
	{
		"inputs": [
			{"internalType": "string", "name": "caipString", "type": "string"}
		],
		"name": "computeSmartAccountAddress",
		"outputs": [{"internalType": "address", "name": "", "type": "address"}],
		"stateMutability": "view",
		"type": "function"
	}
]`

// SmartAccountV1ABI contains the ABI for the NMSC contract
const SmartAccountV1ABI = `[
	{
		"inputs": [
			{
				"components": [
					{
						"internalType": "address",
						"name": "target",
						"type": "address"
					},
					{
						"internalType": "uint256",
						"name": "value",
						"type": "uint256"
					},
					{
						"internalType": "bytes",
						"name": "data",
						"type": "bytes"
					},
					{
						"internalType": "uint256",
						"name": "gasLimit",
						"type": "uint256"
					},
					{
						"internalType": "uint256",
						"name": "maxFeePerGas",
						"type": "uint256"
					},
					{
						"internalType": "uint256",
						"name": "maxPriorityFeePerGas",
						"type": "uint256"
					},
					{
						"internalType": "uint256",
						"name": "nonce",
						"type": "uint256"
					},
					{
						"internalType": "uint256",
						"name": "deadline",
						"type": "uint256"
					}
				],
				"internalType": "struct BytesStr.CrossChainPayload",
				"name": "payload",
				"type": "tuple"
			},
			{
				"internalType": "bytes",
				"name": "signature",
				"type": "bytes"
			}
		],
		"name": "executePayload",
		"outputs": [],
		"stateMutability": "nonpayable",
		"type": "function"
	}
]`

func ParseFactoryABI() (abi.ABI, error) {
	return abi.JSON(strings.NewReader(FactoryV1ABI))
}

func ParseSmartAccountABI() (abi.ABI, error) {
	return abi.JSON(strings.NewReader(SmartAccountV1ABI))
}
