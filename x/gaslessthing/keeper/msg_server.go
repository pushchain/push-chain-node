package keeper

import (
	"context"
	"strings"

	// "crypto/ecdsa"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"

	// gethTypes "github.com/ethereum/go-ethereum/core/types" // for Ethereum tx
	// gethcrypto "github.com/ethereum/go-ethereum/crypto"
	// evmtypes "github.com/evmos/os/x/evm/types"
	"github.com/rollchains/pchain/x/gaslessthing/types"
)

type msgServer struct {
	k Keeper
}

var _ types.MsgServer = msgServer{}

// FactoryV1 address
var factoryAddress = common.HexToAddress("0xF3Ccb7D82aeD24CB34ffC0a0b12C8D6141a888a6")

// var fromAddress = common.HexToAddress("0xYourModuleAddress") // This module account / signer address

// ABI for only deploySmartAccount
const factoryV1ABI = `[
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
	}
]`

func NewMsgServerImpl(keeper Keeper) types.MsgServer {
	return &msgServer{k: keeper}
}

func (ms msgServer) UpdateParams(ctx context.Context, msg *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	fmt.Println("Starting UpdateParams handler")
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Get the Cosmos address in Bech32 format (from the authority in MsgUpdateParams)
	authority := msg.Authority

	// Convert the Bech32 address to sdk.AccAddress
	cosmosAddr, err := sdk.AccAddressFromBech32(authority)
	if err != nil {
		return nil, fmt.Errorf("failed to convert Bech32 address: %v", err)
	}

	// Now convert the Cosmos address to an EVM address
	evmAddress := common.BytesToAddress(cosmosAddr.Bytes()[len(cosmosAddr)-20:])

	// Now you can use `evmAddress` for your EVM calls
	// Example of using the evmAddress in your CallEVM function:
	fmt.Println("Converted EVM Address:", evmAddress.Hex())

	// Parse ABI once
	parsedABI, err := abi.JSON(strings.NewReader(factoryV1ABI))
	if err != nil {
		return nil, err
	}

	// Your arguments
	userKey := []byte("0x30ea71869947818d27b718592ea44010b458903bd9bf0370f50eda79e87d9f69") // replace
	caipString := "eip155:9000:0x44bAEA68f98d2F2c1F8dBf56f34E0636Edb54263"                  // replace
	ownerType := uint8(0)                                                                   // 0 = EVM, 1 = NonEVM (choose)
	verifierPrecompile := common.HexToAddress("0x0000000000000000000000000000000000000902") // replace

	fmt.Println("EVM call parameters:", sdkCtx,
		parsedABI,
		evmAddress,     // who is sending the transaction
		factoryAddress, // destination: your FactoryV1 contract
		true,           // commit = true (you want real tx, not simulation)
		"deploySmartAccount",
		userKey,
		caipString,
		ownerType,
		verifierPrecompile)

	// Use your keeper CallEVM directly
	receipt, err := ms.k.evmKeeper.CallEVM(
		sdkCtx,
		parsedABI,
		evmAddress,     // who is sending the transaction
		factoryAddress, // destination: your FactoryV1 contract
		true,           // commit = true (you want real tx, not simulation)
		"deploySmartAccount",
		userKey,
		caipString,
		ownerType,
		verifierPrecompile,
	)
	if err != nil {
		return nil, err
	}

	fmt.Println("EVM tx hash:", receipt.Hash)
	fmt.Println("Gas used:", receipt.GasUsed)
	fmt.Println("Logs:", receipt.Logs)
	fmt.Println("Returned data:", receipt.Ret)
	if receipt.VmError != "" {
		fmt.Println("VM Error:", receipt.VmError)
	}
	fmt.Println("Return data:", common.Bytes2Hex(receipt.Ret))

	return &types.MsgUpdateParamsResponse{}, nil
}
