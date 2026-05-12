package integrationtest

import (
	"math/big"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"

	utils "github.com/pushchain/push-chain-node/test/utils"
	uexecutorkeeper "github.com/pushchain/push-chain-node/x/uexecutor/keeper"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
)

// ---------------------------------------------------------------------------
// Query: AllPendingInbounds
// ---------------------------------------------------------------------------

func TestQueryAllPendingInbounds(t *testing.T) {
	t.Run("empty result when no pending inbounds", func(t *testing.T) {
		app, ctx, _ := utils.SetAppWithValidators(t)

		resp, err := app.UexecutorKeeper.AllPendingInbounds(
			sdk.WrapSDKContext(ctx),
			&uexecutortypes.QueryAllPendingInboundsRequest{
				Pagination: &query.PageRequest{},
			},
		)
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Empty(t, resp.InboundIds)
	})

	t.Run("returns inbound ids after adding pending inbounds", func(t *testing.T) {
		app, ctx, _ := utils.SetAppWithValidators(t)

		inbound1 := uexecutortypes.Inbound{
			SourceChain: "eip155:11155111",
			TxHash:      "0xpending001",
			LogIndex:    "0",
		}
		inbound2 := uexecutortypes.Inbound{
			SourceChain: "eip155:11155111",
			TxHash:      "0xpending002",
			LogIndex:    "0",
		}

		err := app.UexecutorKeeper.AddPendingInbound(ctx, inbound1)
		require.NoError(t, err)

		err = app.UexecutorKeeper.AddPendingInbound(ctx, inbound2)
		require.NoError(t, err)

		resp, err := app.UexecutorKeeper.AllPendingInbounds(
			sdk.WrapSDKContext(ctx),
			&uexecutortypes.QueryAllPendingInboundsRequest{
				Pagination: &query.PageRequest{},
			},
		)
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Len(t, resp.InboundIds, 2)

		expectedKey1 := uexecutortypes.GetInboundUniversalTxKey(inbound1)
		expectedKey2 := uexecutortypes.GetInboundUniversalTxKey(inbound2)

		idSet := make(map[string]bool, len(resp.InboundIds))
		for _, id := range resp.InboundIds {
			idSet[id] = true
		}
		require.True(t, idSet[expectedKey1], "expected key for inbound1 to be present")
		require.True(t, idSet[expectedKey2], "expected key for inbound2 to be present")
	})

	t.Run("nil request returns error", func(t *testing.T) {
		app, ctx, _ := utils.SetAppWithValidators(t)

		_, err := app.UexecutorKeeper.AllPendingInbounds(sdk.WrapSDKContext(ctx), nil)
		require.Error(t, err)
	})

	t.Run("adding same inbound twice does not create duplicate entries", func(t *testing.T) {
		app, ctx, _ := utils.SetAppWithValidators(t)

		inbound := uexecutortypes.Inbound{
			SourceChain: "eip155:11155111",
			TxHash:      "0xduplicate001",
			LogIndex:    "0",
		}

		err := app.UexecutorKeeper.AddPendingInbound(ctx, inbound)
		require.NoError(t, err)

		// Adding again must be idempotent
		err = app.UexecutorKeeper.AddPendingInbound(ctx, inbound)
		require.NoError(t, err)

		resp, err := app.UexecutorKeeper.AllPendingInbounds(
			sdk.WrapSDKContext(ctx),
			&uexecutortypes.QueryAllPendingInboundsRequest{
				Pagination: &query.PageRequest{},
			},
		)
		require.NoError(t, err)
		require.Len(t, resp.InboundIds, 1, "duplicate add must not create a second entry")
	})
}

// ---------------------------------------------------------------------------
// Query: Params
// ---------------------------------------------------------------------------

