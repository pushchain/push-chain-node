package types

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

// FactoryV1ABI contains the ABI for the deploySmartAccount function only.
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

type AbiCrossChainPayload struct {
	Target               common.Address
	Value                *big.Int
	Data                 []byte
	GasLimit             *big.Int
	MaxFeePerGas         *big.Int
	MaxPriorityFeePerGas *big.Int
	Nonce                *big.Int
	Deadline             *big.Int
}
