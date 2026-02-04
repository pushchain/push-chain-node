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

	universalAccountId := types.UniversalAccountId{
		ChainNamespace: strings.Split(utx.InboundTx.SourceChain, ":")[0],
		ChainId:        strings.Split(utx.InboundTx.SourceChain, ":")[1],
		Owner:          utx.InboundTx.Sender,
	}

	factoryAddress := common.HexToAddress(types.FACTORY_PROXY_ADDRESS_HEX)

	var execErr error
	var receipt *evmtypes.MsgEthereumTxResponse
	var ueaAddr common.Address

	shouldRevert := false
	var revertReason string

	// --- Step 1: token config
	tokenConfig, err := k.uregistryKeeper.GetTokenConfig(ctx, utx.InboundTx.SourceChain, utx.InboundTx.AssetAddr)
	if err != nil {
		execErr = fmt.Errorf("GetTokenConfig failed: %w", err)
		shouldRevert = true
		revertReason = execErr.Error()
	} else {
		// --- Step 2: parse amount
		amount := new(big.Int)
		if amount, ok := amount.SetString(utx.InboundTx.Amount, 10); !ok {
			execErr = fmt.Errorf("invalid amount: %s", utx.InboundTx.Amount)
			shouldRevert = true
			revertReason = execErr.Error()
		} else {
			// --- Step 3: resolve / deploy UEA
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
			} else {
				ueaAddr = ueaAddrRes

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
					// --- Step 4: deposit + autoswap
					prc20AddressHex := common.HexToAddress(
						tokenConfig.NativeRepresentation.ContractAddress,
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
	} else {
		depositPcTx.TxHash = receipt.Hash
		depositPcTx.GasUsed = receipt.GasUsed
		depositPcTx.Status = "SUCCESS"
	}

	updateErr := k.UpdateUniversalTx(ctx, universalTxKey, func(utx *types.UniversalTx) error {
		utx.PcTx = append(utx.PcTx, &depositPcTx)
		if execErr != nil {
			utx.UniversalStatus = types.UniversalTxStatus_PC_EXECUTED_FAILED
		}
		return nil
	})
	if updateErr != nil {
		return updateErr
	}

	// --- create revert ONLY for pre-deposit / deposit failures
	if execErr != nil && shouldRevert {
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

		return nil
	}

	// --- funds deposited successfully → continue with payload

	ueModuleAddr, _ := k.GetUeModuleAddress(ctx)

	// --- Step 5: payload hash
	payloadHashErr := k.StoreVerifiedPayloadHash(sdkCtx, utx, ueaAddr, ueModuleAddr)
	if payloadHashErr != nil {
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
		return nil
	}

	// --- Step 6: execute payload
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
		if err != nil {
			utx.UniversalStatus = types.UniversalTxStatus_PC_EXECUTED_FAILED
		} else {
			utx.UniversalStatus = types.UniversalTxStatus_PC_EXECUTED_SUCCESS
		}
		return nil
	})
	if updateErr != nil {
		return updateErr
	}

	return nil
}
