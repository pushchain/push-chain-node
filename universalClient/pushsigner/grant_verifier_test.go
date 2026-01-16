package pushsigner

import (
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/universalClient/constant"
)

func TestVerifyGrants(t *testing.T) {
	futureTime := time.Now().Add(24 * time.Hour)
	pastTime := time.Now().Add(-24 * time.Hour)
	granter := "push1granter123"

	t.Run("all required grants present and valid", func(t *testing.T) {
		grants := []grantInfo{
			{Granter: granter, MessageType: "/uexecutor.v1.MsgVoteInbound", Expiration: &futureTime},
			{Granter: granter, MessageType: "/uexecutor.v1.MsgVoteGasPrice", Expiration: &futureTime},
			{Granter: granter, MessageType: "/utss.v1.MsgVoteTssKeyProcess", Expiration: &futureTime},
		}

		msgs, err := verifyGrants(grants, granter)
		require.NoError(t, err)
		assert.Len(t, msgs, len(constant.RequiredMsgGrants))

		// Verify all required messages are returned
		for _, req := range constant.RequiredMsgGrants {
			assert.Contains(t, msgs, req)
		}
	})

	t.Run("grants with nil expiration are valid", func(t *testing.T) {
		grants := []grantInfo{
			{Granter: granter, MessageType: "/uexecutor.v1.MsgVoteInbound", Expiration: nil},
			{Granter: granter, MessageType: "/uexecutor.v1.MsgVoteGasPrice", Expiration: nil},
			{Granter: granter, MessageType: "/utss.v1.MsgVoteTssKeyProcess", Expiration: nil},
		}

		msgs, err := verifyGrants(grants, granter)
		require.NoError(t, err)
		assert.Len(t, msgs, len(constant.RequiredMsgGrants))
	})

	t.Run("missing required grant", func(t *testing.T) {
		grants := []grantInfo{
			{Granter: granter, MessageType: "/uexecutor.v1.MsgVoteInbound", Expiration: &futureTime},
			// Missing MsgVoteGasPrice and MsgVoteTssKeyProcess
		}

		msgs, err := verifyGrants(grants, granter)
		require.Error(t, err)
		assert.Nil(t, msgs)
		assert.Contains(t, err.Error(), "missing grants from granter")
	})

	t.Run("expired grants are ignored", func(t *testing.T) {
		grants := []grantInfo{
			{Granter: granter, MessageType: "/uexecutor.v1.MsgVoteInbound", Expiration: &pastTime}, // Expired
			{Granter: granter, MessageType: "/uexecutor.v1.MsgVoteGasPrice", Expiration: &futureTime},
			{Granter: granter, MessageType: "/utss.v1.MsgVoteTssKeyProcess", Expiration: &futureTime},
		}

		msgs, err := verifyGrants(grants, granter)
		require.Error(t, err)
		assert.Nil(t, msgs)
		assert.Contains(t, err.Error(), "missing grants")
		assert.Contains(t, err.Error(), "MsgVoteInbound")
	})

	t.Run("grants from wrong granter are ignored", func(t *testing.T) {
		wrongGranter := "push1wronggranter"
		grants := []grantInfo{
			{Granter: wrongGranter, MessageType: "/uexecutor.v1.MsgVoteInbound", Expiration: &futureTime},
			{Granter: granter, MessageType: "/uexecutor.v1.MsgVoteGasPrice", Expiration: &futureTime},
			{Granter: granter, MessageType: "/utss.v1.MsgVoteTssKeyProcess", Expiration: &futureTime},
		}

		msgs, err := verifyGrants(grants, granter)
		require.Error(t, err)
		assert.Nil(t, msgs)
		assert.Contains(t, err.Error(), "missing grants")
	})

	t.Run("empty grants list", func(t *testing.T) {
		grants := []grantInfo{}

		msgs, err := verifyGrants(grants, granter)
		require.Error(t, err)
		assert.Nil(t, msgs)
		assert.Contains(t, err.Error(), "missing grants")
	})

	t.Run("duplicate grants are handled correctly", func(t *testing.T) {
		grants := []grantInfo{
			{Granter: granter, MessageType: "/uexecutor.v1.MsgVoteInbound", Expiration: &futureTime},
			{Granter: granter, MessageType: "/uexecutor.v1.MsgVoteInbound", Expiration: &futureTime}, // Duplicate
			{Granter: granter, MessageType: "/uexecutor.v1.MsgVoteGasPrice", Expiration: &futureTime},
			{Granter: granter, MessageType: "/utss.v1.MsgVoteTssKeyProcess", Expiration: &futureTime},
		}

		msgs, err := verifyGrants(grants, granter)
		require.NoError(t, err)
		assert.Len(t, msgs, len(constant.RequiredMsgGrants))
	})

	t.Run("extra non-required grants are ignored", func(t *testing.T) {
		grants := []grantInfo{
			{Granter: granter, MessageType: "/uexecutor.v1.MsgVoteInbound", Expiration: &futureTime},
			{Granter: granter, MessageType: "/uexecutor.v1.MsgVoteGasPrice", Expiration: &futureTime},
			{Granter: granter, MessageType: "/utss.v1.MsgVoteTssKeyProcess", Expiration: &futureTime},
			{Granter: granter, MessageType: "/some.other.v1.MsgNotRequired", Expiration: &futureTime}, // Extra grant
		}

		msgs, err := verifyGrants(grants, granter)
		require.NoError(t, err)
		assert.Len(t, msgs, len(constant.RequiredMsgGrants))
		assert.NotContains(t, msgs, "/some.other.v1.MsgNotRequired")
	})
}

