package keeper_test

import (
	"errors"
	"math/big"
	"testing"

	evmtypes "github.com/evmos/os/x/evm/types"
	"github.com/golang/mock/gomock"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ethereum/go-ethereum/common"
	pchaintypes "github.com/rollchains/pchain/types"
	"github.com/rollchains/pchain/utils"
	uekeeper "github.com/rollchains/pchain/x/ue/keeper"
	"github.com/rollchains/pchain/x/ue/types"
)

func TestParams(t *testing.T) {
	f := SetupTest(t)
	require := require.New(t)

	testCases := []struct {
		name    string
		request *types.MsgUpdateParams
		err     bool
	}{
		{
			name: "fail; invalid authority",
			request: &types.MsgUpdateParams{
				Authority: f.addrs[0].String(),
				Params:    types.DefaultParams(),
			},
			err: true,
		},
		{
			name: "success",
			request: &types.MsgUpdateParams{
				Authority: f.govModAddr,
				Params:    types.DefaultParams(),
			},
			err: false,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, err := f.msgServer.UpdateParams(f.ctx, tc.request)

			if tc.err {
				require.Error(err)
			} else {
				require.NoError(err)

				r, err := f.queryServer.Params(f.ctx, &types.QueryParamsRequest{})
				require.NoError(err)

				require.EqualValues(&tc.request.Params, r.Params)
			}

		})
	}
}

