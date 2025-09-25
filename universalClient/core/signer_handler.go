package core

import (
	"context"
	"fmt"

	rpchttp "github.com/cometbft/cometbft/rpc/client/http"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	"github.com/cosmos/cosmos-sdk/x/auth/tx"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types/v1beta1"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	uauthz "github.com/pushchain/push-chain-node/universalClient/authz"
	"github.com/pushchain/push-chain-node/universalClient/keys"
	"github.com/pushchain/push-chain-node/universalClient/utils"
	uetypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	"github.com/rs/zerolog"
)

// SignerHandler manages transaction signing for all chains
// It provides a unified interface for voting on any chain
type SignerHandler struct {
	txSigner TxSignerInterface
	keys     keys.UniversalValidatorKeys
	granter  string
	log      zerolog.Logger
}

// NewSignerHandler creates a new unified signer handler
func NewSignerHandler(
	ctx context.Context,
	log zerolog.Logger,
	validationResult *StartupValidationResult,
	grpcURL string,
) (*SignerHandler, error) {
	log.Info().Msg("Creating unified SignerHandler")

	// Parse the key address
	keyAddr, err := sdk.AccAddressFromBech32(validationResult.KeyAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse key address: %w", err)
	}

	// Create Keys instance with simplified validation result
	universalKeys := keys.NewKeysWithKeybase(
		validationResult.Keyring,
		keyAddr,
		validationResult.KeyName,
		"", // Password will be prompted if needed
	)

	// Create client context for AuthZ TxSigner
	clientCtx, err := createClientContext(validationResult.Keyring, grpcURL)
	if err != nil {
		return nil, fmt.Errorf("failed to create client context: %w", err)
	}

	// Create AuthZ TxSigner
	txSigner := uauthz.NewTxSigner(
		universalKeys,
		clientCtx,
		log,
	)

	log.Info().
		Str("key_name", validationResult.KeyName).
		Str("key_address", validationResult.KeyAddr).
		Str("granter", validationResult.Granter).
		Strs("authorized_messages", validationResult.Messages).
		Msg("✅ SignerHandler initialized successfully")

	return &SignerHandler{
		txSigner: txSigner,
		keys:     universalKeys,
		granter:  validationResult.Granter,
		log:      log.With().Str("component", "signer_handler").Logger(),
	}, nil
}

// GetKeys returns the universal validator keys
func (sh *SignerHandler) GetKeys() keys.UniversalValidatorKeys {
	return sh.keys
}

// GetGranter returns the granter address
func (sh *SignerHandler) GetGranter() string {
	return sh.granter
}

// GetTxSigner returns the underlying transaction signer
func (sh *SignerHandler) GetTxSigner() TxSignerInterface {
	return sh.txSigner
}

// SignAndBroadcast signs and broadcasts a transaction with the given messages
func (sh *SignerHandler) SignAndBroadcast(
	ctx context.Context,
	msgs []sdk.Msg,
	memo string,
	gasLimit uint64,
	feeAmount sdk.Coins,
) (*sdk.TxResponse, error) {
	return sh.txSigner.SignAndBroadcastAuthZTx(ctx, msgs, memo, gasLimit, feeAmount)
}

// createClientContext creates a client context for transaction signing
func createClientContext(kr keyring.Keyring, grpcURL string) (client.Context, error) {
	// Use the shared utility function which handles port defaults and TLS
	conn, err := utils.CreateGRPCConnection(grpcURL)
	if err != nil {
		return client.Context{}, err
	}

	// Setup codec
	interfaceRegistry := keys.CreateInterfaceRegistryWithEVMSupport()
	authz.RegisterInterfaces(interfaceRegistry)
	authtypes.RegisterInterfaces(interfaceRegistry)
	banktypes.RegisterInterfaces(interfaceRegistry)
	stakingtypes.RegisterInterfaces(interfaceRegistry)
	govtypes.RegisterInterfaces(interfaceRegistry)
	uetypes.RegisterInterfaces(interfaceRegistry)

	cdc := codec.NewProtoCodec(interfaceRegistry)
	txConfig := tx.NewTxConfig(cdc, []signing.SignMode{signing.SignMode_SIGN_MODE_DIRECT})

	// Create HTTP RPC client for broadcasting
	hostname, err := utils.ExtractHostnameFromURL(grpcURL)
	if err != nil {
		return client.Context{}, fmt.Errorf("failed to extract hostname from GRPC URL: %w", err)
	}

	rpcURL := fmt.Sprintf("http://%s:26657", hostname)
	httpClient, err := rpchttp.New(rpcURL, "/websocket")
	if err != nil {
		return client.Context{}, fmt.Errorf("failed to create RPC client: %w", err)
	}

	clientCtx := client.Context{}.
		WithCodec(cdc).
		WithInterfaceRegistry(interfaceRegistry).
		WithChainID("push_42101-1"). // Push Chain testnet chain ID
		WithKeyring(kr).
		WithGRPCClient(conn).
		WithTxConfig(txConfig).
		WithBroadcastMode("sync").
		WithClient(httpClient)

	return clientCtx, nil
}