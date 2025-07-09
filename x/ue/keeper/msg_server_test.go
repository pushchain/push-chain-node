package keeper_test

import (
	"errors"
	"math/big"
	"testing"

	evmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/golang/mock/gomock"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ethereum/go-ethereum/common"
	pchaintypes "github.com/rollchains/pchain/types"
	"github.com/rollchains/pchain/utils"
	uekeeper "github.com/rollchains/pchain/x/ue/keeper"
	"github.com/rollchains/pchain/x/ue/types"
	ue "github.com/rollchains/pchain/x/ue/types"
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
	validUA := &types.UniversalAccountId{
		ChainNamespace: "eip155",
		ChainId:        "11155111",
		Owner:          "0x000000000000000000000000000000000000dead",
	}
	validTxHash := "0xabc123"

	t.Run("fail; invalid signer address", func(t *testing.T) {
		msg := &types.MsgDeployUEA{
			Signer:             "invalid_address",
			UniversalAccountId: validUA,
			TxHash:             validTxHash,
		}

		_, err := f.msgServer.DeployUEA(f.ctx, msg)
		require.ErrorContains(err, "failed to parse signer address")
	})

	t.Run("fail; gateway interaction tx not verified", func(t *testing.T) {
		// You can inject failure in f.app or f.k.utvKeeper if mockable
		msg := &types.MsgDeployUEA{
			Signer:             validSigner.String(),
			UniversalAccountId: validUA,
			TxHash:             "invalid_tx",
		}
		f.mockUTVKeeper.
			EXPECT().VerifyGatewayInteractionTx(gomock.Any(), validUA.Owner, "invalid_tx", validUA.GetCAIP2()).
			Return(errors.New("Gateway interaction failed"))

		_, err := f.msgServer.DeployUEA(f.ctx, msg)
		require.ErrorContains(err, "failed to verify gateway interaction transaction")
	})

	t.Run("fail: CallFactoryToDeployUEA Fails", func(t *testing.T) {
		msg := &types.MsgDeployUEA{
			Signer:             validSigner.String(),
			UniversalAccountId: validUA,
			TxHash:             validTxHash,
		}
		addr := common.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")

		padded := common.LeftPadBytes(addr.Bytes(), 32)
		receipt := &evmtypes.MsgEthereumTxResponse{
			Ret: padded,
		}
		f.mockUTVKeeper.
			EXPECT().VerifyGatewayInteractionTx(gomock.Any(), validUA.Owner, validTxHash, validUA.GetCAIP2()).
			Return(nil)

		f.mockEVMKeeper.EXPECT().
			CallEVM(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(receipt, errors.New("unable to deploy UEA"))

		_, err := f.msgServer.DeployUEA(f.ctx, msg)
		require.ErrorContains(err, "unable to deploy UEA")
	})

	t.Run("success; valid input returns UEA", func(t *testing.T) {
		msg := &types.MsgDeployUEA{
			Signer:             validSigner.String(),
			UniversalAccountId: validUA,
			TxHash:             validTxHash,
		}

		addr := common.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")

		padded := common.LeftPadBytes(addr.Bytes(), 32)
		receipt := &evmtypes.MsgEthereumTxResponse{
			Ret: padded,
		}
		f.mockUTVKeeper.
			EXPECT().VerifyGatewayInteractionTx(gomock.Any(), validUA.Owner, validTxHash, validUA.GetCAIP2()).
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
	validUA := &types.UniversalAccountId{
		ChainNamespace: "eip155",
		ChainId:        "11155111",
		Owner:          "0x000000000000000000000000000000000000dead",
	}
	validTxHash := "0xabc123"

	t.Run("fail: VerifyAndGetLockedFunds fails", func(t *testing.T) {
		msg := &types.MsgMintPC{
			Signer:             validSigner.String(),
			UniversalAccountId: validUA,
			TxHash:             validTxHash,
		}

		f.mockUTVKeeper.EXPECT().
			VerifyAndGetLockedFunds(gomock.Any(), validUA.Owner, validTxHash, validUA.GetCAIP2()).
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
			VerifyAndGetLockedFunds(gomock.Any(), validUA.Owner, validTxHash, validUA.GetCAIP2()).
			Return(*usdAmount, decimals, nil)

		f.mockEVMKeeper.EXPECT().
			CallEVM(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(receipt, errors.New("call-factory fails"))

		msg := &types.MsgMintPC{
			Signer: validSigner.String(), UniversalAccountId: validUA, TxHash: validTxHash,
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
			VerifyAndGetLockedFunds(gomock.Any(), validUA.Owner, validTxHash, validUA.GetCAIP2()).
			Return(*usdAmount, decimals, nil)

		f.mockEVMKeeper.EXPECT().
			CallEVM(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(receipt, nil)

		msg := &types.MsgMintPC{
			Signer: validSigner.String(), UniversalAccountId: validUA, TxHash: validTxHash,
		}
		_, err := f.msgServer.MintPC(f.ctx, msg)
		require.ErrorContains(t, err, "failed to convert EVM address")
	})

	t.Run("fail: Mint Fails", func(t *testing.T) {
		msg := &types.MsgMintPC{
			Signer:             validSigner.String(),
			UniversalAccountId: validUA,
			TxHash:             validTxHash,
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
			VerifyAndGetLockedFunds(gomock.Any(), validUA.Owner, validTxHash, validUA.GetCAIP2()).
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
			Signer:             validSigner.String(),
			UniversalAccountId: validUA,
			TxHash:             validTxHash,
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
			VerifyAndGetLockedFunds(gomock.Any(), validUA.Owner, validTxHash, validUA.GetCAIP2()).
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
			Signer:             validSigner.String(),
			UniversalAccountId: validUA,
			TxHash:             validTxHash,
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
			VerifyAndGetLockedFunds(gomock.Any(), validUA.Owner, validTxHash, validUA.GetCAIP2()).
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
	validUA := &types.UniversalAccountId{
		ChainNamespace: "eip155",
		ChainId:        "11155111",
		Owner:          "0x000000000000000000000000000000000000dead",
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
			Signer:             "invalid_address",
			UniversalAccountId: validUA,
			UniversalPayload:   validUP,
			VerificationData:   "test-signature",
		}

		_, err := f.msgServer.ExecutePayload(f.ctx, msg)
		require.ErrorContains(t, err, "failed to parse signer address")
	})

	t.Run("Fail : ChainConfig for Universal Accout not set", func(t *testing.T) {
		// You can inject failure in f.app or f.k.utvKeeper if mockable
		msg := &types.MsgExecutePayload{
			Signer:             validSigner.String(),
			UniversalAccountId: validUA,
			UniversalPayload:   validUP,
			VerificationData:   "test-signature",
		}
		_, err := f.msgServer.ExecutePayload(f.ctx, msg)
		require.ErrorContains(t, err, "failed to get chain config")
	})

	t.Run("Fail: CallFactoryToComputeUEAAddress", func(t *testing.T) {
		// You can inject failure in f.app or f.k.utvKeeper if mockable
		msg := &types.MsgExecutePayload{
			Signer:             validSigner.String(),
			UniversalAccountId: validUA,
			UniversalPayload:   validUP,
			VerificationData:   "test-signature",
		}

		chainConfigTest := types.ChainConfig{
			Chain:             "Ethereum",
			VmType:            ue.VM_TYPE_EVM, // replace with appropriate VM_TYPE enum value
			PublicRpcUrl:      "https://mainnet.infura.io/v3/YOUR_PROJECT_ID",
			GatewayAddress:    "0x1234567890abcdef1234567890abcdef12345678",
			BlockConfirmation: 12,
			GatewayMethods:    []*ue.MethodConfig{},
			Enabled:           true,
		}

		f.k.ChainConfigs.Set(f.ctx, "ethereum", chainConfigTest)

		f.mockEVMKeeper.EXPECT().CallEVM(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("CallFactoryToComputeUEAAddress Failed"))

		_, err := f.msgServer.ExecutePayload(f.ctx, msg)
		require.ErrorContains(t, err, "CallFactoryToComputeUEAAddress Failed")
	})

	t.Run("Fail : Invalid UniversalPayload", func(t *testing.T) {
		// You can inject failure in f.app or f.k.utvKeeper if mockable
		msg := &types.MsgExecutePayload{
			Signer:             validSigner.String(),
			UniversalAccountId: validUA,
			UniversalPayload:   validUP,
			VerificationData:   "test-signature",
		}
		addr := common.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")

		padded := common.LeftPadBytes(addr.Bytes(), 32)
		receipt := &evmtypes.MsgEthereumTxResponse{
			Ret: padded,
		}

		chainConfigTest := types.ChainConfig{
			Chain:             "Ethereum",
			VmType:            ue.VM_TYPE_EVM, // replace with appropriate VM_TYPE enum value
			PublicRpcUrl:      "https://mainnet.infura.io/v3/YOUR_PROJECT_ID",
			GatewayAddress:    "0x1234567890abcdef1234567890abcdef12345678",
			BlockConfirmation: 12,
			GatewayMethods:    []*ue.MethodConfig{},
			Enabled:           true,
		}

		f.k.ChainConfigs.Set(f.ctx, "ethereum", chainConfigTest)

		f.mockEVMKeeper.EXPECT().CallEVM(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(receipt, nil)

		_, err := f.msgServer.ExecutePayload(f.ctx, msg)
		require.ErrorContains(t, err, "invalid universal payload")
	})

	t.Run("Fail : Invalid Signature", func(t *testing.T) {
		avalidUP := &types.UniversalPayload{
			To:                   "0x8ba1f109551bD432803012645Ac136ddd64DBA72", // 20‑byte address
			Value:                "0",                                          // wei, decimal string
			Data:                 "0xdeadbeef",                                 // <- EVEN‑length hex → []byte{0xde, 0xad, 0xbe, 0xef}
			GasLimit:             "21000",                                      // decimal
			MaxFeePerGas:         "1000000000",                                 // 1 gwei
			MaxPriorityFeePerGas: "2000000000",                                 // 2 gwei
			Nonce:                "0",
			Deadline:             "0",
			VType:                ue.VerificationType_signedVerification,
		}
		// You can inject failure in f.app or f.k.utvKeeper if mockable
		msg := &types.MsgExecutePayload{
			Signer:             validSigner.String(),
			UniversalAccountId: validUA,
			UniversalPayload:   avalidUP,
			VerificationData:   "test-signature",
		}
		addr := common.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")

		padded := common.LeftPadBytes(addr.Bytes(), 32)
		receipt := &evmtypes.MsgEthereumTxResponse{
			Ret: padded,
		}

		chainConfigTest := types.ChainConfig{
			Chain:             "Ethereum",
			VmType:            ue.VM_TYPE_EVM, // replace with appropriate VM_TYPE enum value
			PublicRpcUrl:      "https://mainnet.infura.io/v3/YOUR_PROJECT_ID",
			GatewayAddress:    "0x1234567890abcdef1234567890abcdef12345678",
			BlockConfirmation: 12,
			GatewayMethods:    []*ue.MethodConfig{},
			Enabled:           true,
		}

		f.k.ChainConfigs.Set(f.ctx, "ethereum", chainConfigTest)

		f.mockEVMKeeper.EXPECT().CallEVM(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(receipt, nil)

		_, err := f.msgServer.ExecutePayload(f.ctx, msg)
		require.ErrorContains(t, err, "invalid signature format")
	})

}

func TestMsgServer_AddChainConfig(t *testing.T) {
	f := SetupTest(t)
	validSigner := f.addrs[0]

	chainConfigTest := types.ChainConfig{
		Chain:             "Ethereum",
		VmType:            ue.VM_TYPE_EVM, // replace with appropriate VM_TYPE enum value
		PublicRpcUrl:      "https://mainnet.infura.io/v3/YOUR_PROJECT_ID",
		GatewayAddress:    "0x1234567890abcdef1234567890abcdef12345678",
		BlockConfirmation: 12,
		GatewayMethods:    []*ue.MethodConfig{},
		Enabled:           true,
	}
	t.Run("Failed to get params", func(t *testing.T) {
		msg := &types.MsgAddChainConfig{
			Signer:      validSigner.String(),
			ChainConfig: &chainConfigTest,
		}

		_, err := f.msgServer.AddChainConfig(f.ctx, msg)
		require.ErrorContains(t, err, "failed to get params")
	})

	t.Run("fail : Invalid authority", func(t *testing.T) {
		msg := &types.MsgAddChainConfig{
			Signer:      validSigner.String(),
			ChainConfig: &chainConfigTest,
		}
		f.k.Params.Set(f.ctx, ue.Params{})
		_, err := f.msgServer.AddChainConfig(f.ctx, msg)
		require.ErrorContains(t, err, "invalid authority;")
	})

	t.Run("success!", func(t *testing.T) {
		msg := &types.MsgAddChainConfig{
			Signer:      validSigner.String(),
			ChainConfig: &chainConfigTest,
		}
		f.k.Params.Set(f.ctx, ue.Params{Admin: validSigner.String()})
		_, err := f.msgServer.AddChainConfig(f.ctx, msg)
		require.NoError(t, err) // flag : need to add verify condition
	})

}

func TestMsgServer_UpdateChainConfig(t *testing.T) {
	f := SetupTest(t)
	validSigner := f.addrs[0]

	chainConfigTest := types.ChainConfig{
		Chain:             "Ethereum",
		VmType:            ue.VM_TYPE_EVM, // replace with appropriate VM_TYPE enum value
		PublicRpcUrl:      "https://mainnet.infura.io/v3/YOUR_PROJECT_ID",
		GatewayAddress:    "0x1234567890abcdef1234567890abcdef12345678",
		BlockConfirmation: 12,
		GatewayMethods:    []*ue.MethodConfig{},
		Enabled:           true,
	}

	updatedChainConfigTest := types.ChainConfig{
		Chain:             "Ethereum",
		VmType:            ue.VM_TYPE_EVM, // replace with appropriate VM_TYPE enum value
		PublicRpcUrl:      "https://mainnet.infura.io/v3/YOUR_PROJECT_ID",
		GatewayAddress:    "0x1234567890abcdef1234567890abcdef12345678",
		BlockConfirmation: 14,
		GatewayMethods:    []*ue.MethodConfig{},
		Enabled:           true,
	}
	t.Run("Failed to get params", func(t *testing.T) {
		msg := &types.MsgUpdateChainConfig{
			Signer:      validSigner.String(),
			ChainConfig: &chainConfigTest,
		}

		_, err := f.msgServer.UpdateChainConfig(f.ctx, msg)
		require.ErrorContains(t, err, "failed to get params")
	})
	t.Run("fail : Invalid authority", func(t *testing.T) {
		msg := &types.MsgUpdateChainConfig{
			Signer:      validSigner.String(),
			ChainConfig: &chainConfigTest,
		}
		f.k.Params.Set(f.ctx, ue.Params{})
		_, err := f.msgServer.UpdateChainConfig(f.ctx, msg)
		require.ErrorContains(t, err, "invalid authority;")
	})

	t.Run("fail : config does not exist to update", func(t *testing.T) {
		msg := &types.MsgUpdateChainConfig{
			Signer:      validSigner.String(),
			ChainConfig: &chainConfigTest,
		}
		f.k.Params.Set(f.ctx, ue.Params{Admin: validSigner.String()})
		_, err := f.msgServer.UpdateChainConfig(f.ctx, msg)
		require.ErrorContains(t, err, "chain config for Ethereum does not exist")
	})

	t.Run("success!", func(t *testing.T) {
		addConfigMsg := &types.MsgAddChainConfig{
			Signer:      validSigner.String(),
			ChainConfig: &chainConfigTest,
		}
		f.k.Params.Set(f.ctx, ue.Params{Admin: validSigner.String()})
		_, err := f.msgServer.AddChainConfig(f.ctx, addConfigMsg)
		require.NoError(t, err)

		msg := &types.MsgUpdateChainConfig{
			Signer:      validSigner.String(),
			ChainConfig: &updatedChainConfigTest,
		}
		_, err = f.msgServer.UpdateChainConfig(f.ctx, msg)
		require.NoError(t, err) // flag : need to add verify condition (cross-checking)
	})
}
