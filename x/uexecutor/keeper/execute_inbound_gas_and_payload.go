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

func (k Keeper) ExecuteInboundGasAndPayload(ctx context.Context, utx types.UniversalTx) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	ueModuleAccAddress, ueModuleAddressStr := k.GetUeModuleAddress(ctx)
	universalTxKey := types.GetInboundUniversalTxKey(*utx.InboundTx)
	sdkCtx.Logger().Info(
		"ExecuteInboundGasAndPayload: start",
		"utx_id", utx.Id,
		"universal_tx_key", universalTxKey,
		"source_chain", utx.InboundTx.SourceChain,
		"asset_addr", utx.InboundTx.AssetAddr,
		"amount", utx.InboundTx.Amount,
		"sender", utx.InboundTx.Sender,
		"tx_hash", utx.InboundTx.TxHash,
	)

	universalAccountId := types.UniversalAccountId{
		ChainNamespace: strings.Split(utx.InboundTx.SourceChain, ":")[0],
		ChainId:        strings.Split(utx.InboundTx.SourceChain, ":")[1],
		Owner:          utx.InboundTx.Sender,
	}
	sdkCtx.Logger().Info(
		"ExecuteInboundGasAndPayload: universal account id built",
		"chain_namespace", universalAccountId.ChainNamespace,
		"chain_id", universalAccountId.ChainId,
		"owner", universalAccountId.Owner,
	)

	factoryAddress := common.HexToAddress(types.FACTORY_PROXY_ADDRESS_HEX)
	sdkCtx.Logger().Info("ExecuteInboundGasAndPayload: factory address resolved", "factory_address", factoryAddress.Hex())

	var execErr error
	var receipt *evmtypes.MsgEthereumTxResponse
	var ueaAddr common.Address

	shouldRevert := false
	var revertReason string

	// --- Step 1: token config
	sdkCtx.Logger().Info("ExecuteInboundGasAndPayload: fetching token config")
	tokenConfig, err := k.uregistryKeeper.GetTokenConfig(ctx, utx.InboundTx.SourceChain, utx.InboundTx.AssetAddr)
	if err != nil {
		execErr = fmt.Errorf("GetTokenConfig failed: %w", err)
		shouldRevert = true
		revertReason = execErr.Error()
		sdkCtx.Logger().Error("ExecuteInboundGasAndPayload: GetTokenConfig failed", "error", execErr.Error())
	} else {
		sdkCtx.Logger().Info(
			"ExecuteInboundGasAndPayload: token config fetched",
			"native_contract", tokenConfig.NativeRepresentation.ContractAddress,
		)
		// --- Step 2: parse amount
		sdkCtx.Logger().Info("ExecuteInboundGasAndPayload: parsing amount")
		amount := new(big.Int)
		if amount, ok := amount.SetString(utx.InboundTx.Amount, 10); !ok {
			execErr = fmt.Errorf("invalid amount: %s", utx.InboundTx.Amount)
			shouldRevert = true
			revertReason = execErr.Error()
			sdkCtx.Logger().Error("ExecuteInboundGasAndPayload: invalid amount", "amount", utx.InboundTx.Amount)
		} else {
			sdkCtx.Logger().Info("ExecuteInboundGasAndPayload: amount parsed", "amount", amount.String())
			// --- Step 3: resolve / deploy UEA
			sdkCtx.Logger().Info("ExecuteInboundGasAndPayload: resolving UEA from factory")
			ueaAddrRes, isDeployed, fErr := k.CallFactoryToGetUEAAddressForOrigin(
				sdkCtx,
				ueModuleAccAddress,
				factoryAddress,
				&universalAccountId,
			)
			if fErr != nil {
				execErr = fmt.Errorf("factory lookup failed: %w", fErr)
				shouldRevert = true
				revertReason = execErr.Error()
				sdkCtx.Logger().Error("ExecuteInboundGasAndPayload: factory lookup failed", "error", execErr.Error())
			} else {
				ueaAddr = ueaAddrRes
				sdkCtx.Logger().Info("ExecuteInboundGasAndPayload: factory returned UEA", "uea_addr", ueaAddr.Hex(), "is_deployed", isDeployed)

				if !isDeployed {
					sdkCtx.Logger().Info("ExecuteInboundGasAndPayload: deploying UEA")
					deployReceipt, dErr := k.DeployUEAV2(ctx, ueModuleAccAddress, &universalAccountId)
					if dErr != nil {
						execErr = fmt.Errorf("DeployUEAV2 failed: %w", dErr)
						shouldRevert = true
						revertReason = execErr.Error()
						sdkCtx.Logger().Error("ExecuteInboundGasAndPayload: DeployUEAV2 failed", "error", execErr.Error())
					} else {
						ueaAddr = common.BytesToAddress(deployReceipt.Ret)
						sdkCtx.Logger().Info(
							"ExecuteInboundGasAndPayload: UEA deployed",
							"uea_addr", ueaAddr.Hex(),
							"deploy_tx_hash", deployReceipt.Hash,
							"deploy_gas_used", deployReceipt.GasUsed,
						)

						deployPcTx := types.PCTx{
							TxHash:      deployReceipt.Hash,
							Sender:      ueModuleAddressStr,
							BlockHeight: uint64(sdkCtx.BlockHeight()),
							GasUsed:     deployReceipt.GasUsed,
							Status:      "SUCCESS",
						}
						sdkCtx.Logger().Info("ExecuteInboundGasAndPayload: appending deploy PCTx")
						_ = k.UpdateUniversalTx(ctx, universalTxKey, func(utx *types.UniversalTx) error {
							utx.PcTx = append(utx.PcTx, &deployPcTx)
							return nil
						})
						sdkCtx.Logger().Info("ExecuteInboundGasAndPayload: deploy PCTx append attempted")
					}
				}

				if execErr == nil {
					// --- Step 4: deposit + autoswap
					prc20AddressHex := common.HexToAddress(
						tokenConfig.NativeRepresentation.ContractAddress,
					)
					sdkCtx.Logger().Info(
						"ExecuteInboundGasAndPayload: calling CallPRC20DepositAutoSwap",
						"prc20", prc20AddressHex.Hex(),
						"uea_addr", ueaAddr.Hex(),
						"amount", amount.String(),
					)
					receipt, execErr = k.CallPRC20DepositAutoSwap(
						sdkCtx,
						prc20AddressHex,
						ueaAddr,
						amount,
					)
					if execErr != nil {
						shouldRevert = true
						revertReason = execErr.Error()
						sdkCtx.Logger().Error("ExecuteInboundGasAndPayload: CallPRC20DepositAutoSwap failed", "error", execErr.Error())
					} else {
						sdkCtx.Logger().Info(
							"ExecuteInboundGasAndPayload: CallPRC20DepositAutoSwap succeeded",
							"tx_hash", receipt.Hash,
							"gas_used", receipt.GasUsed,
						)
					}
				}
			}
		}
	}

	// --- record deposit attempt
	depositPcTx := types.PCTx{
		Sender:      ueModuleAddressStr,
		BlockHeight: uint64(sdkCtx.BlockHeight()),
		Status:      "FAILED",
	}
	if execErr != nil {
		depositPcTx.ErrorMsg = execErr.Error()
		sdkCtx.Logger().Error("ExecuteInboundGasAndPayload: deposit phase failed", "error", depositPcTx.ErrorMsg)
	} else {
		depositPcTx.TxHash = receipt.Hash
		depositPcTx.GasUsed = receipt.GasUsed
		depositPcTx.Status = "SUCCESS"
		sdkCtx.Logger().Info(
			"ExecuteInboundGasAndPayload: deposit phase success",
			"tx_hash", depositPcTx.TxHash,
			"gas_used", depositPcTx.GasUsed,
		)
	}

	sdkCtx.Logger().Info("ExecuteInboundGasAndPayload: updating universal tx with deposit pcTx")
	updateErr := k.UpdateUniversalTx(ctx, universalTxKey, func(utx *types.UniversalTx) error {
		utx.PcTx = append(utx.PcTx, &depositPcTx)
		if execErr != nil {
			utx.UniversalStatus = types.UniversalTxStatus_PC_EXECUTED_FAILED
		}
		return nil
	})
	if updateErr != nil {
		sdkCtx.Logger().Error("ExecuteInboundGasAndPayload: failed updating universal tx after deposit", "error", updateErr.Error())
		return updateErr
	}
	sdkCtx.Logger().Info("ExecuteInboundGasAndPayload: universal tx updated after deposit")

	// --- create revert ONLY for pre-deposit / deposit failures
	if execErr != nil && shouldRevert {
		sdkCtx.Logger().Info("ExecuteInboundGasAndPayload: creating revert outbound", "reason", revertReason)
		revertOutbound := &types.OutboundTx{
			DestinationChain: utx.InboundTx.SourceChain,
			Recipient: func() string {
				if utx.InboundTx.RevertInstructions != nil {
					return utx.InboundTx.RevertInstructions.FundRecipient
				}
				return utx.InboundTx.Sender
			}(),
			Amount:            utx.InboundTx.Amount,
			ExternalAssetAddr: utx.InboundTx.AssetAddr,
			Sender:            utx.InboundTx.Sender,
			TxType:            types.TxType_INBOUND_REVERT,
			OutboundStatus:    types.Status_PENDING,
			Id:                types.GetOutboundRevertId(utx.InboundTx.TxHash),
		}

		sdkCtx.Logger().Info(
			"ExecuteInboundGasAndPayload: attaching revert outbound",
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
		sdkCtx.Logger().Info("ExecuteInboundGasAndPayload: revert outbound attach attempted")

		return nil
	}

	// --- funds deposited successfully → continue with payload

	ueModuleAddr, _ := k.GetUeModuleAddress(ctx)
	sdkCtx.Logger().Info("ExecuteInboundGasAndPayload: entering payload phase", "ue_module_addr", ueModuleAddr.Hex())

	// --- Step 5: payload hash
	sdkCtx.Logger().Info("ExecuteInboundGasAndPayload: storing verified payload hash")
	payloadHashErr := k.StoreVerifiedPayloadHash(sdkCtx, utx, ueaAddr, ueModuleAddr)
	if payloadHashErr != nil {
		sdkCtx.Logger().Error("ExecuteInboundGasAndPayload: StoreVerifiedPayloadHash failed", "error", payloadHashErr.Error())
		errorPcTx := types.PCTx{
			Sender:      ueModuleAddressStr,
			BlockHeight: uint64(sdkCtx.BlockHeight()),
			Status:      "FAILED",
			ErrorMsg:    fmt.Sprintf("payload hash failed: %v", payloadHashErr),
		}
		_ = k.UpdateUniversalTx(ctx, universalTxKey, func(utx *types.UniversalTx) error {
			utx.PcTx = append(utx.PcTx, &errorPcTx)
			utx.UniversalStatus = types.UniversalTxStatus_PC_EXECUTED_FAILED
			return nil
		})
		sdkCtx.Logger().Info("ExecuteInboundGasAndPayload: payload-hash failure pcTx append attempted")
		return nil
	}
	sdkCtx.Logger().Info("ExecuteInboundGasAndPayload: payload hash stored")

	// --- Step 6: execute payload
	sdkCtx.Logger().Info("ExecuteInboundGasAndPayload: executing payload")
	receipt, err = k.ExecutePayloadV2(
		ctx,
		ueModuleAddr,
		&universalAccountId,
		utx.InboundTx.UniversalPayload,
		utx.InboundTx.VerificationData,
	)

	payloadPcTx := types.PCTx{
		Sender:      ueModuleAddressStr,
		BlockHeight: uint64(sdkCtx.BlockHeight()),
		Status:      "FAILED",
	}
	if err != nil {
		payloadPcTx.ErrorMsg = err.Error()
		sdkCtx.Logger().Error("ExecuteInboundGasAndPayload: ExecutePayloadV2 failed", "error", err.Error())
	} else {
		payloadPcTx.TxHash = receipt.Hash
		payloadPcTx.GasUsed = receipt.GasUsed
		payloadPcTx.Status = "SUCCESS"
		sdkCtx.Logger().Info(
			"ExecuteInboundGasAndPayload: payload execution succeeded",
			"tx_hash", payloadPcTx.TxHash,
			"gas_used", payloadPcTx.GasUsed,
		)

		if receipt != nil {
			sdkCtx.Logger().Info("ExecuteInboundGasAndPayload: attaching outbound(s) from payload receipt")
			_ = k.AttachOutboundsToExistingUniversalTx(sdkCtx, receipt, utx)
			sdkCtx.Logger().Info("ExecuteInboundGasAndPayload: outbound attach attempted")
		}
	}

	sdkCtx.Logger().Info("ExecuteInboundGasAndPayload: updating universal tx with payload pcTx")
	updateErr = k.UpdateUniversalTx(ctx, universalTxKey, func(utx *types.UniversalTx) error {
		utx.PcTx = append(utx.PcTx, &payloadPcTx)
		if err != nil {
			utx.UniversalStatus = types.UniversalTxStatus_PC_EXECUTED_FAILED
		} else {
			utx.UniversalStatus = types.UniversalTxStatus_PC_EXECUTED_SUCCESS
		}
		return nil
	})
	if updateErr != nil {
		sdkCtx.Logger().Error("ExecuteInboundGasAndPayload: failed updating universal tx after payload", "error", updateErr.Error())
		return updateErr
	}
	sdkCtx.Logger().Info("ExecuteInboundGasAndPayload: done", "payload_error", err != nil)

	return nil
}
