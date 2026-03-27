package keeper_test

import (
	"fmt"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/pushchain/push-chain-node/x/uexecutor/types"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
	"github.com/stretchr/testify/require"

	evmtypes "github.com/cosmos/evm/x/vm/types"
)

func setupInboundFundsFixture(t *testing.T) *testFixture {
	t.Helper()
	f := SetupTest(t)

	f.mockEVMKeeper.EXPECT().SetAccount(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	f.mockEVMKeeper.EXPECT().SetCode(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	f.mockEVMKeeper.EXPECT().SetState(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	f.k.InitGenesis(f.ctx, &types.GenesisState{Params: types.DefaultParams()})
	return f
}

func TestExecuteInboundFunds_FailedEVMCall_CapturesTxHash(t *testing.T) {
	f := setupInboundFundsFixture(t)
	require := require.New(t)

	// Setup: create a UTX in state so ExecuteInboundFunds can update it
	inbound := types.Inbound{
		SourceChain: "eip155:1",
		TxHash:      "0xabc123",
		LogIndex:    0,
		Sender:      "0x1234567890abcdef1234567890abcdef12345678",
		Recipient:   "0xabcdefabcdefabcdefabcdefabcdefabcdefabcd",
		Amount:      "1000000",
		AssetAddr:   "0xUSDC",
		TxType:      types.TxType_FUNDS,
	}
	utxKey := types.GetInboundUniversalTxKey(inbound)
	utx := types.UniversalTx{
		Id:        utxKey,
		InboundTx: &inbound,
	}
	require.NoError(f.k.CreateUniversalTx(f.ctx, utxKey, utx))

	// Mock: uregistry returns a valid token config
	f.mockUregistryKeeper.EXPECT().
		GetTokenConfig(gomock.Any(), "eip155:1", "0xUSDC").
		Return(uregistrytypes.TokenConfig{
			NativeRepresentation: &uregistrytypes.NativeRepresentation{
				ContractAddress: "0xPRC20",
			},
		}, nil)

	// Mock: DerivedEVMCall returns BOTH a receipt (with hash) AND an error (EVM revert)
	revertedReceipt := &evmtypes.MsgEthereumTxResponse{
		Hash:    "0xdeadbeef_reverted_tx_hash",
		GasUsed: 21000,
		VmError: "execution reverted",
	}
	f.mockEVMKeeper.EXPECT().
		DerivedEVMCall(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
			gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
			gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
			gomock.Any(), gomock.Any()).
		Return(revertedReceipt, fmt.Errorf("execution reverted: %s", revertedReceipt.VmError))

	// Execute
	err := f.k.ExecuteInboundFunds(f.ctx, utx)
	require.NoError(err) // ExecuteInboundFunds returns nil (schedules revert instead)

	// Verify: the PCTx should have the tx hash from the reverted receipt
	savedUtx, found, err := f.k.GetUniversalTx(f.ctx, utxKey)
	require.NoError(err)
	require.True(found)
	require.NotNil(savedUtx.PcTx)
	require.Len(savedUtx.PcTx, 1)

	pcTx := savedUtx.PcTx[0]
	require.Equal("FAILED", pcTx.Status)
	require.Contains(pcTx.ErrorMsg, "execution reverted")
	// KEY ASSERTION: tx hash is captured even on failure
	require.Equal("0xdeadbeef_reverted_tx_hash", pcTx.TxHash)
	require.Equal(uint64(21000), pcTx.GasUsed)
}

func TestExecuteInboundFunds_GoLevelError_NoTxHash(t *testing.T) {
	f := setupInboundFundsFixture(t)
	require := require.New(t)

	inbound := types.Inbound{
		SourceChain: "eip155:1",
		TxHash:      "0xabc456",
		LogIndex:    0,
		Sender:      "0x1234567890abcdef1234567890abcdef12345678",
		Recipient:   "0xabcdefabcdefabcdefabcdefabcdefabcdefabcd",
		Amount:      "1000000",
		AssetAddr:   "0xUSDC",
		TxType:      types.TxType_FUNDS,
	}
	utxKey := types.GetInboundUniversalTxKey(inbound)
	utx := types.UniversalTx{
		Id:        utxKey,
		InboundTx: &inbound,
	}
	require.NoError(f.k.CreateUniversalTx(f.ctx, utxKey, utx))

	// Mock: uregistry returns a token config with nil NativeRepresentation
	// This causes a Go-level error (not EVM revert) -- no receipt returned
	f.mockUregistryKeeper.EXPECT().
		GetTokenConfig(gomock.Any(), "eip155:1", "0xUSDC").
		Return(uregistrytypes.TokenConfig{
			NativeRepresentation: nil,
		}, nil)

	// Execute
	err := f.k.ExecuteInboundFunds(f.ctx, utx)
	require.NoError(err) // returns nil, schedules revert

	// Verify: PCTx should have no tx hash (Go error, no EVM tx was created)
	savedUtx, found, err := f.k.GetUniversalTx(f.ctx, utxKey)
	require.NoError(err)
	require.True(found)
	require.NotNil(savedUtx.PcTx)
	require.Len(savedUtx.PcTx, 1)

	pcTx := savedUtx.PcTx[0]
	require.Equal("FAILED", pcTx.Status)
	require.Contains(pcTx.ErrorMsg, "no native representation")
	// No tx hash for Go-level errors -- no EVM tx was ever created
	require.Empty(pcTx.TxHash)
}

func TestExecuteInboundFunds_Success_HasTxHash(t *testing.T) {
	f := setupInboundFundsFixture(t)
	require := require.New(t)

	inbound := types.Inbound{
		SourceChain: "eip155:1",
		TxHash:      "0xabc789",
		LogIndex:    0,
		Sender:      "0x1234567890abcdef1234567890abcdef12345678",
		Recipient:   "0xabcdefabcdefabcdefabcdefabcdefabcdefabcd",
		Amount:      "1000000",
		AssetAddr:   "0xUSDC",
		TxType:      types.TxType_FUNDS,
	}
	utxKey := types.GetInboundUniversalTxKey(inbound)
	utx := types.UniversalTx{
		Id:        utxKey,
		InboundTx: &inbound,
	}
	require.NoError(f.k.CreateUniversalTx(f.ctx, utxKey, utx))

	f.mockUregistryKeeper.EXPECT().
		GetTokenConfig(gomock.Any(), "eip155:1", "0xUSDC").
		Return(uregistrytypes.TokenConfig{
			NativeRepresentation: &uregistrytypes.NativeRepresentation{
				ContractAddress: "0xPRC20",
			},
		}, nil)

	successReceipt := &evmtypes.MsgEthereumTxResponse{
		Hash:    "0xsuccess_tx_hash",
		GasUsed: 50000,
	}
	f.mockEVMKeeper.EXPECT().
		DerivedEVMCall(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
			gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
			gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
			gomock.Any(), gomock.Any()).
		Return(successReceipt, nil)

	err := f.k.ExecuteInboundFunds(f.ctx, utx)
	require.NoError(err)

	savedUtx, found, err := f.k.GetUniversalTx(f.ctx, utxKey)
	require.NoError(err)
	require.True(found)
	require.NotNil(savedUtx.PcTx)
	require.Len(savedUtx.PcTx, 1)

	pcTx := savedUtx.PcTx[0]
	require.Equal("SUCCESS", pcTx.Status)
	require.Equal("0xsuccess_tx_hash", pcTx.TxHash)
	require.Equal(uint64(50000), pcTx.GasUsed)
	require.Empty(pcTx.ErrorMsg)
}
