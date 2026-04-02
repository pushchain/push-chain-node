package keeper

import (
	"context"
	"fmt"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

func (k Keeper) ExecuteInboundGas(ctx context.Context, inbound types.Inbound) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	ueModuleAccAddress, ueModuleAddressStr := k.GetUeModuleAddress(ctx)
	universalTxKey := types.GetInboundUniversalTxKey(inbound)

	k.Logger().Info("execute inbound gas: gas abstraction swap",
		"utx_key", universalTxKey,
		"source_chain", inbound.SourceChain,
		"amount", inbound.Amount,
		"sender", inbound.Sender,
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
	tokenConfig, err := k.uregistryKeeper.GetTokenConfig(ctx, inbound.SourceChain, inbound.AssetAddr)
	if err != nil {
		execErr = fmt.Errorf("GetTokenConfig failed: %w", err)
		shouldRevert = true
		revertReason = execErr.Error()
	} else {
		// --- step 2: parse amount
		amount := new(big.Int)
		if amount, ok := amount.SetString(inbound.Amount, 10); !ok {
			execErr = fmt.Errorf("invalid amount: %s", inbound.Amount)
			shouldRevert = true
			revertReason = execErr.Error()
		} else {
			// --- step 3: resolve / deploy UEA
			prc20AddressHex := common.HexToAddress(tokenConfig.NativeRepresentation.ContractAddress)
			chainNamespace, chainId, caipErr := types.ParseCAIP2(inbound.SourceChain)
			if caipErr != nil {
				execErr = fmt.Errorf("invalid SourceChain: %w", caipErr)
				shouldRevert = true
				revertReason = execErr.Error()
			} else {
				universalAccountId := types.UniversalAccountId{
					ChainNamespace: chainNamespace,
					ChainId:        chainId,
					Owner:          inbound.Sender,
				}
				factoryAddress := common.HexToAddress(types.FACTORY_PROXY_ADDRESS_HEX)

				ueaAddr, isDeployed, fErr := k.CallFactoryToGetUEAAddressForOrigin(sdkCtx, ueModuleAccAddress, factoryAddress, &universalAccountId)
				if fErr != nil {
					execErr = fmt.Errorf("CallFactory failed: %w", fErr)
					shouldRevert = true
					revertReason = execErr.Error()
				} else {
					if !isDeployed {
						// Deploy new UEA and record a pcTx for it
						deployReceipt, dErr := k.DeployUEAV2(ctx, ueModuleAccAddress, &universalAccountId)
						if dErr != nil {
							execErr = fmt.Errorf("DeployUEA failed: %w", dErr)
							shouldRevert = true
							revertReason = execErr.Error()
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
							if updateErr := k.UpdateUniversalTx(ctx, universalTxKey, func(utx *types.UniversalTx) error {
							utx.PcTx = append(utx.PcTx, &deployPcTx)
							return nil
						}); updateErr != nil {
							return updateErr
						}
						}
					}

					if execErr == nil {
						// --- step 4: fetch swap quote and compute minPCOut with 5% slippage
						var (
							quoterAddr common.Address
							wpcAddr    common.Address
							fee        *big.Int
							quote      *big.Int
						)

						quoterAddr, execErr = k.GetUniversalCoreQuoterAddress(sdkCtx)
						if execErr != nil {
							shouldRevert = true
							revertReason = execErr.Error()
						}

						if execErr == nil {
							wpcAddr, execErr = k.GetUniversalCoreWPCAddress(sdkCtx)
							if execErr != nil {
								shouldRevert = true
								revertReason = execErr.Error()
							}
						}

						if execErr == nil {
							fee, execErr = k.GetDefaultFeeTierForToken(sdkCtx, prc20AddressHex)
							if execErr != nil {
								shouldRevert = true
								revertReason = execErr.Error()
							}
						}

						if execErr == nil {
							quote, execErr = k.GetSwapQuote(sdkCtx, quoterAddr, prc20AddressHex, wpcAddr, fee, amount)
							if execErr != nil {
								shouldRevert = true
								revertReason = execErr.Error()
							}
						}

						if execErr == nil {
							// 5% slippage: minPCOut = quote * 95 / 100
							minPCOut := new(big.Int).Mul(quote, big.NewInt(95))
							minPCOut.Div(minPCOut, big.NewInt(100))

							// --- step 5: deposit + swap
							receipt, execErr = k.CallPRC20DepositAutoSwap(sdkCtx, prc20AddressHex, ueaAddr, amount, fee, minPCOut)
							if execErr != nil {
								shouldRevert = true
								revertReason = execErr.Error()
							}
						}
					}
				}
			}
		}
	}

	// --- Finalize pcTx
	// Capture tx hash from receipt even on EVM revert for debugging.
	if receipt != nil {
		pcTx.TxHash = receipt.Hash
		pcTx.GasUsed = receipt.GasUsed
	}
	if execErr != nil {
		k.Logger().Warn("execute inbound gas: swap failed",
			"utx_key", universalTxKey,
			"error", execErr.Error(),
			"should_revert", shouldRevert,
		)
		pcTx.ErrorMsg = execErr.Error()
	} else {
		k.Logger().Info("execute inbound gas: swap succeeded",
			"utx_key", universalTxKey,
			"tx_hash", receipt.Hash,
			"gas_used", receipt.GasUsed,
		)
		pcTx.Status = "SUCCESS"
	}

	// --- Update UniversalTx always
	updateErr := k.UpdateUniversalTx(ctx, universalTxKey, func(utx *types.UniversalTx) error {
		utx.PcTx = append(utx.PcTx, &pcTx)
		return nil
	})
	if updateErr != nil {
		// if state update fails, revert the tx
		return updateErr
	}

	if execErr != nil && shouldRevert {
		revertOutbound := k.buildRevertOutbound(sdkCtx, &inbound)

		if attachErr := k.attachOutboundsToUtx(
			sdkCtx,
			universalTxKey,
			[]*types.OutboundTx{revertOutbound},
			revertReason,
		); attachErr != nil {
			if storeErr := k.UpdateUniversalTx(sdkCtx, universalTxKey, func(u *types.UniversalTx) error {
				u.RevertError = attachErr.Error()
				return nil
			}); storeErr != nil {
				return storeErr
			}
		}
	}

	// Never return execErr, only nil
	return nil
}
