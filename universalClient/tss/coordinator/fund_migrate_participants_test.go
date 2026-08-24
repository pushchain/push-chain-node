package coordinator

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/universalClient/store"
	utsstypes "github.com/pushchain/push-chain-node/x/utss/types"
	"github.com/pushchain/push-chain-node/x/uvalidator/types"
)

func activeValidator(addr string) *types.UniversalValidator {
	return &types.UniversalValidator{
		IdentifyInfo:  &types.IdentityInfo{CoreValidatorAddress: addr},
		LifecycleInfo: &types.LifecycleInfo{CurrentStatus: types.UVStatus_UV_STATUS_ACTIVE},
	}
}

func validatorWithStatus(addr string, status types.UVStatus) *types.UniversalValidator {
	return &types.UniversalValidator{
		IdentifyInfo:  &types.IdentityInfo{CoreValidatorAddress: addr},
		LifecycleInfo: &types.LifecycleInfo{CurrentStatus: status},
	}
}

func validatorSet(addrs ...string) []*types.UniversalValidator {
	set := make([]*types.UniversalValidator, 0, len(addrs))
	for _, a := range addrs {
		set = append(set, activeValidator(a))
	}
	return set
}

func addressesOf(vs []*types.UniversalValidator) []string {
	addrs := make([]string, 0, len(vs))
	for _, v := range vs {
		addrs = append(addrs, v.IdentifyInfo.CoreValidatorAddress)
	}
	return addrs
}

func fundMigrateEvent(t *testing.T, oldKeyID string) store.Event {
	t.Helper()
	data, err := json.Marshal(utsstypes.FundMigrationInitiatedEventData{OldKeyID: oldKeyID})
	require.NoError(t, err)
	return store.Event{
		EventID:   "fm-1",
		Type:      store.EventTypeSignFundMigrate,
		EventData: data,
	}
}

func coordinatorWithKeys(keys map[string]*utsstypes.TssKey) *Coordinator {
	return &Coordinator{
		pushCore: &stalenessMockPushCore{keysByID: keys},
		logger:   zerolog.Nop(),
	}
}

// The finding's scenario: the old key has three shareholders, the validator set
// has since grown to ten. Selecting from the current set draws newcomers who
// hold no share of that key.
func TestFundMigrateParticipants_DrawsOnlyFromOldKeyShareholders(t *testing.T) {
	keys := map[string]*utsstypes.TssKey{
		"old-key": {KeyId: "old-key", Participants: []string{"v1", "v2", "v3"}},
	}
	c := coordinatorWithKeys(keys)

	all := validatorSet("v1", "v2", "v3", "v4", "v5", "v6", "v7", "v8", "v9", "v10")

	// Selection is randomised, so repeat to catch a newcomer slipping in.
	for i := 0; i < 200; i++ {
		got, err := c.fundMigrateParticipants(context.Background(), fundMigrateEvent(t, "old-key"), all)
		require.NoError(t, err)

		// Old key threshold is 3 of 3, not 7 of 10.
		require.Len(t, got, 3)
		assert.ElementsMatch(t, []string{"v1", "v2", "v3"}, addressesOf(got))
	}
}

// A subset of shareholders large enough to sign, alongside a much larger
// current set. Every signer must still be a shareholder.
func TestFundMigrateParticipants_UsesOldKeyThreshold(t *testing.T) {
	keys := map[string]*utsstypes.TssKey{
		"old-key": {KeyId: "old-key", Participants: []string{"v1", "v2", "v3", "v4", "v5", "v6"}},
	}
	c := coordinatorWithKeys(keys)

	all := validatorSet("v1", "v2", "v3", "v4", "v5", "v6", "n1", "n2", "n3", "n4", "n5")

	shareholders := map[string]bool{"v1": true, "v2": true, "v3": true, "v4": true, "v5": true, "v6": true}
	for i := 0; i < 200; i++ {
		got, err := c.fundMigrateParticipants(context.Background(), fundMigrateEvent(t, "old-key"), all)
		require.NoError(t, err)

		// CalculateThreshold(6) is 5, and it is the old key's size that decides.
		require.Len(t, got, CalculateThreshold(6))
		for _, addr := range addressesOf(got) {
			assert.True(t, shareholders[addr], "selected %s which holds no share of the old key", addr)
		}
	}
}

