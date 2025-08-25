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

const PRC20ABI = `[
    {
      "type": "function",
      "name": "GAS_LIMIT",
      "inputs": [],
      "outputs": [{ "name": "", "type": "uint256", "internalType": "uint256" }],
      "stateMutability": "view"
    },
    {
      "type": "function",
      "name": "HANDLER_CONTRACT",
      "inputs": [],
      "outputs": [{ "name": "", "type": "address", "internalType": "address" }],
      "stateMutability": "view"
    },
    {
      "type": "function",
      "name": "PC_PROTOCOL_FEE",
      "inputs": [],
      "outputs": [{ "name": "", "type": "uint256", "internalType": "uint256" }],
      "stateMutability": "view"
    },
    {
      "type": "function",
      "name": "SOURCE_CHAIN_ID",
      "inputs": [],
      "outputs": [{ "name": "", "type": "uint256", "internalType": "uint256" }],
      "stateMutability": "view"
    },
    {
      "type": "function",
      "name": "TOKEN_TYPE",
      "inputs": [],
      "outputs": [
        { "name": "", "type": "uint8", "internalType": "enum IPRC20.TokenType" }
      ],
      "stateMutability": "view"
    },
    {
      "type": "function",
      "name": "UNIVERSAL_EXECUTOR_MODULE",
      "inputs": [],
      "outputs": [{ "name": "", "type": "address", "internalType": "address" }],
      "stateMutability": "view"
    },
    {
      "type": "function",
      "name": "allowance",
      "inputs": [
        { "name": "owner", "type": "address", "internalType": "address" },
        { "name": "spender", "type": "address", "internalType": "address" }
      ],
      "outputs": [{ "name": "", "type": "uint256", "internalType": "uint256" }],
      "stateMutability": "view"
    },
    {
      "type": "function",
      "name": "approve",
      "inputs": [
        { "name": "spender", "type": "address", "internalType": "address" },
        { "name": "amount", "type": "uint256", "internalType": "uint256" }
      ],
      "outputs": [{ "name": "", "type": "bool", "internalType": "bool" }],
      "stateMutability": "nonpayable"
    },
    {
      "type": "function",
      "name": "balanceOf",
      "inputs": [
        { "name": "account", "type": "address", "internalType": "address" }
      ],
      "outputs": [{ "name": "", "type": "uint256", "internalType": "uint256" }],
      "stateMutability": "view"
    },
    {
      "type": "function",
      "name": "burn",
      "inputs": [
        { "name": "amount", "type": "uint256", "internalType": "uint256" }
      ],
      "outputs": [{ "name": "", "type": "bool", "internalType": "bool" }],
      "stateMutability": "nonpayable"
    },
    {
      "type": "function",
      "name": "decimals",
      "inputs": [],
      "outputs": [{ "name": "", "type": "uint8", "internalType": "uint8" }],
      "stateMutability": "view"
    },
    {
      "type": "function",
      "name": "deposit",
      "inputs": [
        { "name": "to", "type": "address", "internalType": "address" },
        { "name": "amount", "type": "uint256", "internalType": "uint256" }
      ],
      "outputs": [{ "name": "", "type": "bool", "internalType": "bool" }],
      "stateMutability": "nonpayable"
    },
    {
      "type": "function",
      "name": "name",
      "inputs": [],
      "outputs": [{ "name": "", "type": "string", "internalType": "string" }],
      "stateMutability": "view"
    },
    {
      "type": "function",
      "name": "setName",
      "inputs": [
        { "name": "newName", "type": "string", "internalType": "string" }
      ],
      "outputs": [],
      "stateMutability": "nonpayable"
    },
    {
      "type": "function",
      "name": "setSymbol",
      "inputs": [
        { "name": "newSymbol", "type": "string", "internalType": "string" }
      ],
      "outputs": [],
      "stateMutability": "nonpayable"
    },
    {
      "type": "function",
      "name": "symbol",
      "inputs": [],
      "outputs": [{ "name": "", "type": "string", "internalType": "string" }],
      "stateMutability": "view"
    },
    {
      "type": "function",
      "name": "totalSupply",
      "inputs": [],
      "outputs": [{ "name": "", "type": "uint256", "internalType": "uint256" }],
      "stateMutability": "view"
    },
    {
      "type": "function",
      "name": "transfer",
      "inputs": [
        { "name": "recipient", "type": "address", "internalType": "address" },
        { "name": "amount", "type": "uint256", "internalType": "uint256" }
      ],
      "outputs": [{ "name": "", "type": "bool", "internalType": "bool" }],
      "stateMutability": "nonpayable"
    },
    {
      "type": "function",
      "name": "transferFrom",
      "inputs": [
        { "name": "sender", "type": "address", "internalType": "address" },
        { "name": "recipient", "type": "address", "internalType": "address" },
        { "name": "amount", "type": "uint256", "internalType": "uint256" }
      ],
      "outputs": [{ "name": "", "type": "bool", "internalType": "bool" }],
      "stateMutability": "nonpayable"
    },
    {
      "type": "function",
      "name": "updateGasLimit",
      "inputs": [
        { "name": "gasLimit_", "type": "uint256", "internalType": "uint256" }
      ],
      "outputs": [],
      "stateMutability": "nonpayable"
    },
    {
      "type": "function",
      "name": "updateHandlerContract",
      "inputs": [
        { "name": "addr", "type": "address", "internalType": "address" }
      ],
      "outputs": [],
      "stateMutability": "nonpayable"
    },
    {
      "type": "function",
      "name": "updateProtocolFlatFee",
      "inputs": [
        {
          "name": "protocolFlatFee_",
          "type": "uint256",
          "internalType": "uint256"
        }
      ],
      "outputs": [],
      "stateMutability": "nonpayable"
    },
    {
      "type": "function",
      "name": "withdraw",
      "inputs": [
        { "name": "to", "type": "bytes", "internalType": "bytes" },
        { "name": "amount", "type": "uint256", "internalType": "uint256" }
      ],
      "outputs": [{ "name": "", "type": "bool", "internalType": "bool" }],
      "stateMutability": "nonpayable"
    },
    {
      "type": "function",
      "name": "withdrawGasFee",
      "inputs": [],
      "outputs": [
        { "name": "gasToken", "type": "address", "internalType": "address" },
        { "name": "gasFee", "type": "uint256", "internalType": "uint256" }
      ],
      "stateMutability": "view"
    },
    {
      "type": "function",
      "name": "withdrawGasFeeWithGasLimit",
      "inputs": [
        { "name": "gasLimit_", "type": "uint256", "internalType": "uint256" }
      ],
      "outputs": [
        { "name": "gasToken", "type": "address", "internalType": "address" },
        { "name": "gasFee", "type": "uint256", "internalType": "uint256" }
      ],
      "stateMutability": "view"
    },
    {
      "type": "event",
      "name": "Approval",
      "inputs": [
        {
          "name": "owner",
          "type": "address",
          "indexed": true,
          "internalType": "address"
        },
        {
          "name": "spender",
          "type": "address",
          "indexed": true,
          "internalType": "address"
        },
        {
          "name": "value",
          "type": "uint256",
          "indexed": false,
          "internalType": "uint256"
        }
      ],
      "anonymous": false
    },
    {
      "type": "event",
      "name": "Deposit",
      "inputs": [
        {
          "name": "from",
          "type": "bytes",
          "indexed": false,
          "internalType": "bytes"
        },
        {
          "name": "to",
          "type": "address",
          "indexed": false,
          "internalType": "address"
        },
        {
          "name": "amount",
          "type": "uint256",
          "indexed": false,
          "internalType": "uint256"
        }
      ],
      "anonymous": false
    },
    {
      "type": "event",
      "name": "Transfer",
      "inputs": [
        {
          "name": "from",
          "type": "address",
          "indexed": true,
          "internalType": "address"
        },
        {
          "name": "to",
          "type": "address",
          "indexed": true,
          "internalType": "address"
        },
        {
          "name": "value",
          "type": "uint256",
          "indexed": false,
          "internalType": "uint256"
        }
      ],
      "anonymous": false
    },
    {
      "type": "event",
      "name": "UpdatedGasLimit",
      "inputs": [
        {
          "name": "gasLimit",
          "type": "uint256",
          "indexed": false,
          "internalType": "uint256"
        }
      ],
      "anonymous": false
    },
    {
      "type": "event",
      "name": "UpdatedHandlerContract",
      "inputs": [
        {
          "name": "handler",
          "type": "address",
          "indexed": false,
          "internalType": "address"
        }
      ],
      "anonymous": false
    },
    {
      "type": "event",
      "name": "UpdatedProtocolFlatFee",
      "inputs": [
        {
          "name": "protocolFlatFee",
          "type": "uint256",
          "indexed": false,
          "internalType": "uint256"
        }
      ],
      "anonymous": false
    },
    {
      "type": "event",
      "name": "Withdrawal",
      "inputs": [
        {
          "name": "from",
          "type": "address",
          "indexed": false,
          "internalType": "address"
        },
        {
          "name": "to",
          "type": "bytes",
          "indexed": false,
          "internalType": "bytes"
        },
        {
          "name": "amount",
          "type": "uint256",
          "indexed": false,
          "internalType": "uint256"
        },
        {
          "name": "gasFee",
          "type": "uint256",
          "indexed": false,
          "internalType": "uint256"
        },
        {
          "name": "protocolFlatFee",
          "type": "uint256",
          "indexed": false,
          "internalType": "uint256"
        }
      ],
      "anonymous": false
    },
    { "type": "error", "name": "CallerIsNotUniversalExecutor", "inputs": [] },
    { "type": "error", "name": "GasFeeTransferFailed", "inputs": [] },
    { "type": "error", "name": "InvalidSender", "inputs": [] },
    { "type": "error", "name": "LowAllowance", "inputs": [] },
    { "type": "error", "name": "LowBalance", "inputs": [] },
    { "type": "error", "name": "ZeroAddress", "inputs": [] },
    { "type": "error", "name": "ZeroAmount", "inputs": [] },
    { "type": "error", "name": "ZeroGasPrice", "inputs": [] },
    { "type": "error", "name": "ZerogasToken", "inputs": [] }
  ]`

func ParseFactoryABI() (abi.ABI, error) {
	return abi.JSON(strings.NewReader(FactoryV1ABI))
}

func ParseUeaABI() (abi.ABI, error) {
	return abi.JSON(strings.NewReader(UeaV1ABI))
}

func ParsePRC20ABI() (abi.ABI, error) {
	return abi.JSON(strings.NewReader(PRC20ABI))
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