func TestQueryParams(t *testing.T) {
	t.Run("returns params after they are set", func(t *testing.T) {
		app, ctx, _ := utils.SetAppWithValidators(t)
		querier := uexecutorkeeper.NewQuerier(app.UexecutorKeeper)

		// The genesis init already sets DefaultParams; just verify we can read them back.
		resp, err := querier.Params(sdk.WrapSDKContext(ctx), &uexecutortypes.QueryParamsRequest{})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.NotNil(t, resp.Params)
	})

	t.Run("returns updated params after explicit set", func(t *testing.T) {
		app, ctx, _ := utils.SetAppWithValidators(t)
		querier := uexecutorkeeper.NewQuerier(app.UexecutorKeeper)

		newParams := uexecutortypes.Params{SomeValue: false}
		err := app.UexecutorKeeper.Params.Set(ctx, newParams)
		require.NoError(t, err)

		resp, err := querier.Params(sdk.WrapSDKContext(ctx), &uexecutortypes.QueryParamsRequest{})
		require.NoError(t, err)
		require.NotNil(t, resp.Params)
		require.Equal(t, newParams.SomeValue, resp.Params.SomeValue)
	})
}

// ---------------------------------------------------------------------------
// UpdateParams
// ---------------------------------------------------------------------------

func TestUpdateParams(t *testing.T) {
	t.Run("update replaces existing params", func(t *testing.T) {
		app, ctx, _ := utils.SetAppWithValidators(t)

		initialParams := uexecutortypes.DefaultParams()
		err := app.UexecutorKeeper.Params.Set(ctx, initialParams)
		require.NoError(t, err)

		updatedParams := uexecutortypes.Params{SomeValue: !initialParams.SomeValue}
		err = app.UexecutorKeeper.UpdateParams(ctx, updatedParams)
		require.NoError(t, err)

		stored, err := app.UexecutorKeeper.Params.Get(ctx)
		require.NoError(t, err)
		require.Equal(t, updatedParams.SomeValue, stored.SomeValue)
	})

	t.Run("update is idempotent when called with same params", func(t *testing.T) {
		app, ctx, _ := utils.SetAppWithValidators(t)

		params := uexecutortypes.DefaultParams()
		err := app.UexecutorKeeper.UpdateParams(ctx, params)
		require.NoError(t, err)

		err = app.UexecutorKeeper.UpdateParams(ctx, params)
		require.NoError(t, err)

		stored, err := app.UexecutorKeeper.Params.Get(ctx)
		require.NoError(t, err)
		require.Equal(t, params.SomeValue, stored.SomeValue)
	})
}

// ---------------------------------------------------------------------------
// CreateUniversalTxFromPCTx
// ---------------------------------------------------------------------------

func TestCreateUniversalTxFromPCTx(t *testing.T) {
	t.Run("creates a new UTX keyed from the PCTx", func(t *testing.T) {
		app, ctx, _ := utils.SetAppWithValidators(t)

		pcTx := uexecutortypes.PCTx{
			TxHash:      "0xpc001",
			Sender:      utils.GetDefaultAddresses().DefaultTestAddr,
			GasUsed:     21000,
			BlockHeight: 1,
			Status:      "SUCCESS",
		}

		utx, err := app.UexecutorKeeper.CreateUniversalTxFromPCTx(ctx, pcTx)
		require.NoError(t, err)
		require.NotNil(t, utx)

		// Verify the UTX was persisted
		stored, found, err := app.UexecutorKeeper.GetUniversalTx(ctx, utx.Id)
		require.NoError(t, err)
		require.True(t, found)

		require.Len(t, stored.PcTx, 1)
		require.Equal(t, pcTx.TxHash, stored.PcTx[0].TxHash)
		require.Nil(t, stored.InboundTx)
		require.Empty(t, stored.OutboundTx)
	})

	t.Run("returns error when UTX already exists for the same PCTx", func(t *testing.T) {
		app, ctx, _ := utils.SetAppWithValidators(t)

		pcTx := uexecutortypes.PCTx{
			TxHash:      "0xpc002",
			Sender:      utils.GetDefaultAddresses().DefaultTestAddr,
			GasUsed:     21000,
			BlockHeight: 1,
			Status:      "SUCCESS",
		}

		_, err := app.UexecutorKeeper.CreateUniversalTxFromPCTx(ctx, pcTx)
		require.NoError(t, err)

		// Second creation with same pcTx must fail
		_, err = app.UexecutorKeeper.CreateUniversalTxFromPCTx(ctx, pcTx)
		require.Error(t, err)
		require.Contains(t, err.Error(), "already exists")
	})

	t.Run("two different pcTx hashes produce two distinct UTXs", func(t *testing.T) {
		app, ctx, _ := utils.SetAppWithValidators(t)

		pcTx1 := uexecutortypes.PCTx{
			TxHash:      "0xpc003a",
			Sender:      utils.GetDefaultAddresses().DefaultTestAddr,
			GasUsed:     21000,
			BlockHeight: 1,
			Status:      "SUCCESS",
		}
		pcTx2 := uexecutortypes.PCTx{
			TxHash:      "0xpc003b",
			Sender:      utils.GetDefaultAddresses().DefaultTestAddr,
			GasUsed:     21000,
			BlockHeight: 1,
			Status:      "SUCCESS",
		}

		utx1, err := app.UexecutorKeeper.CreateUniversalTxFromPCTx(ctx, pcTx1)
		require.NoError(t, err)

		utx2, err := app.UexecutorKeeper.CreateUniversalTxFromPCTx(ctx, pcTx2)
		require.NoError(t, err)

		require.NotEqual(t, utx1.Id, utx2.Id, "different pcTx hashes must produce different UTX ids")
	})
}

