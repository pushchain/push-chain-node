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
	"github.com/pushchain/push-chain-node/universalClient/utils"
	uetypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	"github.com/rs/zerolog"
	"google.golang.org/grpc"
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

	// Validate AuthZ permissions (single gRPC call)
	granter, authorizedMsgs, err := sv.validateAuthZPermissions(keyAddr.String())
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

// validateAuthZPermissions checks if the key has required AuthZ grants
func (sv *StartupValidator) validateAuthZPermissions(granteeAddr string) (string, []string, error) {
	// Simple retry: 15s, then 30s
	timeouts := []time.Duration{15 * time.Second, 30 * time.Second}

	for attempt, timeout := range timeouts {
		conn, err := sv.createGRPCConnection()
		if err != nil {
			return "", nil, err
		}
		defer conn.Close()

		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()

		// Single gRPC call to get all grants
		authzClient := authz.NewQueryClient(conn)
		grantResp, err := authzClient.GranteeGrants(ctx, &authz.QueryGranteeGrantsRequest{
			Grantee: granteeAddr,
		})

		if err == nil {
			// Process the grants
			return sv.processGrants(grantResp, granteeAddr)
		}

		// On timeout, retry with longer timeout
		if ctx.Err() == context.DeadlineExceeded && attempt < len(timeouts)-1 {
			sv.log.Warn().
				Int("attempt", attempt+1).
				Dur("timeout", timeout).
				Msg("Timeout querying grants, retrying...")
			time.Sleep(2 * time.Second)
			continue
		}

		return "", nil, fmt.Errorf("failed to query grants: %w", err)
	}

	return "", nil, fmt.Errorf("failed after all retries")
}

// processGrants processes the AuthZ grant response
func (sv *StartupValidator) processGrants(grantResp *authz.QueryGranteeGrantsResponse, granteeAddr string) (string, []string, error) {
	if len(grantResp.Grants) == 0 {
		return "", nil, fmt.Errorf("no AuthZ grants found. Please grant permissions:\npuniversald tx authz grant %s generic --msg-type=/uexecutor.v1.MsgVoteInbound --from <granter>", granteeAddr)
	}

	authorizedMessages := make(map[string]string) // msgType -> granter
	var granter string

	// Check each grant for our required message types
	for _, grant := range grantResp.Grants {
		if grant.Authorization == nil {
			continue
		}

		// Only process GenericAuthorization
		if grant.Authorization.TypeUrl != "/cosmos.authz.v1beta1.GenericAuthorization" {
			continue
		}

		msgType, err := sv.extractMessageType(grant.Authorization)
		if err != nil {
			continue // Skip if we can't extract the message type
		}

		// Check if this is a required message
		for _, requiredMsg := range constant.SupportedMessages {
			if msgType == requiredMsg {
				// Check if grant is not expired
				if grant.Expiration != nil && grant.Expiration.Before(time.Now()) {
					continue // Skip expired grants
				}

				authorizedMessages[msgType] = grant.Granter
				if granter == "" {
					granter = grant.Granter
				}
				break
			}
		}
	}

	// Check if all required messages are authorized
	var missingMessages []string
	for _, requiredMsg := range constant.SupportedMessages {
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

// extractMessageType extracts the message type from a GenericAuthorization
func (sv *StartupValidator) extractMessageType(authzAny *codectypes.Any) (string, error) {
	var genericAuth authz.GenericAuthorization
	if err := sv.cdc.Unmarshal(authzAny.Value, &genericAuth); err != nil {
		return "", err
	}
	return genericAuth.Msg, nil
}

// createGRPCConnection creates a gRPC connection using the shared utility
func (sv *StartupValidator) createGRPCConnection() (*grpc.ClientConn, error) {
	// Simply delegate to the shared utility function which handles all the logic
	return utils.CreateGRPCConnection(sv.grpcURL)
}