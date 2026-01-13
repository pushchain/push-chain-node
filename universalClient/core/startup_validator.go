package core

import (
	"context"
	"fmt"
	"time"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/x/authz"
	"github.com/pushchain/push-chain-node/universalClient/config"
	"github.com/pushchain/push-chain-node/universalClient/constant"
	"github.com/pushchain/push-chain-node/universalClient/keys"
	"github.com/pushchain/push-chain-node/universalClient/pushcore"
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

// GrantInfo represents information about a single AuthZ grant.
type GrantInfo struct {
	Granter     string     // Address of the granter
	MessageType string     // Authorized message type
	Expiration  *time.Time // Grant expiration time (nil if no expiration)
}

// StartupValidator validates startup requirements
type StartupValidator struct {
	log      zerolog.Logger
	config   *config.Config
	pushCore *pushcore.Client
	cdc      *codec.ProtoCodec // Cached codec
}

// NewStartupValidator creates a new startup validator
func NewStartupValidator(
	ctx context.Context,
	log zerolog.Logger,
	config *config.Config,
	pushCore *pushcore.Client,
) *StartupValidator {
	// Create codec once and cache it
	interfaceRegistry := keys.CreateInterfaceRegistryWithEVMSupport()
	authz.RegisterInterfaces(interfaceRegistry)
	uetypes.RegisterInterfaces(interfaceRegistry)

	return &StartupValidator{
		log:      log.With().Str("component", "startup_validator").Logger(),
		config:   config,
		pushCore: pushCore,
		cdc:      codec.NewProtoCodec(interfaceRegistry),
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

	// Query AuthZ grants using pushcore (returns raw response)
	grantResp, err := sv.pushCore.GetGranteeGrants(keyAddr.String())
	if err != nil {
		return nil, fmt.Errorf("failed to query AuthZ grants: %w", err)
	}

	// Extract grant information from the raw response
	grants := extractGrantInfo(grantResp, sv.cdc)

	if len(grants) == 0 {
		return nil, fmt.Errorf("no AuthZ grants found. Please grant permissions:\npuniversald tx authz grant %s generic --msg-type=/uexecutor.v1.MsgVoteInbound --from <granter>", keyAddr.String())
	}

	// Validate that all required message types are authorized
	granter, authorizedMsgs, err := sv.validateGrants(grants, keyAddr.String())
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

// validateGrants validates that all required message types are present in the grants.
// It filters out expired grants and checks that all required messages are authorized.
func (sv *StartupValidator) validateGrants(grants []GrantInfo, granteeAddr string) (string, []string, error) {
	now := time.Now()
	authorizedMessages := make(map[string]string) // msgType -> granter
	var granter string

	// Process grants and filter out expired ones
	for _, grant := range grants {
		// Skip expired grants
		if grant.Expiration != nil && grant.Expiration.Before(now) {
			continue
		}

		// Check if this is a required message
		for _, requiredMsg := range constant.RequiredMsgGrants {
			if grant.MessageType == requiredMsg {
				authorizedMessages[grant.MessageType] = grant.Granter
				if granter == "" {
					granter = grant.Granter
				}
				break
			}
		}
	}

	// Check if all required messages are authorized
	var missingMessages []string
	for _, requiredMsg := range constant.RequiredMsgGrants {
		if _, ok := authorizedMessages[requiredMsg]; !ok {
			missingMessages = append(missingMessages, requiredMsg)
		}
	}

	if len(missingMessages) > 0 {
		return "", nil, fmt.Errorf("missing AuthZ grants for: %v\nGrant permissions using:\npuniversald tx authz grant %s generic --msg-type=<message_type> --from <granter>", missingMessages, granteeAddr)
	}

	// Return authorized messages
	authorizedList := make([]string, 0, len(authorizedMessages))
	for msgType := range authorizedMessages {
		authorizedList = append(authorizedList, msgType)
	}

	return granter, authorizedList, nil
}

// extractGrantInfo extracts grant information from the AuthZ grant response.
// It only processes GenericAuthorization grants and returns their message types.
func extractGrantInfo(grantResp *authz.QueryGranteeGrantsResponse, cdc *codec.ProtoCodec) []GrantInfo {
	var grants []GrantInfo

	for _, grant := range grantResp.Grants {
		if grant.Authorization == nil {
			continue
		}

		// Only process GenericAuthorization
		if grant.Authorization.TypeUrl != "/cosmos.authz.v1beta1.GenericAuthorization" {
			continue
		}

		msgType, err := extractMessageType(grant.Authorization, cdc)
		if err != nil {
			continue // Skip if we can't extract the message type
		}

		// grant.Expiration is already *time.Time, so we can use it directly
		expiration := grant.Expiration

		grants = append(grants, GrantInfo{
			Granter:     grant.Granter,
			MessageType: msgType,
			Expiration:  expiration,
		})
	}

	return grants
}

// extractMessageType extracts the message type from a GenericAuthorization protobuf Any.
func extractMessageType(authzAny *codectypes.Any, cdc *codec.ProtoCodec) (string, error) {
	var genericAuth authz.GenericAuthorization
	if err := cdc.Unmarshal(authzAny.Value, &genericAuth); err != nil {
		return "", err
	}
	return genericAuth.Msg, nil
}
