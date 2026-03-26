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

func (k Keeper) ExecuteInboundFundsAndPayload(ctx context.Context, utx types.UniversalTx) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	_, ueModuleAddressStr := k.GetUeModuleAddress(ctx)
	universalTxKey := types.GetInboundUniversalTxKey(*utx.InboundTx)

	shouldRevert := false
	var revertReason string

	// Build universalAccountId
	chainNamespace, chainId, err := types.ParseCAIP2(utx.InboundTx.SourceChain)
	if err != nil {
		return fmt.Errorf("invalid SourceChain: %w", err)
	}
	universalAccountId := types.UniversalAccountId{
		ChainNamespace: chainNamespace,
		ChainId:        chainId,
		Owner:          utx.InboundTx.Sender,
	}

	factoryAddress := common.HexToAddress(types.FACTORY_PROXY_ADDRESS_HEX)
	ueModuleAccAddress, _ := k.GetUeModuleAddress(ctx)

	var execErr error
	var receipt *evmtypes.MsgEthereumTxResponse
	var ueaAddr common.Address
	var isSmartContract bool
	payloadSender := utx.InboundTx.Sender      // default: inbound sender; overridden to UEA owner for CEA
	payloadChain := utx.InboundTx.SourceChain // default: inbound source chain; overridden to UEA origin chain for CEA

	// Parse amount to check for zero-amount path
	inboundAmount := new(big.Int)
	inboundAmount.SetString(utx.InboundTx.Amount, 10)

	if utx.InboundTx.IsCEA {
		// isCEA path: recipient is explicitly specified.
		// Three-way check:
		//   1. Recipient is a UEA  → existing flow (deposit + ExecutePayloadV2)
		//   2. Recipient is a deployed smart contract (not UEA) → deposit + executeUniversalTx
		//   3. Neither → record FAILED PCTx, no INBOUND_REVERT
		if !strings.HasPrefix(strings.ToLower(utx.InboundTx.Recipient), "0x") {
			execErr = fmt.Errorf("recipient must be a valid hex address when isCEA is true")
		} else {
			ueaAddr = common.HexToAddress(utx.InboundTx.Recipient)

			origin, isUEA, ueaCheckErr := k.CallFactoryGetOriginForUEA(sdkCtx, ueModuleAccAddress, factoryAddress, ueaAddr)
			if ueaCheckErr != nil {
				execErr = fmt.Errorf("failed to verify UEA: %w", ueaCheckErr)
			} else if isUEA {
				// Use UEA owner and origin chain for payload hash verification
				payloadSender = origin.Owner
				payloadChain = fmt.Sprintf("%s:%s", origin.ChainNamespace, origin.ChainId)
				// UEA path: deposit PRC20 into the UEA (if amount > 0), then execute payload via UEA
				if inboundAmount.Sign() > 0 {
					receipt, execErr = k.depositPRC20(
						sdkCtx,
						utx.InboundTx.SourceChain,
						utx.InboundTx.AssetAddr,
						ueaAddr,
						utx.InboundTx.Amount,
					)
					if execErr != nil {
						execErr = fmt.Errorf("depositPRC20 failed: %w", execErr)
					}
				}
			} else {
				// Non-UEA path (smart contract or EOA): deposit PRC20 and call executeUniversalTx
				isSmartContract = true
				if inboundAmount.Sign() > 0 {
					receipt, execErr = k.depositPRC20(
						sdkCtx,
						utx.InboundTx.SourceChain,
						utx.InboundTx.AssetAddr,
						ueaAddr,
						utx.InboundTx.Amount,
					)
					if execErr != nil {
						execErr = fmt.Errorf("depositPRC20 failed: %w", execErr)
					}
				}
			}
		}
		// isCEA failures never create an INBOUND_REVERT outbound.
	} else {
		// Original logic: check factory for UEA, deploy if not deployed
		ueaAddrRes, isDeployed, err := k.CallFactoryToGetUEAAddressForOrigin(sdkCtx, ueModuleAccAddress, factoryAddress, &universalAccountId)
		if err != nil {
			execErr = fmt.Errorf("factory lookup failed: %w", err)
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
					if updateErr := k.UpdateUniversalTx(ctx, universalTxKey, func(utx *types.UniversalTx) error {
						utx.PcTx = append(utx.PcTx, &deployPcTx)
						return nil
					}); updateErr != nil {
						k.logger.Error("failed to record deployment PCTx in UniversalTx", "key", universalTxKey, "err", updateErr)
					}
				}
			}

			if execErr == nil && inboundAmount.Sign() > 0 {
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

	// --- record deposit attempt (only if amount > 0 or there was an error)
	if inboundAmount.Sign() > 0 || execErr != nil {
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
			return nil
		})
		if updateErr != nil {
			return updateErr
		}
	}

	// If deposit failed, stop here.
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
				Id:                types.GetOutboundRevertId(utx.InboundTx.SourceChain, utx.InboundTx.TxHash),
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

	// Smart contract path: call executeUniversalTx and return
	if isSmartContract {
		tokenConfig, tcErr := k.uregistryKeeper.GetTokenConfig(sdkCtx, utx.InboundTx.SourceChain, utx.InboundTx.AssetAddr)

		var contractReceipt *evmtypes.MsgEthereumTxResponse
		var contractErr error

		if tcErr != nil {
			contractErr = fmt.Errorf("token config lookup failed: %w", tcErr)
		} else {
			prc20Addr := common.HexToAddress(tokenConfig.NativeRepresentation.ContractAddress)

			amount := new(big.Int)
			amount, ok := amount.SetString(utx.InboundTx.Amount, 10)
			if !ok {
				contractErr = fmt.Errorf("invalid amount: %s", utx.InboundTx.Amount)
			} else {
				txId := common.HexToHash(utx.Id)

				var payload []byte
				if utx.InboundTx.UniversalPayload != nil && utx.InboundTx.UniversalPayload.Data != "" {
					payload = common.FromHex(utx.InboundTx.UniversalPayload.Data)
				}

				contractReceipt, contractErr = k.CallExecuteUniversalTx(
					sdkCtx,
					ueaAddr,
					utx.InboundTx.SourceChain,
					[]byte(utx.InboundTx.Sender),
					payload,
					amount,
					prc20Addr,
					txId,
				)
			}
		}

		callPcTx := types.PCTx{
			Sender:      ueModuleAddressStr,
			BlockHeight: uint64(sdkCtx.BlockHeight()),
			Status:      "FAILED",
		}
		if contractErr != nil {
			callPcTx.ErrorMsg = contractErr.Error()
		} else {
			callPcTx.TxHash = contractReceipt.Hash
			callPcTx.GasUsed = contractReceipt.GasUsed
			callPcTx.Status = "SUCCESS"
		}
		if updateErr := k.UpdateUniversalTx(ctx, universalTxKey, func(utx *types.UniversalTx) error {
			utx.PcTx = append(utx.PcTx, &callPcTx)
			return nil
		}); updateErr != nil {
			k.logger.Error("failed to record PCTx in UniversalTx", "key", universalTxKey, "err", updateErr)
		}
		return nil
	}

	ueModuleAddr, _ := k.GetUeModuleAddress(ctx)

	// --- Step 3: compute and store payload hash
	payloadHashErr := k.StoreVerifiedPayloadHash(sdkCtx, utx, ueaAddr, ueModuleAddr, payloadSender, payloadChain)
	if payloadHashErr != nil {
		errorPcTx := types.PCTx{
			Sender:      ueModuleAddressStr,
			BlockHeight: uint64(sdkCtx.BlockHeight()),
			Status:      "FAILED",
			ErrorMsg:    fmt.Sprintf("payload hash failed: %v", payloadHashErr),
		}
		if updateErr := k.UpdateUniversalTx(ctx, universalTxKey, func(utx *types.UniversalTx) error {
			utx.PcTx = append(utx.PcTx, &errorPcTx)
			return nil
		}); updateErr != nil {
			k.logger.Error("failed to record PCTx in UniversalTx", "key", universalTxKey, "err", updateErr)
		}
		return nil
	}

	// --- Step 4: execute payload via UEA
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
			if attachErr := k.AttachOutboundsToExistingUniversalTx(sdkCtx, receipt, utx); attachErr != nil {
				k.logger.Error("failed to attach outbounds to UniversalTx", "id", utx.Id, "err", attachErr)
			}
		}
	}

	updateErr2 := k.UpdateUniversalTx(ctx, universalTxKey, func(utx *types.UniversalTx) error {
		utx.PcTx = append(utx.PcTx, &payloadPcTx)
		return nil
	})
	if updateErr2 != nil {
		return updateErr2
	}

	return nil
}
