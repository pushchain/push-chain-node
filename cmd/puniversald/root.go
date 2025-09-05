package main

import (
	"os"

	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/config"
	"github.com/cosmos/cosmos-sdk/client/flags"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	cosmosevmkeyring "github.com/cosmos/evm/crypto/keyring"
	"github.com/pushchain/push-chain-node/app"
	"github.com/pushchain/push-chain-node/app/params"
	"github.com/pushchain/push-chain-node/universalClient/constant"
	"github.com/spf13/cobra"
	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	"cosmossdk.io/log"
)

func NewRootCmd() *cobra.Command {
	// Create temporary app to get proper encoding configuration (like pchaind)
	tempApp := app.NewChainApp(
		log.NewNopLogger(), dbm.NewMemDB(), nil, false, simtestutil.NewAppOptionsWithFlagHome(tempDir()),
		[]wasmkeeper.Option{},
		app.EVMAppOptions,
	)
	encodingConfig := params.EncodingConfig{
		InterfaceRegistry: tempApp.InterfaceRegistry(),
		Codec:             tempApp.AppCodec(),
		TxConfig:          tempApp.TxConfig(),
		Amino:             tempApp.LegacyAmino(),
	}

	initClientCtx := client.Context{}.
		WithCodec(encodingConfig.Codec).
		WithInterfaceRegistry(encodingConfig.InterfaceRegistry).
		WithTxConfig(encodingConfig.TxConfig).
		WithLegacyAmino(encodingConfig.Amino).
		WithInput(os.Stdin).
		WithAccountRetriever(authtypes.AccountRetriever{}).
		WithHomeDir(constant.DefaultNodeHome).
		WithBroadcastMode(flags.FlagBroadcastMode).
		WithKeyringOptions(cosmosevmkeyring.Option()).
		WithLedgerHasProtobuf(true).
		WithViper("")

	rootCmd := &cobra.Command{
		Use:   "puniversald",
		Short: "Push Universal Client Daemon",
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			// set the default command outputs
			cmd.SetOut(cmd.OutOrStdout())
			cmd.SetErr(cmd.ErrOrStderr())

			initClientCtx = initClientCtx.WithCmdContext(cmd.Context())
			initClientCtx, err := client.ReadPersistentCommandFlags(initClientCtx, cmd.Flags())
			if err != nil {
				return err
			}

			initClientCtx, err = config.ReadFromClientConfig(initClientCtx)
			if err != nil {
				return err
			}

			if err := client.SetCmdClientContextHandler(initClientCtx, cmd); err != nil {
				return err
			}

			return nil
		},
	}

	InitRootCmd(rootCmd) // add subcommands like `start` and `version`

	return rootCmd
}

var tempDir = func() string {
	dir, err := os.MkdirTemp("", "puniversald")
	if err != nil {
		panic("failed to create temp dir: " + err.Error())
	}
	defer os.RemoveAll(dir)
	return dir
}
