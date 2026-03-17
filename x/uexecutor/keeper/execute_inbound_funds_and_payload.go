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
	var isSmartContract bool

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

			_, isUEA, ueaCheckErr := k.CallFactoryGetOriginForUEA(sdkCtx, ueModuleAccAddress, factoryAddress, ueaAddr)
			if ueaCheckErr != nil {
				execErr = fmt.Errorf("failed to verify UEA: %w", ueaCheckErr)
			} else if isUEA {
				// UEA path: deposit PRC20 into the UEA, then execute payload via UEA
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
			} else {
				// Non-UEA path (smart contract or EOA): deposit PRC20 and call executeUniversalTx
				isSmartContract = true
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
					_ = k.UpdateUniversalTx(ctx, universalTxKey, func(utx *types.UniversalTx) error {
						utx.PcTx = append(utx.PcTx, &deployPcTx)
						return nil
					})
				}
			}

			if execErr == nil {
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
		return nil
	})
	if updateErr != nil {
		return updateErr
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
		_ = k.UpdateUniversalTx(ctx, universalTxKey, func(utx *types.UniversalTx) error {
			utx.PcTx = append(utx.PcTx, &callPcTx)
			return nil
		})
		return nil
	}

	ueModuleAddr, _ := k.GetUeModuleAddress(ctx)

	// --- Step 3: compute and store payload hash
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
			return nil
		})
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
			_ = k.AttachOutboundsToExistingUniversalTx(sdkCtx, receipt, utx)
		}
	}

	updateErr = k.UpdateUniversalTx(ctx, universalTxKey, func(utx *types.UniversalTx) error {
		utx.PcTx = append(utx.PcTx, &payloadPcTx)
		return nil
	})
	if updateErr != nil {
		return updateErr
	}

	return nil
}
