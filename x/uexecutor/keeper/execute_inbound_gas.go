package keeper

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

func (k Keeper) ExecuteInboundGas(ctx context.Context, inbound types.Inbound) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	ueModuleAccAddress, ueModuleAddressStr := k.GetUeModuleAddress(ctx)
	universalTxKey := types.GetInboundUniversalTxKey(inbound)

	// Default pcTx, will be filled along the way
	pcTx := types.PCTx{
		Sender:      ueModuleAddressStr,
		BlockHeight: uint64(sdkCtx.BlockHeight()),
		Status:      "FAILED", // assume failed until proven successful
	}

	var execErr error
	var receipt *evmtypes.MsgEthereumTxResponse

	// --- step 1: get token config
	tokenConfig, err := k.uregistryKeeper.GetTokenConfig(ctx, inbound.SourceChain, inbound.AssetAddr)
	if err != nil {
		execErr = fmt.Errorf("GetTokenConfig failed: %w", err)
	} else {
		// --- step 2: parse amount
		amount := new(big.Int)
		if amount, ok := amount.SetString(inbound.Amount, 10); !ok {
			execErr = fmt.Errorf("invalid amount: %s", inbound.Amount)
		} else {
			// --- step 3: resolve / deploy UEA
			prc20AddressHex := common.HexToAddress(tokenConfig.NativeRepresentation.ContractAddress)
			universalAccountId := types.UniversalAccountId{
				ChainNamespace: strings.Split(inbound.SourceChain, ":")[0],
				ChainId:        strings.Split(inbound.SourceChain, ":")[1],
				Owner:          inbound.Sender,
			}
			factoryAddress := common.HexToAddress(types.FACTORY_PROXY_ADDRESS_HEX)

			ueaAddr, isDeployed, fErr := k.CallFactoryToGetUEAAddressForOrigin(sdkCtx, ueModuleAccAddress, factoryAddress, &universalAccountId)
			if fErr != nil {
				execErr = fmt.Errorf("CallFactory failed: %w", fErr)
			} else {
				if !isDeployed {
					// Deploy new UEA and record a pcTx for it
					deployReceipt, dErr := k.DeployUEAV2(ctx, ueModuleAccAddress, &universalAccountId)
					if dErr != nil {
						execErr = fmt.Errorf("DeployUEA failed: %w", dErr)
					} else {
						// Parse deployed address from return data
						deployedAddr := common.BytesToAddress(deployReceipt.Ret)
						ueaAddr = deployedAddr

						// Record deployment pcTx
						deployPcTx := types.PCTx{
							TxHash:      deployReceipt.Hash,
							Sender:      ueModuleAddressStr,
							BlockHeight: uint64(sdkCtx.BlockHeight()),
							GasUsed:     deployReceipt.GasUsed,
							Status:      "SUCCESS",
						}
						_ = k.UpdateUniversalTx(ctx, universalTxKey, func(utx *types.UniversalTx) error {
							utx.PcTx = append(utx.PcTx, &deployPcTx)
							return nil
						})
					}
				}

				if execErr == nil {
					// --- step 4: deposit + swap
					receipt, execErr = k.CallPRC20DepositAutoSwap(sdkCtx, prc20AddressHex, ueaAddr, amount)
				}
			}
		}
	}

	// --- Finalize pcTx
	if execErr != nil {
		pcTx.ErrorMsg = execErr.Error()
	} else {
		pcTx.TxHash = receipt.Hash
		pcTx.GasUsed = receipt.GasUsed
		pcTx.Status = "SUCCESS"
		pcTx.ErrorMsg = ""
	}

	// --- Update UniversalTx always
	updateErr := k.UpdateUniversalTx(ctx, universalTxKey, func(utx *types.UniversalTx) error {
		utx.PcTx = append(utx.PcTx, &pcTx)
		if execErr != nil {
			utx.UniversalStatus = types.UniversalTxStatus_PC_EXECUTED_FAILED
		} else {
			utx.UniversalStatus = types.UniversalTxStatus_PC_EXECUTED_SUCCESS
		}
		return nil
	})
	if updateErr != nil {
		// if state update fails, revert the tx
		return updateErr
	}

	// Never return execErr, only nil
	return nil
}
