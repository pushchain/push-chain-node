package types

import (
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/rollchains/pchain/utils"
)

// FactoryV1ABI contains the ABI for the factory contract
const FactoryV1ABI = `[
	{
      "type": "function",
      "name": "deploySmartAccount",
      "inputs": [
        {
          "name": "_id",
          "type": "tuple",
          "internalType": "struct AccountId",
          "components": [
            { "name": "namespace", "type": "string", "internalType": "string" },
            { "name": "chainId", "type": "string", "internalType": "string" },
            { "name": "ownerKey", "type": "bytes", "internalType": "bytes" },
            {
              "name": "vmType",
              "type": "uint8",
              "internalType": "enum VM_TYPE"
            }
          ]
        }
      ],
      "outputs": [{ "name": "", "type": "address", "internalType": "address" }],
      "stateMutability": "nonpayable"
    },
	{
      "type": "function",
      "name": "computeSmartAccountAddress",
      "inputs": [
        {
          "name": "_id",
          "type": "tuple",
          "internalType": "struct AccountId",
          "components": [
            { "name": "namespace", "type": "string", "internalType": "string" },
            { "name": "chainId", "type": "string", "internalType": "string" },
            { "name": "ownerKey", "type": "bytes", "internalType": "bytes" },
            {
              "name": "vmType",
              "type": "uint8",
              "internalType": "enum VM_TYPE"
            }
          ]
        }
      ],
      "outputs": [{ "name": "", "type": "address", "internalType": "address" }],
      "stateMutability": "view"
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

func NewAbiCrossChainPayload(proto *CrossChainPayload) (AbiCrossChainPayload, error) {
	data, err := utils.HexToBytes(proto.Data)
	if err != nil {
		return AbiCrossChainPayload{}, err
	}
	return AbiCrossChainPayload{
		Target:               common.HexToAddress(proto.Target),
		Value:                utils.StringToBigInt(proto.Value),
		Data:                 data,
		GasLimit:             utils.StringToBigInt(proto.GasLimit),
		MaxFeePerGas:         utils.StringToBigInt(proto.MaxFeePerGas),
		MaxPriorityFeePerGas: utils.StringToBigInt(proto.MaxPriorityFeePerGas),
		Nonce:                utils.StringToBigInt(proto.Nonce),
		Deadline:             utils.StringToBigInt(proto.Deadline),
	}, nil
}

type AbiAccountId struct {
	Namespace string
	ChainId   string
	OwnerKey  []byte
	VmType    uint8
}

func NewAbiAccountId(proto *AccountId) (AbiAccountId, error) {
	ownerKey, err := utils.HexToBytes(proto.OwnerKey)
	if err != nil {
		return AbiAccountId{}, err
	}

	return AbiAccountId{
		Namespace: proto.Namespace,
		ChainId:   proto.ChainId,
		OwnerKey:  ownerKey,
		VmType:    uint8(proto.VmType),
	}, nil
}
