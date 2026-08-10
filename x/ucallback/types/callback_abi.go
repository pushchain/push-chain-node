package types

import (
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
)

// Method names on UniversalCallback. Both are module-gated: the contract admits
// only the x/ucallback module account as caller.
const (
	MethodFulfillExternalCallback = "fulfillExternalCallback"
	MethodExpireExternalRead      = "expireExternalRead"
)

// universalCallbackABI covers only the two module-gated entry points x/ucallback
// calls. Transcribed from push-chain-core-contracts
// src/UniversalCallback.sol:150 and :197.
//
// Deliberately not the full contract ABI: everything else on UniversalCallback is
// either user-facing or read-only, and a narrower fragment is one less thing to
// keep in sync with the contract.
const universalCallbackABI = `[
  {
    "type": "function",
    "name": "fulfillExternalCallback",
    "stateMutability": "nonpayable",
    "inputs": [
      {"name": "requestId",           "type": "uint256"},
      {"name": "resultData",          "type": "bytes"},
      {"name": "observedBlockHeight", "type": "uint64"},
      {"name": "observedBlockHash",   "type": "bytes32"}
    ],
    "outputs": []
  },
  {
    "type": "function",
    "name": "expireExternalRead",
    "stateMutability": "nonpayable",
    "inputs": [{"name": "requestId", "type": "uint256"}],
    "outputs": []
  }
]`

var parsedCallbackABI abi.ABI

func init() {
	parsed, err := abi.JSON(strings.NewReader(universalCallbackABI))
	if err != nil {
		panic(fmt.Sprintf("ucallback: bad UniversalCallback ABI: %v", err))
	}
	parsedCallbackABI = parsed
}

// ParseUniversalCallbackABI returns the parsed fragment for UniversalCallback.
func ParseUniversalCallbackABI() (abi.ABI, error) {
	return parsedCallbackABI, nil
}
