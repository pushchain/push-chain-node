package types

import (
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pushchain/push-chain-node/utils"
)

// FactoryV1ABI contains the ABI for the factory contract
const FactoryV1ABI = `[
	{
    "type": "function",
    "name": "deployUEA",
    "inputs": [
      {
        "name": "_id",
        "type": "tuple",
        "internalType": "struct UniversalAccountId",
        "components": [
          {
            "name": "chainNamespace",
            "type": "string",
            "internalType": "string"
          },
          { "name": "chainId", "type": "string", "internalType": "string" },
          { "name": "owner", "type": "bytes", "internalType": "bytes" }
        ]
      }
    ],
    "outputs": [{ "name": "", "type": "address", "internalType": "address" }],
    "stateMutability": "nonpayable"
  },
  {
    "type": "function",
    "name": "computeUEA",
    "inputs": [
      {
        "name": "_id",
        "type": "tuple",
        "internalType": "struct UniversalAccountId",
        "components": [
          {
            "name": "chainNamespace",
            "type": "string",
            "internalType": "string"
          },
          { "name": "chainId", "type": "string", "internalType": "string" },
          { "name": "owner", "type": "bytes", "internalType": "bytes" }
        ]
      }
    ],
    "outputs": [{ "name": "", "type": "address", "internalType": "address" }],
    "stateMutability": "view"
  },
  {
    "type": "function",
    "name": "getUEAForOrigin",
    "inputs": [
      {
        "name": "_id",
        "type": "tuple",
        "internalType": "struct UniversalAccountId",
        "components": [
          {
            "name": "chainNamespace",
            "type": "string",
            "internalType": "string"
          },
          { "name": "chainId", "type": "string", "internalType": "string" },
          { "name": "owner", "type": "bytes", "internalType": "bytes" }
        ]
      }
    ],
    "outputs": [
      { "name": "uea", "type": "address", "internalType": "address" },
      { "name": "isDeployed", "type": "bool", "internalType": "bool" }
    ],
    "stateMutability": "view"
  }
]`

// UeaV1ABI contains the ABI for the UEA contract
const UeaV1ABI = `[
	{
      "type": "function",
      "name": "executePayload",
      "inputs": [
        {
          "name": "payload",
          "type": "tuple",
          "internalType": "struct UniversalPayload",
          "components": [
            { "name": "to", "type": "address", "internalType": "address" },
            { "name": "value", "type": "uint256", "internalType": "uint256" },
            { "name": "data", "type": "bytes", "internalType": "bytes" },
            {
              "name": "gasLimit",
              "type": "uint256",
              "internalType": "uint256"
            },
            {
              "name": "maxFeePerGas",
              "type": "uint256",
              "internalType": "uint256"
            },
            {
              "name": "maxPriorityFeePerGas",
              "type": "uint256",
              "internalType": "uint256"
            },
            { "name": "nonce", "type": "uint256", "internalType": "uint256" },
            {
              "name": "deadline",
              "type": "uint256",
              "internalType": "uint256"
            },
            {
              "name": "vType",
              "type": "uint8",
              "internalType": "enum VerificationType"
            }
          ]
        },
        { "name": "verificationData", "type": "bytes", "internalType": "bytes" }
      ],
      "outputs": [],
      "stateMutability": "nonpayable"
    }
]`

func ParseFactoryABI() (abi.ABI, error) {
	return abi.JSON(strings.NewReader(FactoryV1ABI))
}

func ParseUeaABI() (abi.ABI, error) {
	return abi.JSON(strings.NewReader(UeaV1ABI))
}

type AbiUniversalPayload struct {
	To                   common.Address
	Value                *big.Int
	Data                 []byte
	GasLimit             *big.Int
	MaxFeePerGas         *big.Int
	MaxPriorityFeePerGas *big.Int
	Nonce                *big.Int
	Deadline             *big.Int
	VType                uint8
}

func NewAbiUniversalPayload(proto *UniversalPayload) (AbiUniversalPayload, error) {
	data, err := utils.HexToBytes(proto.Data)
	if err != nil {
		return AbiUniversalPayload{}, err
	}
	return AbiUniversalPayload{
		To:                   common.HexToAddress(proto.To),
		Value:                utils.StringToBigInt(proto.Value),
		Data:                 data,
		GasLimit:             utils.StringToBigInt(proto.GasLimit),
		MaxFeePerGas:         utils.StringToBigInt(proto.MaxFeePerGas),
		MaxPriorityFeePerGas: utils.StringToBigInt(proto.MaxPriorityFeePerGas),
		Nonce:                utils.StringToBigInt(proto.Nonce),
		Deadline:             utils.StringToBigInt(proto.Deadline),
		VType:                uint8(proto.VType),
	}, nil
}

type AbiUniversalAccountId struct {
	ChainNamespace string
	ChainId        string
	Owner          []byte
}

func NewAbiUniversalAccountId(proto *UniversalAccountId) (AbiUniversalAccountId, error) {
	owner, err := utils.HexToBytes(proto.Owner)
	if err != nil {
		return AbiUniversalAccountId{}, err
	}

	return AbiUniversalAccountId{
		ChainNamespace: proto.ChainNamespace,
		ChainId:        proto.ChainId,
		Owner:          owner,
	}, nil
}