func TestExtractGrantInfo(t *testing.T) {
	interfaceRegistry := codectypes.NewInterfaceRegistry()
	authz.RegisterInterfaces(interfaceRegistry)
	cdc := codec.NewProtoCodec(interfaceRegistry)

	futureTime := time.Now().Add(24 * time.Hour)

	t.Run("extract valid generic authorization grants", func(t *testing.T) {
		ga := &authz.GenericAuthorization{Msg: "/uexecutor.v1.MsgVoteInbound"}
		gaAny, err := codectypes.NewAnyWithValue(ga)
		require.NoError(t, err)

		resp := &authz.QueryGranteeGrantsResponse{
			Grants: []*authz.GrantAuthorization{
				{
					Granter:       "push1granter123",
					Authorization: gaAny,
					Expiration:    &futureTime,
				},
			},
		}

		grants := extractGrantInfo(resp, cdc)
		require.Len(t, grants, 1)
		assert.Equal(t, "push1granter123", grants[0].Granter)
		assert.Equal(t, "/uexecutor.v1.MsgVoteInbound", grants[0].MessageType)
		assert.Equal(t, &futureTime, grants[0].Expiration)
	})

	t.Run("skip non-generic authorization types", func(t *testing.T) {
		// Create an Any with a different type URL
		wrongTypeAny := &codectypes.Any{
			TypeUrl: "/cosmos.authz.v1beta1.SendAuthorization",
			Value:   []byte{},
		}

		resp := &authz.QueryGranteeGrantsResponse{
			Grants: []*authz.GrantAuthorization{
				{
					Granter:       "push1granter123",
					Authorization: wrongTypeAny,
					Expiration:    &futureTime,
				},
			},
		}

		grants := extractGrantInfo(resp, cdc)
		assert.Len(t, grants, 0)
	})

	t.Run("skip grants with nil authorization", func(t *testing.T) {
		resp := &authz.QueryGranteeGrantsResponse{
			Grants: []*authz.GrantAuthorization{
				{
					Granter:       "push1granter123",
					Authorization: nil,
					Expiration:    &futureTime,
				},
			},
		}

		grants := extractGrantInfo(resp, cdc)
		assert.Len(t, grants, 0)
	})

	t.Run("empty grants response", func(t *testing.T) {
		resp := &authz.QueryGranteeGrantsResponse{
			Grants: []*authz.GrantAuthorization{},
		}

		grants := extractGrantInfo(resp, cdc)
		assert.Len(t, grants, 0)
	})

	t.Run("multiple valid grants", func(t *testing.T) {
		ga1 := &authz.GenericAuthorization{Msg: "/uexecutor.v1.MsgVoteInbound"}
		ga1Any, err := codectypes.NewAnyWithValue(ga1)
		require.NoError(t, err)

		ga2 := &authz.GenericAuthorization{Msg: "/uexecutor.v1.MsgVoteGasPrice"}
		ga2Any, err := codectypes.NewAnyWithValue(ga2)
		require.NoError(t, err)

		resp := &authz.QueryGranteeGrantsResponse{
			Grants: []*authz.GrantAuthorization{
				{
					Granter:       "push1granter123",
					Authorization: ga1Any,
					Expiration:    &futureTime,
				},
				{
					Granter:       "push1granter456",
					Authorization: ga2Any,
					Expiration:    nil,
				},
			},
		}

		grants := extractGrantInfo(resp, cdc)
		require.Len(t, grants, 2)
		assert.Equal(t, "/uexecutor.v1.MsgVoteInbound", grants[0].MessageType)
		assert.Equal(t, "/uexecutor.v1.MsgVoteGasPrice", grants[1].MessageType)
	})
}

