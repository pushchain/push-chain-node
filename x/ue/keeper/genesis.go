package keeper

import (
	"context"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/evmos/os/x/evm/statedb"
	"github.com/rollchains/pchain/x/ue/types"
)

func deployFactoryContract(ctx context.Context, evmKeeper types.EVMKeeper) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	factoryAddress := common.HexToAddress(types.FACTORY_ADDRESS_HEX)
	owner := common.HexToAddress(types.FACTORY_OWNER_ADDRESS_HEX)

	// Compute the code hash from the runtime bytecode
	codeHash := crypto.Keccak256(types.FactoryRuntimeBytecode)

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
	evmKeeper.SetCode(sdkCtx, codeHash, types.FactoryRuntimeBytecode)

	// Initialize storage slot 0 (Ownable.owner) with the owner address (left padded to 32 bytes)
	evmKeeper.SetState(sdkCtx, factoryAddress, common.Hash{}, common.LeftPadBytes(owner.Bytes(), 32))
}
