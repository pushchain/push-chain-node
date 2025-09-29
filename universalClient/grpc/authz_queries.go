package grpc

import (
	"context"
	"fmt"
	"time"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	"github.com/pushchain/push-chain-node/universalClient/constant"
	"github.com/rs/zerolog"
)

// QueryGrantsWithRetry queries AuthZ grants for a grantee with retry logic
func QueryGrantsWithRetry(grpcURL, granteeAddr string, cdc *codec.ProtoCodec, log zerolog.Logger) (string, []string, error) {
	// Simple retry: 15s, then 30s
	timeouts := []time.Duration{15 * time.Second, 30 * time.Second}

	for attempt, timeout := range timeouts {
		conn, err := CreateGRPCConnection(grpcURL)
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
			return processGrants(grantResp, granteeAddr, cdc)
		}

		// On timeout, retry with longer timeout
		if ctx.Err() == context.DeadlineExceeded && attempt < len(timeouts)-1 {
			log.Warn().
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
func processGrants(grantResp *authz.QueryGranteeGrantsResponse, granteeAddr string, cdc *codec.ProtoCodec) (string, []string, error) {
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

		msgType, err := extractMessageType(grant.Authorization, cdc)
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
func extractMessageType(authzAny *codectypes.Any, cdc *codec.ProtoCodec) (string, error) {
	var genericAuth authz.GenericAuthorization
	if err := cdc.Unmarshal(authzAny.Value, &genericAuth); err != nil {
		return "", err
	}
	return genericAuth.Msg, nil
}