// ---------------------------------------------------------------------------
// PostTxProcessing — no-op path (receipt with no matching event logs)
// ---------------------------------------------------------------------------

func TestPostTxProcessing_NoMatchingLogs(t *testing.T) {
	t.Run("receipt with nil logs is a no-op", func(t *testing.T) {
		app, ctx, _ := utils.SetAppWithValidators(t)
		hooks := uexecutorkeeper.NewEVMHooks(app.UexecutorKeeper)

		sender := common.HexToAddress(utils.GetDefaultAddresses().DefaultTestAddr)
		err := hooks.PostTxProcessing(ctx, sender, core.Message{}, nil)
		require.NoError(t, err)
	})

	t.Run("receipt with empty logs is a no-op", func(t *testing.T) {
		app, ctx, _ := utils.SetAppWithValidators(t)
		hooks := uexecutorkeeper.NewEVMHooks(app.UexecutorKeeper)

		sender := common.HexToAddress(utils.GetDefaultAddresses().DefaultTestAddr)
		receipt := &ethtypes.Receipt{
			TxHash:  common.HexToHash("0xabc"),
			GasUsed: 21000,
			Logs:    []*ethtypes.Log{},
		}

		err := hooks.PostTxProcessing(ctx, sender, core.Message{}, receipt)
		require.NoError(t, err)
	})

	t.Run("receipt with logs from non-gateway address is a no-op", func(t *testing.T) {
		app, ctx, _ := utils.SetAppWithValidators(t)
		hooks := uexecutorkeeper.NewEVMHooks(app.UexecutorKeeper)

		sender := common.HexToAddress(utils.GetDefaultAddresses().DefaultTestAddr)
		randomAddr := common.HexToAddress("0x1234567890123456789012345678901234567890")
		log := &ethtypes.Log{
			Address: randomAddr,
			Topics:  []common.Hash{common.HexToHash("0xdeadbeef")},
			Data:    []byte{},
		}
		receipt := &ethtypes.Receipt{
			TxHash:  common.HexToHash("0xabc"),
			GasUsed: 21000,
			Logs:    []*ethtypes.Log{log},
		}

		// Should be a no-op: no UniversalTx should be created
		err := hooks.PostTxProcessing(ctx, sender, core.Message{}, receipt)
		require.NoError(t, err)

		// Confirm no UTX was created
		querier := uexecutorkeeper.NewQuerier(app.UexecutorKeeper)
		allResp, err := querier.AllUniversalTx(
			sdk.WrapSDKContext(ctx),
			&uexecutortypes.QueryAllUniversalTxRequest{
				Pagination: &query.PageRequest{},
			},
		)
		require.NoError(t, err)
		require.Empty(t, allResp.UniversalTxs)
	})
}

// ---------------------------------------------------------------------------
// CreateUniversalTxFromReceiptIfOutbound — no-op when no matching logs
// ---------------------------------------------------------------------------

