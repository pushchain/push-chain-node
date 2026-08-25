package sessionmanager

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/pushchain/push-chain-node/universalClient/tss/coordinator"
	utsstypes "github.com/pushchain/push-chain-node/x/utss/types"
	"github.com/pushchain/push-chain-node/x/uvalidator/types"
)

func fundMigrateStoreEvent(t *testing.T, oldKeyID string) *store.Event {
	t.Helper()
	data, err := json.Marshal(utsstypes.FundMigrationInitiatedEventData{OldKeyID: oldKeyID})
	require.NoError(t, err)
	return &store.Event{
		EventID:   "fm-e2e",
		Type:      store.EventTypeSignFundMigrate,
		EventData: data,
	}
}

func activeValidators(addrs ...string) []*types.UniversalValidator {
	set := make([]*types.UniversalValidator, 0, len(addrs))
	for _, a := range addrs {
		set = append(set, makeActiveValidator(a))
	}
	return set
}

func partyIDs(vs []*types.UniversalValidator) []string {
	ids := make([]string, 0, len(vs))
	for _, v := range vs {
		ids = append(ids, v.IdentifyInfo.CoreValidatorAddress)
	}
	return ids
}

// End to end across both components: the coordinator selects the participants,
// then a participant validates the setup message it receives.
//
// The two sides derive the answer independently, so a change to one that the
// other does not mirror leaves a selection the coordinator can legitimately
// make and every participant rejects. Neither side's own tests catch that.
func TestFundMigrate_CoordinatorSelectionPassesParticipantValidation(t *testing.T) {
	ctx := context.Background()

	// The finding's scenario: the old key has 3 shareholders and the validator
	// set has since grown to 10.
	oldKey := &utsstypes.TssKey{KeyId: "old-key", Participants: []string{"v1", "v2", "v3"}}

	sm, coord, _, _, _, _ := setupTestSessionManager(t)
	setCoordinatorPushCore(coord, &mockPushCore{
		keysByID: map[string]*utsstypes.TssKey{"old-key": oldKey},
	})
	setCoordinatorValidators(coord, activeValidators(
		"v1", "v2", "v3", "v4", "v5", "v6", "v7", "v8", "v9", "v10"))

	event := fundMigrateStoreEvent(t, "old-key")

	// Selection is randomised, so repeat rather than trusting one draw.
	for i := 0; i < 100; i++ {
		selected, err := coord.SelectParticipants(ctx, *event, coord.Validators())
		require.NoError(t, err, "coordinator could not select signers")

		ids := partyIDs(selected)
		assert.ElementsMatch(t, []string{"v1", "v2", "v3"}, ids,
			"coordinator selected a validator that holds no share of the old key")

		require.NoError(t, sm.validateParticipants(ctx, ids, event),
			"participant rejected a selection the coordinator legitimately made")
	}
}

// The same round trip for an outbound, which must keep using the current
// validator set on both sides.
func TestSignOutbound_CoordinatorSelectionPassesParticipantValidation(t *testing.T) {
	ctx := context.Background()

	sm, coord, _, _, _, _ := setupTestSessionManager(t)
	all := activeValidators("v1", "v2", "v3", "v4", "v5", "v6", "v7", "v8", "v9", "v10")
	setCoordinatorValidators(coord, all)

	event := &store.Event{EventID: "ob-e2e", Type: store.EventTypeSignOutbound}

	for i := 0; i < 100; i++ {
		selected, err := coord.SelectParticipants(ctx, *event, coord.Validators())
		require.NoError(t, err)

		ids := partyIDs(selected)
		require.Len(t, ids, coordinator.CalculateThreshold(len(all)))
		require.NoError(t, sm.validateParticipants(ctx, ids, event))
	}
}

// Validation must be tied to the old key, not merely lenient. A set that meets
// the count but contains a validator holding no share is still rejected.
func TestFundMigrate_ValidationRejectsNonShareholders(t *testing.T) {
	ctx := context.Background()

	sm, coord, _, _, _, _ := setupTestSessionManager(t)
	setCoordinatorPushCore(coord, &mockPushCore{
		keysByID: map[string]*utsstypes.TssKey{
			"old-key": {KeyId: "old-key", Participants: []string{"v1", "v2", "v3"}},
		},
	})
	setCoordinatorValidators(coord, activeValidators(
		"v1", "v2", "v3", "v4", "v5", "v6", "v7", "v8", "v9", "v10"))

	event := fundMigrateStoreEvent(t, "old-key")

	t.Run("newcomer in an otherwise valid set", func(t *testing.T) {
		err := sm.validateParticipants(ctx, []string{"v1", "v2", "v10"}, event)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "v10")
	})

	t.Run("all newcomers, count satisfied", func(t *testing.T) {
		err := sm.validateParticipants(ctx, []string{"v8", "v9", "v10"}, event)
		require.Error(t, err)
	})

	t.Run("below the old key threshold", func(t *testing.T) {
		err := sm.validateParticipants(ctx, []string{"v1", "v2"}, event)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "below required threshold 3")
	})

	t.Run("exactly the shareholders is accepted", func(t *testing.T) {
		require.NoError(t, sm.validateParticipants(ctx, []string{"v1", "v2", "v3"}, event))
	})
}

