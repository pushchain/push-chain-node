package pushsigner

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/x/authz"

	"github.com/pushchain/push-chain-node/universalClient/config"
	"github.com/pushchain/push-chain-node/universalClient/constant"
	"github.com/pushchain/push-chain-node/universalClient/pushcore"
	keysv2 "github.com/pushchain/push-chain-node/universalClient/pushsigner/keys"
	uetypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// GrantInfo represents information about a single AuthZ grant.
type GrantInfo struct {
	Granter     string
	MessageType string
	Expiration  *time.Time
}

// ValidationResult contains the validated hotkey information.
type ValidationResult struct {
	Keyring  keyring.Keyring
	KeyName  string
	KeyAddr  string
	Granter  string
	Messages []string
}

// ValidateKeysAndGrants validates hotkey and AuthZ grants against the specified granter.
func ValidateKeysAndGrants(cfg *config.Config, pushCore *pushcore.Client, granter string) (*ValidationResult, error) {
	interfaceRegistry := keysv2.CreateInterfaceRegistryWithEVMSupport()
	authz.RegisterInterfaces(interfaceRegistry)
	uetypes.RegisterInterfaces(interfaceRegistry)
	cdc := codec.NewProtoCodec(interfaceRegistry)

	// Prepare password reader for file backend
	var reader io.Reader = nil
	if cfg.KeyringBackend == config.KeyringBackendFile {
		if cfg.KeyringPassword == "" {
			return nil, fmt.Errorf("keyring_password is required for file backend")
		}
		// Keyring expects password twice, each followed by newline
		passwordInput := fmt.Sprintf("%s\n%s\n", cfg.KeyringPassword, cfg.KeyringPassword)
		reader = strings.NewReader(passwordInput)
	}

	kr, err := keysv2.CreateKeyringFromConfig(constant.DefaultNodeHome, reader, cfg.KeyringBackend)
	if err != nil {
		return nil, fmt.Errorf("failed to create keyring: %w", err)
	}

	keyInfos, err := kr.List()
	if err != nil {
		return nil, fmt.Errorf("failed to list keys: %w", err)
	}
	if len(keyInfos) == 0 {
		return nil, fmt.Errorf("no keys found in keyring")
	}

	keyInfo := keyInfos[0]
	keyAddr, err := keyInfo.GetAddress()
	if err != nil {
		return nil, fmt.Errorf("failed to get key address: %w", err)
	}
	keyAddrStr := keyAddr.String()

	grantResp, err := pushCore.GetGranteeGrants(keyAddrStr)
	if err != nil {
		return nil, fmt.Errorf("failed to query grants: %w", err)
	}

	grants := extractGrantInfo(grantResp, cdc)
	if len(grants) == 0 {
		return nil, fmt.Errorf("no AuthZ grants found for %s", keyAddrStr)
	}

	// Verify grants against the specified granter
	authorizedMsgs, err := VerifyGrants(grants, granter)
	if err != nil {
		return nil, err
	}

	return &ValidationResult{
		Keyring:  kr,
		KeyName:  keyInfo.Name,
		KeyAddr:  keyAddrStr,
		Granter:  granter,
		Messages: authorizedMsgs,
	}, nil
}

// VerifyGrants verifies that all required messages are authorized by the specified granter.
func VerifyGrants(grants []GrantInfo, granter string) ([]string, error) {
	now := time.Now()
	authorized := make(map[string]bool)

	for _, grant := range grants {
		// Skip expired grants
		if grant.Expiration != nil && grant.Expiration.Before(now) {
			continue
		}

		// Only consider grants from the specified granter
		if grant.Granter != granter {
			continue
		}

		// Check if this grant is for a required message type
		if slices.Contains(constant.RequiredMsgGrants, grant.MessageType) {
			authorized[grant.MessageType] = true
		}
	}

	// Verify all required grants are present
	var missing []string
	for _, req := range constant.RequiredMsgGrants {
		if !authorized[req] {
			missing = append(missing, req)
		}
	}

	if len(missing) > 0 {
		return nil, fmt.Errorf("missing grants from granter %s: %v", granter, missing)
	}

	// Return list of authorized message types
	msgs := make([]string, 0, len(authorized))
	for m := range authorized {
		msgs = append(msgs, m)
	}
	return msgs, nil
}

// extractGrantInfo extracts grant info from response.
func extractGrantInfo(resp *authz.QueryGranteeGrantsResponse, cdc *codec.ProtoCodec) []GrantInfo {
	var grants []GrantInfo
	for _, grant := range resp.Grants {
		if grant.Authorization == nil || grant.Authorization.TypeUrl != "/cosmos.authz.v1beta1.GenericAuthorization" {
			continue
		}
		msgType, err := extractMessageType(grant.Authorization, cdc)
		if err != nil {
			continue
		}
		grants = append(grants, GrantInfo{
			Granter:     grant.Granter,
			MessageType: msgType,
			Expiration:  grant.Expiration,
		})
	}
	return grants
}

// extractMessageType extracts message type from GenericAuthorization.
func extractMessageType(any *codectypes.Any, cdc *codec.ProtoCodec) (string, error) {
	var ga authz.GenericAuthorization
	if err := cdc.Unmarshal(any.Value, &ga); err != nil {
		return "", err
	}
	return ga.Msg, nil
}
