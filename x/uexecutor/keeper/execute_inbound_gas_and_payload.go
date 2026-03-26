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

	var execErr error
	var receipt *evmtypes.MsgEthereumTxResponse
	var ueaAddr common.Address
	var isSmartContract bool
	payloadSender := utx.InboundTx.Sender      // default: inbound sender; overridden to UEA owner for CEA
	payloadChain := utx.InboundTx.SourceChain // default: inbound source chain; overridden to UEA origin chain for CEA

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
			if utx.InboundTx.IsCEA {
				// isCEA path: recipient is explicitly specified.
				// Three-way check:
				//   1. Recipient is a UEA  → deposit + autoswap + ExecutePayloadV2
				//   2. Recipient is a deployed smart contract (not UEA) → deposit + autoswap + executeUniversalTx
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
						// UEA path: deposit + autoswap into the UEA (if amount > 0), then execute payload via UEA
						if amount.Sign() > 0 {
							prc20AddrHex := common.HexToAddress(tokenConfig.NativeRepresentation.ContractAddress)
							receipt, execErr = k.gasAndPayloadDepositAutoSwap(sdkCtx, prc20AddrHex, ueaAddr, amount)
							if execErr != nil {
								execErr = fmt.Errorf("depositAutoSwap failed: %w", execErr)
							}
						}
					} else {
						// Non-UEA path (smart contract or EOA): deposit + autoswap and call executeUniversalTx
						isSmartContract = true
						if amount.Sign() > 0 {
							prc20AddrHex := common.HexToAddress(tokenConfig.NativeRepresentation.ContractAddress)
							receipt, execErr = k.gasAndPayloadDepositAutoSwap(sdkCtx, prc20AddrHex, ueaAddr, amount)
							if execErr != nil {
								execErr = fmt.Errorf("depositAutoSwap failed: %w", execErr)
							}
						}
					}
				}
				// isCEA failures never create an INBOUND_REVERT outbound.
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
							if updateErr := k.UpdateUniversalTx(ctx, universalTxKey, func(utx *types.UniversalTx) error {
								utx.PcTx = append(utx.PcTx, &deployPcTx)
								return nil
							}); updateErr != nil {
								k.logger.Error("failed to record deployment PCTx in UniversalTx", "key", universalTxKey, "err", updateErr)
							}
						}
					}

					if execErr == nil && amount.Sign() > 0 {
						// --- Step 4 & 5: deposit + autoswap (only when amount > 0)
						prc20AddrHex := common.HexToAddress(tokenConfig.NativeRepresentation.ContractAddress)
						receipt, execErr = k.gasAndPayloadDepositAutoSwap(sdkCtx, prc20AddrHex, ueaAddr, amount)
						if execErr != nil {
							shouldRevert = true
							revertReason = execErr.Error()
						}
					}
				}
			}
		}
	}

	// --- record deposit attempt (only if amount > 0 or there was an error)
	depositAmount := new(big.Int)
	depositAmount.SetString(utx.InboundTx.Amount, 10)
	if depositAmount.Sign() > 0 || execErr != nil {
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
	}

	// --- create revert ONLY for pre-deposit / deposit failures (non-isCEA path)
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
			Id:                types.GetOutboundRevertId(utx.InboundTx.SourceChain, utx.InboundTx.TxHash),
		}

		_ = k.attachOutboundsToUtx(
			sdkCtx,
			universalTxKey,
			[]*types.OutboundTx{revertOutbound},
			revertReason,
		)

		return nil
	}

	// isCEA failures: record FAILED PCTx but no revert
	if execErr != nil && utx.InboundTx.IsCEA {
		return nil
	}

	// Smart contract path (isCEA): call executeUniversalTx and return
	if isSmartContract {
		prc20Addr := common.HexToAddress(tokenConfig.NativeRepresentation.ContractAddress)

		scAmount := new(big.Int)
		scAmount, ok := scAmount.SetString(utx.InboundTx.Amount, 10)
		if !ok {
			return fmt.Errorf("invalid amount: %s", utx.InboundTx.Amount)
		}

		txId := common.HexToHash(utx.Id)

		var payload []byte
		if utx.InboundTx.UniversalPayload != nil && utx.InboundTx.UniversalPayload.Data != "" {
			payload = common.FromHex(utx.InboundTx.UniversalPayload.Data)
		}

		contractReceipt, contractErr := k.CallExecuteUniversalTx(
			sdkCtx,
			ueaAddr,
			utx.InboundTx.SourceChain,
			[]byte(utx.InboundTx.Sender),
			payload,
			scAmount,
			prc20Addr,
			txId,
		)

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

	// --- deposit successful (or skipped for zero amount) → continue with payload

	ueModuleAddr, _ := k.GetUeModuleAddress(ctx)

	// --- Step 6: payload hash
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

	// --- Step 7: execute payload
	receipt, err = k.ExecutePayloadV2(
		ctx,
		ueModuleAddr,
		ueaAddr,
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
			if attachErr := k.AttachOutboundsToExistingUniversalTx(sdkCtx, receipt, utx); attachErr != nil {
				k.logger.Error("failed to attach outbounds to UniversalTx", "id", utx.Id, "err", attachErr)
			}
		}
	}

	updateErr := k.UpdateUniversalTx(ctx, universalTxKey, func(utx *types.UniversalTx) error {
		utx.PcTx = append(utx.PcTx, &payloadPcTx)
		if err != nil {

		} else {

		}
		return nil
	})
	if updateErr != nil {
		return updateErr
	}

	return nil
}

// gasAndPayloadDepositAutoSwap handles the swap quote + deposit autoswap for GAS_AND_PAYLOAD.
func (k Keeper) gasAndPayloadDepositAutoSwap(
	sdkCtx sdk.Context,
	prc20AddressHex common.Address,
	ueaAddr common.Address,
	amount *big.Int,
) (*evmtypes.MsgEthereumTxResponse, error) {
	quoterAddr, err := k.GetUniversalCoreQuoterAddress(sdkCtx)
	if err != nil {
		return nil, err
	}

	wpcAddr, err := k.GetUniversalCoreWPCAddress(sdkCtx)
	if err != nil {
		return nil, err
	}

	fee, err := k.GetDefaultFeeTierForToken(sdkCtx, prc20AddressHex)
	if err != nil {
		return nil, err
	}

	quote, err := k.GetSwapQuote(sdkCtx, quoterAddr, prc20AddressHex, wpcAddr, fee, amount)
	if err != nil {
		return nil, err
	}

	// 5% slippage: minPCOut = quote * 95 / 100
	minPCOut := new(big.Int).Mul(quote, big.NewInt(95))
	minPCOut.Div(minPCOut, big.NewInt(100))

	return k.CallPRC20DepositAutoSwap(sdkCtx, prc20AddressHex, ueaAddr, amount, fee, minPCOut)
}