// Fail closed rather than hand back a set that cannot reach the old key's
// threshold. A short set would stall the session on an ACK that never arrives.
func TestFundMigrateParticipants_FailsWhenTooFewShareholdersRemain(t *testing.T) {
	keys := map[string]*utsstypes.TssKey{
		"old-key": {KeyId: "old-key", Participants: []string{"v1", "v2", "v3", "v4", "v5", "v6"}},
	}
	c := coordinatorWithKeys(keys)

	// Only 4 of the 6 shareholders remain, one short of the threshold of 5,
	// while the current set is comfortably large.
	all := validatorSet("v1", "v2", "v3", "v4", "n1", "n2", "n3", "n4", "n5", "n6")

	got, err := c.fundMigrateParticipants(context.Background(), fundMigrateEvent(t, "old-key"), all)
	require.Error(t, err)
	assert.Nil(t, got)
	assert.Contains(t, err.Error(), "only 4 are still eligible")
}

// Pending leave keeps signing; anything else is not a usable signer even when
// it holds a share.
func TestFundMigrateParticipants_ExcludesIneligibleShareholders(t *testing.T) {
	keys := map[string]*utsstypes.TssKey{
		"old-key": {KeyId: "old-key", Participants: []string{"v1", "v2", "v3"}},
	}
	c := coordinatorWithKeys(keys)

	all := []*types.UniversalValidator{
		validatorWithStatus("v1", types.UVStatus_UV_STATUS_ACTIVE),
		validatorWithStatus("v2", types.UVStatus_UV_STATUS_PENDING_LEAVE),
		validatorWithStatus("v3", types.UVStatus_UV_STATUS_ACTIVE),
	}

	got, err := c.fundMigrateParticipants(context.Background(), fundMigrateEvent(t, "old-key"), all)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"v1", "v2", "v3"}, addressesOf(got))

	// The same set with one shareholder no longer signing is one short.
	all[1] = validatorWithStatus("v2", types.UVStatus_UV_STATUS_INACTIVE)
	got, err = c.fundMigrateParticipants(context.Background(), fundMigrateEvent(t, "old-key"), all)
	require.Error(t, err)
	assert.Nil(t, got)
}

func TestFundMigrateParticipants_RejectsUnusableEventData(t *testing.T) {
	c := coordinatorWithKeys(map[string]*utsstypes.TssKey{
		"old-key": {KeyId: "old-key", Participants: []string{"v1", "v2", "v3"}},
	})
	all := validatorSet("v1", "v2", "v3")

	t.Run("malformed event data", func(t *testing.T) {
		event := store.Event{EventID: "fm-1", Type: store.EventTypeSignFundMigrate, EventData: []byte("not json")}
		_, err := c.fundMigrateParticipants(context.Background(), event, all)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "parse fund migration data")
	})

	t.Run("no old key id", func(t *testing.T) {
		_, err := c.fundMigrateParticipants(context.Background(), fundMigrateEvent(t, ""), all)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no old key id")
	})

	t.Run("unknown old key", func(t *testing.T) {
		_, err := c.fundMigrateParticipants(context.Background(), fundMigrateEvent(t, "missing-key"), all)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "records no participants")
	})

	t.Run("key with empty participants", func(t *testing.T) {
		c := coordinatorWithKeys(map[string]*utsstypes.TssKey{"old-key": {KeyId: "old-key"}})
		_, err := c.fundMigrateParticipants(context.Background(), fundMigrateEvent(t, "old-key"), all)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "records no participants")
	})

	t.Run("lookup failure", func(t *testing.T) {
		c := &Coordinator{
			pushCore: &stalenessMockPushCore{keyErr: fmt.Errorf("rpc down")},
			logger:   zerolog.Nop(),
		}
		_, err := c.fundMigrateParticipants(context.Background(), fundMigrateEvent(t, "old-key"), all)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "fetch old key")
	})
}