func TestCreateUniversalTxFromReceiptIfOutbound_NoLogs(t *testing.T) {
	t.Run("receipt with no logs results in no UTX creation", func(t *testing.T) {
		app, ctx, _ := utils.SetAppWithValidators(t)

		receipt := &evmtypes.MsgEthereumTxResponse{
			Hash:    "0xnologs",
			GasUsed: 21000,
			Logs:    []*evmtypes.Log{},
		}
		pcTx := uexecutortypes.PCTx{
			TxHash:      "0xnologs",
			Sender:      utils.GetDefaultAddresses().DefaultTestAddr,
			GasUsed:     21000,
			BlockHeight: 1,
			Status:      "SUCCESS",
		}

		err := app.UexecutorKeeper.CreateUniversalTxFromReceiptIfOutbound(ctx, receipt, pcTx)
		require.NoError(t, err)

		// Confirm no UTX was created
		querier := uexecutorkeeper.NewQuerier(app.UexecutorKeeper)
		allResp, err := querier.AllUniversalTx(
			sdk.WrapSDKContext(ctx),
			&uexecutortypes.QueryAllUniversalTxRequest{
				Pagination: &query.PageRequest{},
			},
		)
		require.NoError(t, err)
		require.Empty(t, allResp.UniversalTxs)
	})

	t.Run("receipt with logs from non-gateway address results in no UTX creation", func(t *testing.T) {
		app, ctx, _ := utils.SetAppWithValidators(t)

		receipt := &evmtypes.MsgEthereumTxResponse{
			Hash:    "0xwrongaddr",
			GasUsed: 21000,
			Logs: []*evmtypes.Log{
				{
					Address: "0x1111111111111111111111111111111111111111",
					Topics:  []string{"0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"},
					Data:    []byte{},
					Removed: false,
				},
			},
		}
		pcTx := uexecutortypes.PCTx{
			TxHash:      "0xwrongaddr",
			Sender:      utils.GetDefaultAddresses().DefaultTestAddr,
			GasUsed:     21000,
			BlockHeight: 1,
			Status:      "SUCCESS",
		}

		err := app.UexecutorKeeper.CreateUniversalTxFromReceiptIfOutbound(ctx, receipt, pcTx)
		require.NoError(t, err)

		querier := uexecutorkeeper.NewQuerier(app.UexecutorKeeper)
		allResp, err := querier.AllUniversalTx(
			sdk.WrapSDKContext(ctx),
			&uexecutortypes.QueryAllUniversalTxRequest{
				Pagination: &query.PageRequest{},
			},
		)
		require.NoError(t, err)
		require.Empty(t, allResp.UniversalTxs)
	})

	t.Run("removed logs are ignored and result in no UTX creation", func(t *testing.T) {
		app, ctx, _ := utils.SetAppWithValidators(t)

		gatewayAddr := utils.GetDefaultAddresses().UniversalGatewayPCAddr.Hex()

		receipt := &evmtypes.MsgEthereumTxResponse{
			Hash:    "0xremoved",
			GasUsed: 21000,
			Logs: []*evmtypes.Log{
				{
					Address: gatewayAddr,
					Topics:  []string{uexecutortypes.UniversalTxOutboundEventSig},
					Data:    []byte{},
					Removed: true, // marked as removed / reorg'd
				},
			},
		}
		pcTx := uexecutortypes.PCTx{
			TxHash:      "0xremoved",
			Sender:      utils.GetDefaultAddresses().DefaultTestAddr,
			GasUsed:     21000,
			BlockHeight: 1,
			Status:      "SUCCESS",
		}

		err := app.UexecutorKeeper.CreateUniversalTxFromReceiptIfOutbound(ctx, receipt, pcTx)
		require.NoError(t, err)

		querier := uexecutorkeeper.NewQuerier(app.UexecutorKeeper)
		allResp, err := querier.AllUniversalTx(
			sdk.WrapSDKContext(ctx),
			&uexecutortypes.QueryAllUniversalTxRequest{
				Pagination: &query.PageRequest{},
			},
		)
		require.NoError(t, err)
		require.Empty(t, allResp.UniversalTxs)
	})
}

// ---------------------------------------------------------------------------
// PostTxProcessing — happy path with synthetic UniversalTxOutbound event
// ---------------------------------------------------------------------------

