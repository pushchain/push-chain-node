package keeper

import (
	"context"
	"fmt"
	"math/big"
	"sort"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/cosmos/evm/x/vm/statedb"
	"github.com/pushchain/push-chain-node/x/uregistry/types"
)

func deployProxyContract(ctx context.Context, evmKeeper types.EVMKeeper, proxyAddressHex, proxyAdminHex, implAddressHex string, runtimeBytecode []byte) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	proxyAddress := common.HexToAddress(proxyAddressHex)
	proxyAdmin := common.HexToAddress(proxyAdminHex)
	implAddress := common.HexToAddress(implAddressHex)

	// Compute the code hash from the runtime bytecode
	codeHash := crypto.Keccak256(runtimeBytecode)

	// Create the EVM account object
	evmAccount := statedb.Account{
		Nonce:    1,
		Balance:  big.NewInt(0),
		CodeHash: codeHash,
	}

	// Set the EVM account
	if err := evmKeeper.SetAccount(sdkCtx, proxyAddress, evmAccount); err != nil {
		panic("failed to set proxy contract account: " + err.Error())
	}

	// Store the runtime bytecode linked to the code hash
	evmKeeper.SetCode(sdkCtx, codeHash, runtimeBytecode)

	// Update storage slots
	evmKeeper.SetState(sdkCtx, proxyAddress, types.PROXY_ADMIN_SLOT, common.LeftPadBytes(proxyAdmin.Bytes(), 32))
	evmKeeper.SetState(sdkCtx, proxyAddress, types.PROXY_IMPLEMENTATION_SLOT, common.LeftPadBytes(implAddress.Bytes(), 32))
}

func deployImplementationContract(ctx context.Context, evmKeeper types.EVMKeeper, implAddressHex string, runtimeBytecode []byte) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	implAddress := common.HexToAddress(implAddressHex)

	// Compute the code hash from the runtime bytecode
	codeHash := crypto.Keccak256(runtimeBytecode)

	// Create the EVM account object
	evmAccount := statedb.Account{
		Nonce:    1,             // prevent tx nonce=0 conflicts
		Balance:  big.NewInt(0), // zero balance by default
		CodeHash: codeHash,
	}

	// Set the EVM account
	if err := evmKeeper.SetAccount(sdkCtx, implAddress, evmAccount); err != nil {
		panic("failed to set implementation contract account: " + err.Error())
	}

	// Store the runtime bytecode linked to the code hash
	evmKeeper.SetCode(sdkCtx, codeHash, runtimeBytecode)
}

func deployProxyAdminContract(ctx context.Context, evmKeeper types.EVMKeeper, proxyAdminContractAddress string, runtimeBytecode []byte) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	proxyAdminAddress := common.HexToAddress(proxyAdminContractAddress)
	owner := common.HexToAddress(types.PROXY_ADMIN_OWNER_ADDRESS_HEX)

	// Compute the code hash from the runtime bytecode
	codeHash := crypto.Keccak256(runtimeBytecode)

	// Create the EVM account object
	evmAccount := statedb.Account{
		Nonce:    1,             // to prevent tx nonce=0 conflicts
		Balance:  big.NewInt(0), // zero balance by default
		CodeHash: codeHash,      // link to deployed code
	}

	// Set the EVM account with the proxy admin contract
	err := evmKeeper.SetAccount(sdkCtx, proxyAdminAddress, evmAccount)
	if err != nil {
		panic("failed to set proxy admin contract account: " + err.Error())
	}

	// Store the runtime bytecode linked to the code hash
	evmKeeper.SetCode(sdkCtx, codeHash, runtimeBytecode)

	// Initialize storage slot 0 (Ownable.owner) with the owner address (left padded to 32 bytes)
	evmKeeper.SetState(sdkCtx, proxyAdminAddress, common.Hash{}, common.LeftPadBytes(owner.Bytes(), 32))
}

func deploySystemContracts(ctx context.Context, evmKeeper types.EVMKeeper, systemContracts map[string]types.ContractAddresses) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Sort contract names for deterministic iteration order across all validators
	names := make([]string, 0, len(systemContracts))
	for name := range systemContracts {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		contract := systemContracts[name]

		proxyAddr := common.HexToAddress(contract.Address)
		if isContractDeployed(sdkCtx, evmKeeper, proxyAddr) {
			sdkCtx.Logger().Info(
				"system contract already deployed, skipping",
				"name", name,
				"proxy", proxyAddr.Hex(),
			)
			continue
		}

		bytecodes, ok := types.BYTECODE[name]
		if !ok {
			panic(fmt.Sprintf("no bytecode found for contract %s", name))
		}

		// 1. Deploy ProxyAdmin with ADMIN_RUNTIME
		deployProxyAdminContract(ctx, evmKeeper, contract.ProxyAdmin, bytecodes.ADMIN_RUNTIME)

		// 2. Deploy Implementation with IMPL_RUNTIME
		deployImplementationContract(ctx, evmKeeper, contract.Implementation, bytecodes.IMPL_RUNTIME)

		// 3. Deploy Proxy with PROXY_RUNTIME
		deployProxyContract(ctx, evmKeeper, contract.Address, contract.ProxyAdmin, contract.Implementation, bytecodes.PROXY_RUNTIME)
	}
}

func isContractDeployed(
	ctx sdk.Context,
	evmKeeper types.EVMKeeper,
	addr common.Address,
) bool {
	acc := evmKeeper.GetAccount(ctx, addr)
	return acc != nil && acc.CodeHash != nil && len(acc.CodeHash) != 0
}