// A shareholder that has since dropped its identity record must not be counted
// towards the threshold, since it cannot be addressed as a party.
func TestFundMigrateParticipants_SkipsValidatorWithoutIdentity(t *testing.T) {
	c := coordinatorWithKeys(map[string]*utsstypes.TssKey{
		"old-key": {KeyId: "old-key", Participants: []string{"v1", "v2", "v3"}},
	})

	all := []*types.UniversalValidator{
		activeValidator("v1"),
		{LifecycleInfo: &types.LifecycleInfo{CurrentStatus: types.UVStatus_UV_STATUS_ACTIVE}},
		activeValidator("v3"),
	}

	_, err := c.fundMigrateParticipants(context.Background(), fundMigrateEvent(t, "old-key"), all)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "only 2 are still eligible")
}

// The routing itself: a fund migration must not be selected the way an
// outbound is, which is the defect this change fixes.
func TestSelectParticipants_RoutesFundMigrateToShareholders(t *testing.T) {
	c := coordinatorWithKeys(map[string]*utsstypes.TssKey{
		"old-key": {KeyId: "old-key", Participants: []string{"v1", "v2", "v3"}},
	})

	all := validatorSet("v1", "v2", "v3", "v4", "v5", "v6", "v7", "v8", "v9", "v10")

	t.Run("fund migrate is confined to the old key", func(t *testing.T) {
		for i := 0; i < 100; i++ {
			got, err := c.SelectParticipants(context.Background(), fundMigrateEvent(t, "old-key"), all)
			require.NoError(t, err)
			assert.ElementsMatch(t, []string{"v1", "v2", "v3"}, addressesOf(got))
		}
	})

	t.Run("outbound still uses the current set", func(t *testing.T) {
		event := store.Event{EventID: "ob-1", Type: store.EventTypeSignOutbound}
		got, err := c.SelectParticipants(context.Background(), event, all)
		require.NoError(t, err)
		assert.Len(t, got, CalculateThreshold(len(all)))
	})

	t.Run("fund migrate reports rather than returning a short set", func(t *testing.T) {
		_, err := c.SelectParticipants(context.Background(), fundMigrateEvent(t, "gone"), all)
		require.Error(t, err)
	})

	t.Run("other protocols take every eligible validator", func(t *testing.T) {
		event := store.Event{EventID: "kg-1", Type: store.EventTypeKeygen}
		got, err := c.SelectParticipants(context.Background(), event, all)
		require.NoError(t, err)
		assert.Len(t, got, len(all))
	})
}

// selectRandomSubset is what keeps the count tied to the old key rather than to
// the surviving holders.
func TestSelectRandomSubset(t *testing.T) {
	all := validatorSet("v1", "v2", "v3", "v4", "v5")

	assert.Nil(t, selectRandomSubset(nil, 3))
	assert.Nil(t, selectRandomSubset(all, 0))
	assert.Nil(t, selectRandomSubset(all, -1))
	assert.Len(t, selectRandomSubset(all, 5), 5)
	assert.Len(t, selectRandomSubset(all, 9), 5)

	// Picks vary across calls and never repeat a validator within one pick.
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		got := selectRandomSubset(all, 3)
		require.Len(t, got, 3)
		unique := map[string]bool{}
		for _, addr := range addressesOf(got) {
			assert.False(t, unique[addr], "duplicate %s in one selection", addr)
			unique[addr] = true
			seen[addr] = true
		}
	}
	assert.Len(t, seen, 5, "selection never reached some validators")
}