// encodeUniversalTxOutboundData ABI-encodes the non-indexed fields of the
// UniversalTxOutbound event so we can build a synthetic log.
func encodeUniversalTxOutboundData(
	chainId string,
	target []byte,
	amount *big.Int,
	gasToken common.Address,
	gasFee *big.Int,
	gasLimit *big.Int,
	payload []byte,
	protocolFee *big.Int,
	revertRecipient common.Address,
	txType uint8,
	gasPrice *big.Int,
) ([]byte, error) {
	stringType, _ := abi.NewType("string", "", nil)
	bytesType, _ := abi.NewType("bytes", "", nil)
	uint256Type, _ := abi.NewType("uint256", "", nil)
	addressType, _ := abi.NewType("address", "", nil)
	uint8Type, _ := abi.NewType("uint8", "", nil)

	args := abi.Arguments{
		{Type: stringType},  // chainId
		{Type: bytesType},   // target
		{Type: uint256Type}, // amount
		{Type: addressType}, // gasToken
		{Type: uint256Type}, // gasFee
		{Type: uint256Type}, // gasLimit
		{Type: bytesType},   // payload
		{Type: uint256Type}, // protocolFee
		{Type: addressType}, // revertRecipient
		{Type: uint8Type},   // txType
		{Type: uint256Type}, // gasPrice
	}

	return args.Pack(
		chainId, target, amount, gasToken, gasFee, gasLimit,
		payload, protocolFee, revertRecipient, txType, gasPrice,
	)
}

