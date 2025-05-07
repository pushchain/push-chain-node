package types

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