// A larger old key, so the accepted count is a strict subset of shareholders
// rather than all of them, and the current set is not what sizes it.
func TestFundMigrate_ValidationUsesOldKeyThresholdNotCurrentSet(t *testing.T) {
	ctx := context.Background()

	sm, coord, _, _, _, _ := setupTestSessionManager(t)
	setCoordinatorPushCore(coord, &mockPushCore{
		keysByID: map[string]*utsstypes.TssKey{
			"old-key": {KeyId: "old-key", Participants: []string{"v1", "v2", "v3", "v4", "v5", "v6"}},
		},
	})
	// 6 shareholders among 12 validators. Old key threshold is 5, the current
	// set's would be 9, which no set of shareholders could ever satisfy.
	setCoordinatorValidators(coord, activeValidators(
		"v1", "v2", "v3", "v4", "v5", "v6", "n1", "n2", "n3", "n4", "n5", "n6"))

	event := fundMigrateStoreEvent(t, "old-key")

	require.Equal(t, 5, coordinator.CalculateThreshold(6))
	require.Equal(t, 9, coordinator.CalculateThreshold(12))

	t.Run("old key threshold is accepted", func(t *testing.T) {
		require.NoError(t, sm.validateParticipants(ctx, []string{"v1", "v2", "v3", "v4", "v5"}, event))
	})

	t.Run("all shareholders is accepted", func(t *testing.T) {
		require.NoError(t, sm.validateParticipants(ctx, []string{"v1", "v2", "v3", "v4", "v5", "v6"}, event))
	})

	t.Run("one below the old key threshold is rejected", func(t *testing.T) {
		err := sm.validateParticipants(ctx, []string{"v1", "v2", "v3", "v4"}, event)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "below required threshold 5")
	})
}

// Some shareholders are gone but enough remain to sign. The threshold must
// still be the old key's, not one derived from the survivors: deriving it from
// the survivors lowers the bar every time a shareholder drops out, so a
// coordinator could open a session below the quorum the key was created under.
func TestFundMigrate_ValidationThresholdDoesNotShrinkWithSurvivors(t *testing.T) {
	ctx := context.Background()

	sm, coord, _, _, _, _ := setupTestSessionManager(t)
	setCoordinatorPushCore(coord, &mockPushCore{
		keysByID: map[string]*utsstypes.TssKey{
			"old-key": {KeyId: "old-key", Participants: []string{"v1", "v2", "v3", "v4", "v5", "v6"}},
		},
	})
	// v6 is gone, so 5 of the 6 shareholders survive. The old key still requires
	// 5, while a threshold over the survivors would be only 4.
	setCoordinatorValidators(coord, activeValidators("v1", "v2", "v3", "v4", "v5", "n1", "n2", "n3"))

	event := fundMigrateStoreEvent(t, "old-key")

	require.Equal(t, 5, coordinator.CalculateThreshold(6), "old key threshold")
	require.Equal(t, 4, coordinator.CalculateThreshold(5), "threshold over survivors")

	t.Run("four survivors is below the old key threshold", func(t *testing.T) {
		err := sm.validateParticipants(ctx, []string{"v1", "v2", "v3", "v4"}, event)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "below required threshold 5")
	})

	t.Run("all five survivors is accepted", func(t *testing.T) {
		require.NoError(t, sm.validateParticipants(ctx, []string{"v1", "v2", "v3", "v4", "v5"}, event))
	})

	t.Run("the coordinator selects exactly those five", func(t *testing.T) {
		selected, err := coord.SelectParticipants(ctx, *event, coord.Validators())
		require.NoError(t, err)
		ids := partyIDs(selected)
		assert.ElementsMatch(t, []string{"v1", "v2", "v3", "v4", "v5"}, ids)
		require.NoError(t, sm.validateParticipants(ctx, ids, event))
	})
}

// Too few shareholders left to sign at all. Both sides must refuse, and the
// coordinator must not dispatch a set it knows cannot reach quorum.
func TestFundMigrate_BothSidesFailClosedWhenShareholdersGone(t *testing.T) {
	ctx := context.Background()

	sm, coord, _, _, _, _ := setupTestSessionManager(t)
	setCoordinatorPushCore(coord, &mockPushCore{
		keysByID: map[string]*utsstypes.TssKey{
			"old-key": {KeyId: "old-key", Participants: []string{"v1", "v2", "v3", "v4", "v5", "v6"}},
		},
	})
	// Only 4 of the 6 shareholders remain, one short of the threshold of 5.
	setCoordinatorValidators(coord, activeValidators("v1", "v2", "v3", "v4", "n1", "n2", "n3", "n4"))

	event := fundMigrateStoreEvent(t, "old-key")

	_, err := coord.SelectParticipants(ctx, *event, coord.Validators())
	require.Error(t, err, "coordinator dispatched a set that cannot reach quorum")

	err = sm.validateParticipants(ctx, []string{"v1", "v2", "v3", "v4"}, event)
	require.Error(t, err)
}

// Validation must not fall open when the old key cannot be resolved.
func TestFundMigrate_ValidationFailsClosedOnUnresolvableKey(t *testing.T) {
	ctx := context.Background()

	sm, coord, _, _, _, _ := setupTestSessionManager(t)
	setCoordinatorValidators(coord, activeValidators("v1", "v2", "v3", "v4", "v5"))

	// The default mock returns a key with no participants for any id.
	setCoordinatorPushCore(coord, &mockPushCore{})

	err := sm.validateParticipants(ctx, []string{"v1", "v2", "v3", "v4"}, fundMigrateStoreEvent(t, "old-key"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolve fund migration signers")

	t.Run("malformed event data", func(t *testing.T) {
		event := &store.Event{
			EventID:   "fm-bad",
			Type:      store.EventTypeSignFundMigrate,
			EventData: []byte("not json"),
		}
		err := sm.validateParticipants(ctx, []string{"v1", "v2", "v3", "v4"}, event)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "resolve fund migration signers")
	})
}