func TestPostTxProcessing_WithSyntheticOutboundEvent(t *testing.T) {
	t.Run("synthetic UniversalTxOutbound event creates UTX and outbound", func(t *testing.T) {
		chainApp, ctx, _ := utils.SetAppWithValidators(t)

		destChain := "eip155:11155111"
		chainConfig := uregistrytypes.ChainConfig{
			Chain:          destChain,
			VmType:         uregistrytypes.VmType_EVM,
			PublicRpcUrl:   "https://sepolia.drpc.org",
			GatewayAddress: "0x28E0F09bE2321c1420Dc60Ee146aACbD68B335Fe",
			Enabled: &uregistrytypes.ChainEnabled{
				IsInboundEnabled:  true,
				IsOutboundEnabled: true,
			},
		}
		require.NoError(t, chainApp.UregistryKeeper.AddChainConfig(ctx, &chainConfig))

		usdcAddr := utils.GetDefaultAddresses().ExternalUSDCAddr
		prc20Addr := utils.GetDefaultAddresses().PRC20USDCAddr
		tokenConfig := uregistrytypes.TokenConfig{
			Chain:        destChain,
			Address:      usdcAddr.String(),
			Name:         "USD Coin",
			Symbol:       "USDC",
			Decimals:     6,
			Enabled:      true,
			LiquidityCap: "1000000000000000000000000",
			TokenType:    1,
			NativeRepresentation: &uregistrytypes.NativeRepresentation{
				Denom:           "",
				ContractAddress: prc20Addr.String(),
			},
		}
		require.NoError(t, chainApp.UregistryKeeper.AddTokenConfig(ctx, &tokenConfig))

		gatewayAddr := uregistrytypes.SYSTEM_CONTRACTS["UNIVERSAL_GATEWAY_PC"].Address
		eventSigHash := common.HexToHash(uexecutortypes.UniversalTxOutboundEventSig)
		txIdHash := common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000001")
		senderHash := common.HexToHash("0x000000000000000000000000" + utils.GetDefaultAddresses().DefaultTestAddr[2:])
		tokenHash := common.HexToHash("0x000000000000000000000000" + prc20Addr.Hex()[2:])
		recipient := common.HexToAddress("0x527f3692f5c53cfa83f7689885995606f93b6164")

		data, err := encodeUniversalTxOutboundData(
			destChain, recipient.Bytes(), big.NewInt(1000000),
			common.Address{}, big.NewInt(111), big.NewInt(21000),
			[]byte{}, big.NewInt(0),
			common.HexToAddress(utils.GetDefaultAddresses().DefaultTestAddr),
			2, big.NewInt(1000000000),
		)
		require.NoError(t, err)

		evmLog := &ethtypes.Log{
			Address: common.HexToAddress(gatewayAddr),
			Topics:  []common.Hash{eventSigHash, txIdHash, senderHash, tokenHash},
			Data:    data,
			Removed: false,
		}
		receipt := &ethtypes.Receipt{
			TxHash:  common.HexToHash("0xsynth001"),
			GasUsed: 50000,
			Logs:    []*ethtypes.Log{evmLog},
		}

		sender := common.HexToAddress(utils.GetDefaultAddresses().DefaultTestAddr)
		hooks := uexecutorkeeper.NewEVMHooks(chainApp.UexecutorKeeper)

		err = hooks.PostTxProcessing(ctx, sender, core.Message{}, receipt)
		require.NoError(t, err)

		querier := uexecutorkeeper.NewQuerier(chainApp.UexecutorKeeper)
		allResp, err := querier.AllUniversalTx(
			sdk.WrapSDKContext(ctx),
			&uexecutortypes.QueryAllUniversalTxRequest{Pagination: &query.PageRequest{}},
		)
		require.NoError(t, err)
		require.NotEmpty(t, allResp.UniversalTxs, "UTX should be created from synthetic event")

		utx := allResp.UniversalTxs[0]
		require.NotEmpty(t, utx.OutboundTx, "outbound should be attached to UTX")
		require.Equal(t, destChain, utx.OutboundTx[0].DestinationChain)
		require.Equal(t, "1000000", utx.OutboundTx[0].Amount)
		require.Equal(t, uexecutortypes.TxType_FUNDS, utx.OutboundTx[0].TxType)
		require.Equal(t, uexecutortypes.Status_PENDING, utx.OutboundTx[0].OutboundStatus)
	})

	t.Run("outbound disabled returns error", func(t *testing.T) {
		chainApp, ctx, _ := utils.SetAppWithValidators(t)

		destChain := "eip155:11155111"
		chainConfig := uregistrytypes.ChainConfig{
			Chain:          destChain,
			VmType:         uregistrytypes.VmType_EVM,
			PublicRpcUrl:   "https://sepolia.drpc.org",
			GatewayAddress: "0x28E0F09bE2321c1420Dc60Ee146aACbD68B335Fe",
			Enabled: &uregistrytypes.ChainEnabled{
				IsInboundEnabled:  true,
				IsOutboundEnabled: false,
			},
		}
		require.NoError(t, chainApp.UregistryKeeper.AddChainConfig(ctx, &chainConfig))

		gatewayAddr := uregistrytypes.SYSTEM_CONTRACTS["UNIVERSAL_GATEWAY_PC"].Address
		eventSigHash := common.HexToHash(uexecutortypes.UniversalTxOutboundEventSig)
		txIdHash := common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000002")
		senderHash := common.HexToHash("0x000000000000000000000000" + utils.GetDefaultAddresses().DefaultTestAddr[2:])
		prc20Addr := utils.GetDefaultAddresses().PRC20USDCAddr
		tokenHash := common.HexToHash("0x000000000000000000000000" + prc20Addr.Hex()[2:])
		recipient := common.HexToAddress("0x527f3692f5c53cfa83f7689885995606f93b6164")

		data, err := encodeUniversalTxOutboundData(
			destChain, recipient.Bytes(), big.NewInt(500000),
			common.Address{}, big.NewInt(111), big.NewInt(21000),
			[]byte{}, big.NewInt(0),
			common.HexToAddress(utils.GetDefaultAddresses().DefaultTestAddr),
			2, big.NewInt(1000000000),
		)
		require.NoError(t, err)

		evmLog := &ethtypes.Log{
			Address: common.HexToAddress(gatewayAddr),
			Topics:  []common.Hash{eventSigHash, txIdHash, senderHash, tokenHash},
			Data:    data,
			Removed: false,
		}
		receipt := &ethtypes.Receipt{
			TxHash:  common.HexToHash("0xsynth002"),
			GasUsed: 50000,
			Logs:    []*ethtypes.Log{evmLog},
		}

		sender := common.HexToAddress(utils.GetDefaultAddresses().DefaultTestAddr)
		hooks := uexecutorkeeper.NewEVMHooks(chainApp.UexecutorKeeper)

		err = hooks.PostTxProcessing(ctx, sender, core.Message{}, receipt)
		require.Error(t, err)
		require.Contains(t, err.Error(), "outbound is disabled")
	})
}