func TestMsgServer_DeployUEA(t *testing.T) {
	f := SetupTest(t)
	require := require.New(t)

	validSigner := f.addrs[0]
	validUA := &types.UniversalAccount{
		Owner: "0x1234567890abcdef1234567890abcdef12345678",
		Chain: "ethereum",
	}
	validTxHash := "0xabc123"

	t.Run("fail; invalid signer address", func(t *testing.T) {
		msg := &types.MsgDeployUEA{
			Signer:           "invalid_address",
			UniversalAccount: validUA,
			TxHash:           validTxHash,
		}

		_, err := f.msgServer.DeployUEA(f.ctx, msg)
		require.ErrorContains(err, "failed to parse signer address")
	})

	t.Run("fail; gateway interaction tx not verified", func(t *testing.T) {
		// You can inject failure in f.app or f.k.utvKeeper if mockable
		msg := &types.MsgDeployUEA{
			Signer:           validSigner.String(),
			UniversalAccount: validUA,
			TxHash:           "invalid_tx",
		}
		f.mockUTVKeeper.
			EXPECT().VerifyGatewayInteractionTx(gomock.Any(), validUA.Owner, "invalid_tx", validUA.Chain).
			Return(errors.New("Gateway interaction failed"))

		_, err := f.msgServer.DeployUEA(f.ctx, msg)
		require.ErrorContains(err, "failed to verify gateway interaction transaction")
	})

	t.Run("fail: CallFactoryToDeployUEA Fails", func(t *testing.T) {
		msg := &types.MsgDeployUEA{
			Signer:           validSigner.String(),
			UniversalAccount: validUA,
			TxHash:           validTxHash,
		}
		addr := common.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")

		padded := common.LeftPadBytes(addr.Bytes(), 32)
		receipt := &evmtypes.MsgEthereumTxResponse{
			Ret: padded,
		}
		f.mockUTVKeeper.
			EXPECT().VerifyGatewayInteractionTx(gomock.Any(), validUA.Owner, validTxHash, validUA.Chain).
			Return(nil)

		f.mockEVMKeeper.EXPECT().
			CallEVM(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(receipt, errors.New("unable to deploy UEA"))

		_, err := f.msgServer.DeployUEA(f.ctx, msg)
		require.ErrorContains(err, "unable to deploy UEA")
	})

	t.Run("success; valid input returns UEA", func(t *testing.T) {
		msg := &types.MsgDeployUEA{
			Signer:           validSigner.String(),
			UniversalAccount: validUA,
			TxHash:           validTxHash,
		}

		addr := common.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")

		padded := common.LeftPadBytes(addr.Bytes(), 32)
		receipt := &evmtypes.MsgEthereumTxResponse{
			Ret: padded,
		}
		f.mockUTVKeeper.
			EXPECT().VerifyGatewayInteractionTx(gomock.Any(), validUA.Owner, validTxHash, validUA.Chain).
			Return(nil)

		f.mockEVMKeeper.EXPECT().
			CallEVM(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(receipt, nil)

		_, err := f.msgServer.DeployUEA(f.ctx, msg)
		require.NoError(err)
	})
}

func TestMsgServer_MintPC(t *testing.T) {
	f := SetupTest(t)

	validSigner := f.addrs[0]
	validUA := &types.UniversalAccount{
		Owner: "0x1234567890abcdef1234567890abcdef12345678",
		Chain: "ethereum",
	}
	validTxHash := "0xabc123"

	t.Run("fail: VerifyAndGetLockedFunds fails", func(t *testing.T) {
		msg := &types.MsgMintPC{
			Signer:           validSigner.String(),
			UniversalAccount: validUA,
			TxHash:           validTxHash,
		}

		f.mockUTVKeeper.EXPECT().
			VerifyAndGetLockedFunds(gomock.Any(), validUA.Owner, validTxHash, validUA.Chain).
			Return(*big.NewInt(0), uint32(0), errors.New("some error"))

		_, err := f.msgServer.MintPC(f.ctx, msg)
		require.ErrorContains(t, err, "failed to verify gateway interaction transaction")
	})

	t.Run("fail: CallFactoryToComputeUEAAddress returns error", func(t *testing.T) {
		usdAmount := new(big.Int)
		usdAmount.SetString("10000000000000000000", 10)
		decimals := uint32(18)

		addr := common.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")

		padded := common.LeftPadBytes(addr.Bytes(), 32) // Incorrect 40 bytes padding to initiate the error
		receipt := &evmtypes.MsgEthereumTxResponse{
			Ret: padded,
		}

		f.mockUTVKeeper.EXPECT().
			VerifyAndGetLockedFunds(gomock.Any(), validUA.Owner, validTxHash, validUA.Chain).
			Return(*usdAmount, decimals, nil)

		f.mockEVMKeeper.EXPECT().
			CallEVM(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(receipt, errors.New("call-factory fails"))

		msg := &types.MsgMintPC{
			Signer: validSigner.String(), UniversalAccount: validUA, TxHash: validTxHash,
		}
		_, err := f.msgServer.MintPC(f.ctx, msg)
		require.ErrorContains(t, err, "call-factory fails")
	})

	t.Run("bad-address", func(t *testing.T) {
		usdAmount := new(big.Int)
		usdAmount.SetString("10000000000000000000", 10)
		decimals := uint32(18)

		addr := common.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")

		padded := common.LeftPadBytes(addr.Bytes(), 40) // Incorrect 40 bytes padding to initiate the error
		receipt := &evmtypes.MsgEthereumTxResponse{
			Ret: padded,
		}

		f.mockUTVKeeper.EXPECT().
			VerifyAndGetLockedFunds(gomock.Any(), validUA.Owner, validTxHash, validUA.Chain).
			Return(*usdAmount, decimals, nil)

		f.mockEVMKeeper.EXPECT().
			CallEVM(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(receipt, nil)

		msg := &types.MsgMintPC{
			Signer: validSigner.String(), UniversalAccount: validUA, TxHash: validTxHash,
		}
		_, err := f.msgServer.MintPC(f.ctx, msg)
		require.ErrorContains(t, err, "failed to convert EVM address")
	})

	t.Run("fail: Mint Fails", func(t *testing.T) {
		msg := &types.MsgMintPC{
			Signer:           validSigner.String(),
			UniversalAccount: validUA,
			TxHash:           validTxHash,
		}

		addr := common.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")

		padded := common.LeftPadBytes(addr.Bytes(), 32)
		receipt := &evmtypes.MsgEthereumTxResponse{
			Ret: padded,
		}

		usdAmount := new(big.Int)
		usdAmount.SetString("1000000000000000000", 10) // 10 USD, 18 decimals
		decimals := uint32(18)
		amountToMint := uekeeper.ConvertUsdToPCTokens(usdAmount, decimals)
		expectedCoins := sdk.NewCoins(sdk.NewCoin(pchaintypes.BaseDenom, amountToMint))

		// Mock VerifyAndGetLockedFunds
		f.mockUTVKeeper.EXPECT().
			VerifyAndGetLockedFunds(gomock.Any(), validUA.Owner, validTxHash, validUA.Chain).
			Return(*big.NewInt(1_000_000), uint32(6), nil)

		f.mockEVMKeeper.EXPECT().
			CallEVM(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(receipt, nil)

		// MintCoins should be called with correct args
		f.mockBankKeeper.EXPECT().
			MintCoins(gomock.Any(), types.ModuleName, expectedCoins).
			Return(errors.New("minting failed"))

		_, err := f.msgServer.MintPC(f.ctx, msg)
		require.ErrorContains(t, err, "failed to mint coins")
	})

	t.Run("success", func(t *testing.T) {
		msg := &types.MsgMintPC{
			Signer:           validSigner.String(),
			UniversalAccount: validUA,
			TxHash:           validTxHash,
		}

		addr := common.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")

		padded := common.LeftPadBytes(addr.Bytes(), 32)
		receipt := &evmtypes.MsgEthereumTxResponse{
			Ret: padded,
		}

		usdAmount := new(big.Int)
		usdAmount.SetString("1000000000000000000", 10) // 10 USD, 18 decimals
		decimals := uint32(18)
		amountToMint := uekeeper.ConvertUsdToPCTokens(usdAmount, decimals)
		expectedCoins := sdk.NewCoins(sdk.NewCoin(pchaintypes.BaseDenom, amountToMint))

		// Mock VerifyAndGetLockedFunds
		f.mockUTVKeeper.EXPECT().
			VerifyAndGetLockedFunds(gomock.Any(), validUA.Owner, validTxHash, validUA.Chain).
			Return(*big.NewInt(1_000_000), uint32(6), nil)

		f.mockEVMKeeper.EXPECT().
			CallEVM(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(receipt, nil)

		// MintCoins should be called with correct args
		f.mockBankKeeper.EXPECT().
			MintCoins(gomock.Any(), types.ModuleName, expectedCoins).
			Return(nil)

		// Expected Cosmos address from UEA address (derived from validUA.Owner)
		expectedAddr, err := utils.ConvertAnyAddressToBytes(validUA.Owner)
		require.NoError(t, err)

		f.mockBankKeeper.EXPECT().
			SendCoinsFromModuleToAccount(gomock.Any(), types.ModuleName, expectedAddr, expectedCoins).
			Return(errors.New("SendCoinFromModuleToAccount fails"))

		_, err = f.msgServer.MintPC(f.ctx, msg)
		require.ErrorContains(t, err, "failed to send coins from module to account")
	})

	t.Run("success", func(t *testing.T) {
		msg := &types.MsgMintPC{
			Signer:           validSigner.String(),
			UniversalAccount: validUA,
			TxHash:           validTxHash,
		}

		addr := common.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")

		padded := common.LeftPadBytes(addr.Bytes(), 32)
		receipt := &evmtypes.MsgEthereumTxResponse{
			Ret: padded,
		}

		usdAmount := new(big.Int)
		usdAmount.SetString("1000000000000000000", 10) // 10 USD, 18 decimals
		decimals := uint32(18)
		amountToMint := uekeeper.ConvertUsdToPCTokens(usdAmount, decimals)
		expectedCoins := sdk.NewCoins(sdk.NewCoin(pchaintypes.BaseDenom, amountToMint))

		// Mock VerifyAndGetLockedFunds
		f.mockUTVKeeper.EXPECT().
			VerifyAndGetLockedFunds(gomock.Any(), validUA.Owner, validTxHash, validUA.Chain).
			Return(*big.NewInt(1_000_000), uint32(6), nil)

		f.mockEVMKeeper.EXPECT().
			CallEVM(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(receipt, nil)

		// MintCoins should be called with correct args
		f.mockBankKeeper.EXPECT().
			MintCoins(gomock.Any(), types.ModuleName, expectedCoins).
			Return(nil)

		// Expected Cosmos address from UEA address (derived from validUA.Owner)
		expectedAddr, err := utils.ConvertAnyAddressToBytes(validUA.Owner)
		require.NoError(t, err)

		f.mockBankKeeper.EXPECT().
			SendCoinsFromModuleToAccount(gomock.Any(), types.ModuleName, expectedAddr, expectedCoins).
			Return(nil)

		_, err = f.msgServer.MintPC(f.ctx, msg)
		require.NoError(t, err)
	})

}

func TestMsgServer_ExecutePayload(t *testing.T) {
	f := SetupTest(t)

	validSigner := f.addrs[0]
	validUA := &types.UniversalAccount{
		Owner: "0x1234567890abcdef1234567890abcdef12345678",
		Chain: "ethereum",
	}
	validUP := &types.UniversalPayload{
		To:                   "0x1234567890abcdef1234567890abcdef12345670",
		Value:                "10",
		Data:                 "test-data",
		GasLimit:             "1000000000000",
		MaxFeePerGas:         "10",
		MaxPriorityFeePerGas: "10",
		Nonce:                "1",
		Deadline:             "some-deadline",
	}

	t.Run("fail; invalid signer address", func(t *testing.T) {
		msg := &types.MsgExecutePayload{
			Signer:           "invalid_address",
			UniversalAccount: validUA,
			UniversalPayload: validUP,
			Signature:        "test-signature",
		}

		_, err := f.msgServer.ExecutePayload(f.ctx, msg)
		require.ErrorContains(t, err, "failed to parse signer address")
	})

	t.Run("fail; gateway interaction tx not verified", func(t *testing.T) {
		// You can inject failure in f.app or f.k.utvKeeper if mockable
		msg := &types.MsgExecutePayload{
			Signer:           validSigner.String(),
			UniversalAccount: validUA,
			UniversalPayload: validUP,
			Signature:        "test-signature",
		}

		_, err := f.msgServer.ExecutePayload(f.ctx, msg)
		require.Error(t, err)
	})
}
