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
	sdkCtx.Logger().Info(
		"ExecuteInboundGas: start",
		"universal_tx_key", universalTxKey,
		"source_chain", inbound.SourceChain,
		"asset_addr", inbound.AssetAddr,
		"amount", inbound.Amount,
		"sender", inbound.Sender,
		"tx_hash", inbound.TxHash,
	)

	// Default pcTx, will be filled along the way
	pcTx := types.PCTx{
		Sender:      ueModuleAddressStr,
		BlockHeight: uint64(sdkCtx.BlockHeight()),
		Status:      "FAILED", // assume failed until proven successful
	}

	var execErr error
	var receipt *evmtypes.MsgEthereumTxResponse

	shouldRevert := false
	var revertReason string

	// --- step 1: get token config
	sdkCtx.Logger().Info("ExecuteInboundGas: fetching token config")
	tokenConfig, err := k.uregistryKeeper.GetTokenConfig(ctx, inbound.SourceChain, inbound.AssetAddr)
	if err != nil {
		execErr = fmt.Errorf("GetTokenConfig failed: %w", err)
		shouldRevert = true
		revertReason = execErr.Error()
		sdkCtx.Logger().Error("ExecuteInboundGas: GetTokenConfig failed", "error", execErr.Error())
	} else {
		sdkCtx.Logger().Info(
			"ExecuteInboundGas: token config fetched",
			"native_contract", tokenConfig.NativeRepresentation.ContractAddress,
		)
		// --- step 2: parse amount
		sdkCtx.Logger().Info("ExecuteInboundGas: parsing amount")
		amount := new(big.Int)
		if amount, ok := amount.SetString(inbound.Amount, 10); !ok {
			execErr = fmt.Errorf("invalid amount: %s", inbound.Amount)
			shouldRevert = true
			revertReason = execErr.Error()
			sdkCtx.Logger().Error("ExecuteInboundGas: invalid amount", "amount", inbound.Amount)
		} else {
			sdkCtx.Logger().Info("ExecuteInboundGas: amount parsed", "amount", amount.String())
			// --- step 3: resolve / deploy UEA
			prc20AddressHex := common.HexToAddress(tokenConfig.NativeRepresentation.ContractAddress)
			universalAccountId := types.UniversalAccountId{
				ChainNamespace: strings.Split(inbound.SourceChain, ":")[0],
				ChainId:        strings.Split(inbound.SourceChain, ":")[1],
				Owner:          inbound.Sender,
			}
			factoryAddress := common.HexToAddress(types.FACTORY_PROXY_ADDRESS_HEX)
			sdkCtx.Logger().Info(
				"ExecuteInboundGas: resolving UEA from factory",
				"factory_address", factoryAddress.Hex(),
				"chain_namespace", universalAccountId.ChainNamespace,
				"chain_id", universalAccountId.ChainId,
				"owner", universalAccountId.Owner,
			)

			ueaAddr, isDeployed, fErr := k.CallFactoryToGetUEAAddressForOrigin(sdkCtx, ueModuleAccAddress, factoryAddress, &universalAccountId)
			if fErr != nil {
				execErr = fmt.Errorf("CallFactory failed: %w", fErr)
				shouldRevert = true
				revertReason = execErr.Error()
				sdkCtx.Logger().Error("ExecuteInboundGas: CallFactory failed", "error", execErr.Error())
			} else {
				sdkCtx.Logger().Info("ExecuteInboundGas: factory returned UEA", "uea_addr", ueaAddr.Hex(), "is_deployed", isDeployed)
				if !isDeployed {
					// Deploy new UEA and record a pcTx for it
					sdkCtx.Logger().Info("ExecuteInboundGas: deploying new UEA")
					deployReceipt, dErr := k.DeployUEAV2(ctx, ueModuleAccAddress, &universalAccountId)
					if dErr != nil {
						execErr = fmt.Errorf("DeployUEA failed: %w", dErr)
						shouldRevert = true
						revertReason = execErr.Error()
						sdkCtx.Logger().Error("ExecuteInboundGas: DeployUEAV2 failed", "error", execErr.Error())
					} else {
						// Parse deployed address from return data
						deployedAddr := common.BytesToAddress(deployReceipt.Ret)
						ueaAddr = deployedAddr
						sdkCtx.Logger().Info(
							"ExecuteInboundGas: UEA deployed",
							"uea_addr", deployedAddr.Hex(),
							"deploy_tx_hash", deployReceipt.Hash,
							"deploy_gas_used", deployReceipt.GasUsed,
						)

						// Record deployment pcTx
						deployPcTx := types.PCTx{
							TxHash:      deployReceipt.Hash,
							Sender:      ueModuleAddressStr,
							BlockHeight: uint64(sdkCtx.BlockHeight()),
							GasUsed:     deployReceipt.GasUsed,
							Status:      "SUCCESS",
						}
						sdkCtx.Logger().Info("ExecuteInboundGas: appending deploy PCTx")
						_ = k.UpdateUniversalTx(ctx, universalTxKey, func(utx *types.UniversalTx) error {
							utx.PcTx = append(utx.PcTx, &deployPcTx)
							return nil
						})
						sdkCtx.Logger().Info("ExecuteInboundGas: deploy PCTx append attempted")
					}
				}

				if execErr == nil {
					// --- step 4: deposit + swap
					sdkCtx.Logger().Info(
						"ExecuteInboundGas: calling CallPRC20DepositAutoSwap",
						"prc20", prc20AddressHex.Hex(),
						"uea_addr", ueaAddr.Hex(),
						"amount", amount.String(),
					)
					receipt, execErr = k.CallPRC20DepositAutoSwap(sdkCtx, prc20AddressHex, ueaAddr, amount)
					if execErr != nil {
						shouldRevert = true
						revertReason = execErr.Error()
						sdkCtx.Logger().Error("ExecuteInboundGas: CallPRC20DepositAutoSwap failed", "error", execErr.Error())
					} else {
						sdkCtx.Logger().Info(
							"ExecuteInboundGas: CallPRC20DepositAutoSwap succeeded",
							"tx_hash", receipt.Hash,
							"gas_used", receipt.GasUsed,
						)
					}
				}
			}
		}
	}

	// --- Finalize pcTx
	if execErr != nil {
		pcTx.ErrorMsg = execErr.Error()
		sdkCtx.Logger().Error("ExecuteInboundGas: final pcTx marked failed", "error", pcTx.ErrorMsg)
	} else {
		pcTx.TxHash = receipt.Hash
		pcTx.GasUsed = receipt.GasUsed
		pcTx.Status = "SUCCESS"
		pcTx.ErrorMsg = ""
		sdkCtx.Logger().Info(
			"ExecuteInboundGas: final pcTx marked success",
			"tx_hash", pcTx.TxHash,
			"gas_used", pcTx.GasUsed,
		)
	}

	// --- Update UniversalTx always
	sdkCtx.Logger().Info("ExecuteInboundGas: updating universal tx", "universal_tx_key", universalTxKey)
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
		sdkCtx.Logger().Error("ExecuteInboundGas: UpdateUniversalTx failed", "error", updateErr.Error())
		return updateErr
	}
	sdkCtx.Logger().Info("ExecuteInboundGas: universal tx updated")

	if execErr != nil && shouldRevert {
		sdkCtx.Logger().Info("ExecuteInboundGas: preparing revert outbound", "reason", revertReason)
		revertOutbound := &types.OutboundTx{
			DestinationChain: inbound.SourceChain,
			Recipient: func() string {
				if inbound.RevertInstructions != nil {
					return inbound.RevertInstructions.FundRecipient
				}
				return inbound.Sender
			}(),
			Amount:            inbound.Amount,
			ExternalAssetAddr: inbound.AssetAddr,
			Sender:            inbound.Sender,
			TxType:            types.TxType_INBOUND_REVERT,
			OutboundStatus:    types.Status_PENDING,
			Id:                types.GetOutboundRevertId(inbound.TxHash),
		}

		sdkCtx.Logger().Info(
			"ExecuteInboundGas: attaching revert outbound",
			"outbound_id", revertOutbound.Id,
			"destination_chain", revertOutbound.DestinationChain,
			"recipient", revertOutbound.Recipient,
		)
		_ = k.attachOutboundsToUtx(
			sdkCtx,
			universalTxKey,
			[]*types.OutboundTx{revertOutbound},
			revertReason,
		)
		sdkCtx.Logger().Info("ExecuteInboundGas: revert outbound attach attempted")
	}

	// Never return execErr, only nil
	sdkCtx.Logger().Info("ExecuteInboundGas: done", "has_exec_error", execErr != nil)
	return nil
}
