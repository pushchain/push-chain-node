package keeper

import (
	"context"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/cosmos/evm/x/vm/statedb"
	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

func deployFactoryProxy(ctx context.Context, evmKeeper types.EVMKeeper) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	proxyAddress := common.HexToAddress(types.FACTORY_PROXY_ADDRESS_HEX)
	proxyAdminOwner := common.HexToAddress(types.PROXY_ADMIN_ADDRESS_HEX)
	factoryImplAddress := common.HexToAddress(types.FACTORY_IMPL_ADDRESS_HEX)

	// Compute the code hash from the runtime bytecode
	codeHash := crypto.Keccak256(types.ProxyRuntimeBytecode)

	// Create the EVM account object
	evmAccount := statedb.Account{
		Nonce:    1,             // to prevent tx nonce=0 conflicts
		Balance:  big.NewInt(0), // zero balance by default
		CodeHash: codeHash,      // link to deployed code
	}

	// Set the EVM account with the factory proxy contract
	err := evmKeeper.SetAccount(sdkCtx, proxyAddress, evmAccount)
	if err != nil {
		panic("failed to set factory proxy contract account: " + err.Error())
	}

	// Store the runtime bytecode linked to the code hash
	evmKeeper.SetCode(sdkCtx, codeHash, types.ProxyRuntimeBytecode)

	// Update proxyAdmin Slot with the proxyAdmin owner address (left padded to 32 bytes)
	evmKeeper.SetState(sdkCtx, proxyAddress, types.PROXY_ADMIN_SLOT, common.LeftPadBytes(proxyAdminOwner.Bytes(), 32))

	// Update proxyImplementation Slot with the factory implementation address (left padded to 32 bytes)
	evmKeeper.SetState(sdkCtx, proxyAddress, types.PROXY_IMPLEMENTATION_SLOT, common.LeftPadBytes(factoryImplAddress.Bytes(), 32))
}

func deployFactoryImplContract(ctx context.Context, evmKeeper types.EVMKeeper) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	factoryAddress := common.HexToAddress(types.FACTORY_IMPL_ADDRESS_HEX)

	// Compute the code hash from the runtime bytecode
	codeHash := crypto.Keccak256(types.FactoryImplRuntimeBytecode)

	// Create the EVM account object
	evmAccount := statedb.Account{
		Nonce:    1,             // to prevent tx nonce=0 conflicts
		Balance:  big.NewInt(0), // zero balance by default
		CodeHash: codeHash,      // link to deployed code
	}

	// Set the EVM account with the factory contract
	err := evmKeeper.SetAccount(sdkCtx, factoryAddress, evmAccount)
	if err != nil {
		panic("failed to set factory contract account: " + err.Error())
	}

	// Store the runtime bytecode linked to the code hash
	evmKeeper.SetCode(sdkCtx, codeHash, types.FactoryImplRuntimeBytecode)
}

func deployProxyAdminContract(ctx context.Context, evmKeeper types.EVMKeeper) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	proxyAdminAddress := common.HexToAddress(types.PROXY_ADMIN_ADDRESS_HEX)
	owner := common.HexToAddress(types.PROXY_ADMIN_OWNER_ADDRESS_HEX)

	// Compute the code hash from the runtime bytecode
	codeHash := crypto.Keccak256(types.ProxyAdminRuntimeBytecode)

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
	evmKeeper.SetCode(sdkCtx, codeHash, types.ProxyAdminRuntimeBytecode)

	// Initialize storage slot 0 (Ownable.owner) with the owner address (left padded to 32 bytes)
	evmKeeper.SetState(sdkCtx, proxyAdminAddress, common.Hash{}, common.LeftPadBytes(owner.Bytes(), 32))
}

func deployFactoryEA(ctx context.Context, evmKeeper types.EVMKeeper) {
	// Deploy the factory implementation contract
	deployFactoryImplContract(ctx, evmKeeper)

	// Deploy the proxy admin contract
	deployProxyAdminContract(ctx, evmKeeper)

	// Deploy the factory proxy contract
	deployFactoryProxy(ctx, evmKeeper)
}
