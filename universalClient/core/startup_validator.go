package core

import (
	"context"
	"fmt"

	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/x/authz"
	"github.com/pushchain/push-chain-node/universalClient/config"
	"github.com/pushchain/push-chain-node/universalClient/constant"
	grpcClient "github.com/pushchain/push-chain-node/universalClient/grpc"
	"github.com/pushchain/push-chain-node/universalClient/keys"
	uetypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	"github.com/rs/zerolog"
)

// StartupValidationResult contains the validated hotkey information
type StartupValidationResult struct {
	Keyring  keyring.Keyring
	KeyName  string
	KeyAddr  string
	Granter  string
	Messages []string // List of authorized message types
}

// StartupValidator validates startup requirements
type StartupValidator struct {
	log     zerolog.Logger
	config  *config.Config
	grpcURL string
	cdc     *codec.ProtoCodec // Cached codec
}

// NewStartupValidator creates a new startup validator
func NewStartupValidator(
	ctx context.Context,
	log zerolog.Logger,
	config *config.Config,
	grpcURL string,
) *StartupValidator {
	// Create codec once and cache it
	interfaceRegistry := keys.CreateInterfaceRegistryWithEVMSupport()
	authz.RegisterInterfaces(interfaceRegistry)
	uetypes.RegisterInterfaces(interfaceRegistry)

	return &StartupValidator{
		log:     log.With().Str("component", "startup_validator").Logger(),
		config:  config,
		grpcURL: grpcURL,
		cdc:     codec.NewProtoCodec(interfaceRegistry),
	}
}

// ValidateStartupRequirements validates hotkey and AuthZ permissions
func (sv *StartupValidator) ValidateStartupRequirements() (*StartupValidationResult, error) {
	sv.log.Info().Msg("🔍 Validating startup requirements")

	// Create keyring
	kr, err := keys.CreateKeyringFromConfig(constant.DefaultNodeHome, nil, sv.config.KeyringBackend)
	if err != nil {
		return nil, fmt.Errorf("failed to create keyring: %w", err)
	}

	// Get first available key
	keyInfos, err := kr.List()
	if err != nil {
		return nil, fmt.Errorf("failed to list keys: %w", err)
	}

	if len(keyInfos) == 0 {
		return nil, fmt.Errorf("no keys found in keyring. Please create a hotkey: puniversald keys add <keyname>")
	}

	// Use the first key found
	keyInfo := keyInfos[0]
	keyAddr, err := keyInfo.GetAddress()
	if err != nil {
		return nil, fmt.Errorf("failed to get key address: %w", err)
	}

	sv.log.Info().
		Str("key_name", keyInfo.Name).
		Str("key_address", keyAddr.String()).
		Msg("Using hotkey from keyring")

	// Validate AuthZ permissions using the grpc package
	granter, authorizedMsgs, err := grpcClient.QueryGrantsWithRetry(sv.grpcURL, keyAddr.String(), sv.cdc, sv.log)
	if err != nil {
		return nil, fmt.Errorf("AuthZ validation failed: %w", err)
	}

	sv.log.Info().
		Str("granter", granter).
		Int("authorized_count", len(authorizedMsgs)).
		Msg("✅ AuthZ permissions validated")

	return &StartupValidationResult{
		Keyring:  kr,
		KeyName:  keyInfo.Name,
		KeyAddr:  keyAddr.String(),
		Granter:  granter,
		Messages: authorizedMsgs,
	}, nil
}

