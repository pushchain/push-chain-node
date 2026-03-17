package keeper

import (
	"context"
	"fmt"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

func (k Keeper) ExecuteInboundFundsAndPayload(ctx context.Context, utx types.UniversalTx) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	_, ueModuleAddressStr := k.GetUeModuleAddress(ctx)
	universalTxKey := types.GetInboundUniversalTxKey(*utx.InboundTx)

	shouldRevert := false
	var revertReason string

	// Build universalAccountId
	universalAccountId := types.UniversalAccountId{
		ChainNamespace: strings.Split(utx.InboundTx.SourceChain, ":")[0],
		ChainId:        strings.Split(utx.InboundTx.SourceChain, ":")[1],
		Owner:          utx.InboundTx.Sender,
	}

	factoryAddress := common.HexToAddress(types.FACTORY_PROXY_ADDRESS_HEX)
	ueModuleAccAddress, _ := k.GetUeModuleAddress(ctx)

	var execErr error
	var receipt *evmtypes.MsgEthereumTxResponse
	var ueaAddr common.Address

	// When isCEA is true, validate recipient is a UEA before attempting deposit
	if utx.InboundTx.IsCEA {
		// Validate and use recipient as the UEA address
		if !strings.HasPrefix(strings.ToLower(utx.InboundTx.Recipient), "0x") {
			execErr = fmt.Errorf("recipient must be a valid hex address when isCEA is true")
			shouldRevert = true
			revertReason = execErr.Error()
		} else {
			ueaAddr = common.HexToAddress(utx.InboundTx.Recipient)

			// --- Step 1: Validate that recipient is a UEA
			_, isUEA, ueaCheckErr := k.CallFactoryGetOriginForUEA(sdkCtx, ueModuleAccAddress, factoryAddress, ueaAddr)
			if ueaCheckErr != nil {
				execErr = fmt.Errorf("failed to verify UEA: %w", ueaCheckErr)
				shouldRevert = true
				revertReason = execErr.Error()
			} else if !isUEA {
				execErr = fmt.Errorf("recipient is not a valid UEA")
				shouldRevert = true
				revertReason = execErr.Error()
			} else {
				// --- Step 2: Recipient is valid UEA, deposit PRC20 into it
				receipt, execErr = k.depositPRC20(
					sdkCtx,
					utx.InboundTx.SourceChain,
					utx.InboundTx.AssetAddr,
					ueaAddr,
					utx.InboundTx.Amount,
				)
				if execErr != nil {
					execErr = fmt.Errorf("depositPRC20 failed: %w", execErr)
					shouldRevert = true
					revertReason = execErr.Error()
				}
			}
		}
	} else {
		// Original logic: check factory for UEA, deploy if not deployed
		// --- Step 1: check factory for UEA
		ueaAddrRes, isDeployed, err := k.CallFactoryToGetUEAAddressForOrigin(sdkCtx, ueModuleAccAddress, factoryAddress, &universalAccountId)
		if err != nil {
			execErr = fmt.Errorf("factory lookup failed: %w", err)
			shouldRevert = true
			revertReason = execErr.Error()
		} else {
			ueaAddr = ueaAddrRes

			// deploy UEA if not yet deployed
			if !isDeployed {
				deployReceipt, dErr := k.DeployUEAV2(ctx, ueModuleAccAddress, &universalAccountId)
				if dErr != nil {
					execErr = fmt.Errorf("DeployUEAV2 failed: %w", dErr)
					shouldRevert = true
					revertReason = execErr.Error()
				} else {
					ueaAddr = common.BytesToAddress(deployReceipt.Ret)

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
				// --- Step 2: deposit PRC20 into UEA
				receipt, err = k.depositPRC20(
					sdkCtx,
					utx.InboundTx.SourceChain,
					utx.InboundTx.AssetAddr,
					ueaAddr,
					utx.InboundTx.Amount,
				)
				if err != nil {
					execErr = fmt.Errorf("depositPRC20 failed: %w", err)
					shouldRevert = true
					revertReason = execErr.Error()
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
	} else {
		depositPcTx.TxHash = receipt.Hash
		depositPcTx.GasUsed = receipt.GasUsed
		depositPcTx.Status = "SUCCESS"
	}
	updateErr := k.UpdateUniversalTx(ctx, universalTxKey, func(utx *types.UniversalTx) error {
		utx.PcTx = append(utx.PcTx, &depositPcTx)
		if execErr != nil {
		
		}
		return nil
	})
	if updateErr != nil {
		return updateErr
	}

	// If deposit failed, stop here (don’t attempt payload execution)
	if execErr != nil {
		if shouldRevert {
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

			_ = k.attachOutboundsToUtx(
				sdkCtx,
				universalTxKey,
				[]*types.OutboundTx{revertOutbound},
				revertReason,
			)
		}

		return nil
	}

	ueModuleAddr, _ := k.GetUeModuleAddress(ctx)

	// --- Step 3: compute and store payload hash
	payloadHashErr := k.StoreVerifiedPayloadHash(sdkCtx, utx, ueaAddr, ueModuleAddr)
	if payloadHashErr != nil {
		// Update UniversalTx with payload hash error and stop
		errorPcTx := types.PCTx{
			Sender:      ueModuleAddressStr,
			BlockHeight: uint64(sdkCtx.BlockHeight()),
			Status:      "FAILED",
			ErrorMsg:    fmt.Sprintf("payload hash failed: %v", payloadHashErr),
		}
		_ = k.UpdateUniversalTx(ctx, universalTxKey, func(utx *types.UniversalTx) error {
			utx.PcTx = append(utx.PcTx, &errorPcTx)
		
			return nil
		})
		return nil
	}

	// --- Step 4: execute payload
	// ueaAddr is already resolved and validated above (either from recipient for CEA, or from factory)
	var payloadErr error
	receipt, payloadErr = k.ExecutePayloadV2(ctx, ueModuleAddr, ueaAddr, utx.InboundTx.UniversalPayload, utx.InboundTx.VerificationData)

	payloadPcTx := types.PCTx{
		Sender:      ueModuleAddressStr,
		BlockHeight: uint64(sdkCtx.BlockHeight()),
		Status:      "FAILED",
	}
	if payloadErr != nil {
		payloadPcTx.ErrorMsg = payloadErr.Error()
	} else {
		payloadPcTx.TxHash = receipt.Hash
		payloadPcTx.GasUsed = receipt.GasUsed
		payloadPcTx.Status = "SUCCESS"

		if receipt != nil {
			_ = k.AttachOutboundsToExistingUniversalTx(sdkCtx, receipt, utx)
		}
	}

	updateErr = k.UpdateUniversalTx(ctx, universalTxKey, func(utx *types.UniversalTx) error {
		utx.PcTx = append(utx.PcTx, &payloadPcTx)
		if payloadErr != nil {
		
		} else {
		
		}
		return nil
	})
	if updateErr != nil {
		return updateErr
	}

	return nil
}