func TestExtractMessageType(t *testing.T) {
	interfaceRegistry := codectypes.NewInterfaceRegistry()
	authz.RegisterInterfaces(interfaceRegistry)
	cdc := codec.NewProtoCodec(interfaceRegistry)

	t.Run("extract message type from valid generic authorization", func(t *testing.T) {
		ga := &authz.GenericAuthorization{Msg: "/uexecutor.v1.MsgVoteInbound"}
		gaAny, err := codectypes.NewAnyWithValue(ga)
		require.NoError(t, err)

		msgType, err := extractMessageType(gaAny, cdc)
		require.NoError(t, err)
		assert.Equal(t, "/uexecutor.v1.MsgVoteInbound", msgType)
	})

	t.Run("error on invalid proto data", func(t *testing.T) {
		invalidAny := &codectypes.Any{
			TypeUrl: "/cosmos.authz.v1beta1.GenericAuthorization",
			Value:   []byte("invalid proto data"),
		}

		msgType, err := extractMessageType(invalidAny, cdc)
		require.Error(t, err)
		assert.Equal(t, "", msgType)
	})

	t.Run("empty message type", func(t *testing.T) {
		ga := &authz.GenericAuthorization{Msg: ""}
		gaAny, err := codectypes.NewAnyWithValue(ga)
		require.NoError(t, err)

		msgType, err := extractMessageType(gaAny, cdc)
		require.NoError(t, err)
		assert.Equal(t, "", msgType)
	})
}

func TestGrantInfo(t *testing.T) {
	t.Run("grantInfo struct fields", func(t *testing.T) {
		exp := time.Now().Add(24 * time.Hour)
		grant := grantInfo{
			Granter:     "push1granter123",
			MessageType: "/uexecutor.v1.MsgVoteInbound",
			Expiration:  &exp,
		}

		assert.Equal(t, "push1granter123", grant.Granter)
		assert.Equal(t, "/uexecutor.v1.MsgVoteInbound", grant.MessageType)
		assert.NotNil(t, grant.Expiration)
	})

	t.Run("grantInfo with nil expiration", func(t *testing.T) {
		grant := grantInfo{
			Granter:     "push1granter123",
			MessageType: "/uexecutor.v1.MsgVoteInbound",
			Expiration:  nil,
		}

		assert.Nil(t, grant.Expiration)
	})
}

func TestValidationResult(t *testing.T) {
	t.Run("validationResult struct fields", func(t *testing.T) {
		result := validationResult{
			KeyName:  "test-key",
			KeyAddr:  "push1keyaddr123",
			Granter:  "push1granter123",
			Messages: []string{"/uexecutor.v1.MsgVoteInbound", "/uexecutor.v1.MsgVoteGasPrice"},
		}

		assert.Equal(t, "test-key", result.KeyName)
		assert.Equal(t, "push1keyaddr123", result.KeyAddr)
		assert.Equal(t, "push1granter123", result.Granter)
		assert.Len(t, result.Messages, 2)
	})
}
