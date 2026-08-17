package keysharegc

import (
	"context"
	"errors"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	utsstypes "github.com/pushchain/push-chain-node/x/utss/types"
)

const (
	pubkeyA = "0xAAA"
	pubkeyB = "0xBBB"
)

type mockStore struct {
	ids     []string
	deleted []string
	listErr error
	delErr  error
}

func (m *mockStore) List() ([]string, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.ids, nil
}

func (m *mockStore) Delete(id string) error {
	if m.delErr != nil {
		return m.delErr
	}
	m.deleted = append(m.deleted, id)
	return nil
}

type mockCore struct {
	current    *utsstypes.TssKey
	keys       []*utsstypes.TssKey
	pending    []*utsstypes.TssEvent
	migrations []*utsstypes.FundMigration

	currentErr, keysErr, pendingErr, migErr error
}

func (m *mockCore) GetCurrentKey(context.Context) (*utsstypes.TssKey, error) {
	return m.current, m.currentErr
}
func (m *mockCore) GetAllKeys(context.Context) ([]*utsstypes.TssKey, error) {
	return m.keys, m.keysErr
}
func (m *mockCore) GetPendingTssEvents(context.Context) ([]*utsstypes.TssEvent, error) {
	return m.pending, m.pendingErr
}
func (m *mockCore) GetPendingFundMigrations(context.Context) ([]*utsstypes.FundMigration, error) {
	return m.migrations, m.migErr
}

func key(id, pubkey string) *utsstypes.TssKey {
	return &utsstypes.TssKey{KeyId: id, TssPubkey: pubkey}
}

// baseCore: K1 and K2 share pubkeyA (quorum change / refresh); K0 is a rotated
// away key on pubkeyB. K2 is current.
func baseCore() *mockCore {
	return &mockCore{
		current: key("K2", pubkeyA),
		keys: []*utsstypes.TssKey{
			key("K0", pubkeyB),
			key("K1", pubkeyA),
			key("K2", pubkeyA),
		},
	}
}

func sweepWith(t *testing.T, store *mockStore, core *mockCore) *mockStore {
	t.Helper()
	NewSweeper(Config{Keyshares: store, PushCore: core, Logger: zerolog.Nop()}).sweep(context.Background())
	return store
}

func TestSweep_DeletesSupersededSamePubkeyShare(t *testing.T) {
	store := sweepWith(t, &mockStore{ids: []string{"K1", "K2"}}, baseCore())
	assert.Equal(t, []string{"K1"}, store.deleted)
}

func TestSweep_KeepsCurrentKey(t *testing.T) {
	store := sweepWith(t, &mockStore{ids: []string{"K1", "K2"}}, baseCore())
	assert.NotContains(t, store.deleted, "K2")
}

// A rotated-away key (different pubkey) may still be needed to sign fund
// migration out of the retired vault, so it must survive.
func TestSweep_KeepsRotatedAwayPubkeyShare(t *testing.T) {
	store := sweepWith(t, &mockStore{ids: []string{"K0", "K1", "K2"}}, baseCore())
	assert.NotContains(t, store.deleted, "K0")
	assert.Equal(t, []string{"K1"}, store.deleted)
}

func TestSweep_KeepsShareReferencedByPendingFundMigration(t *testing.T) {
	core := baseCore()
	core.migrations = []*utsstypes.FundMigration{{OldKeyId: "K1"}}
	store := sweepWith(t, &mockStore{ids: []string{"K1", "K2"}}, core)
	assert.Empty(t, store.deleted)
}

// A pending TSS process means an in-flight session may still load a predecessor
// share; a missing share is silently treated as "new party" during quorum change.
func TestSweep_SkipsWhileTssProcessPending(t *testing.T) {
	core := baseCore()
	core.pending = []*utsstypes.TssEvent{{}}
	store := sweepWith(t, &mockStore{ids: []string{"K1", "K2"}}, core)
	assert.Empty(t, store.deleted)
}

// A share the chain doesn't know about is never deleted.
func TestSweep_KeepsUnknownKeyID(t *testing.T) {
	store := sweepWith(t, &mockStore{ids: []string{"mystery", "K2"}}, baseCore())
	assert.Empty(t, store.deleted)
}

func TestSweep_FailsClosedOnRPCError(t *testing.T) {
	cases := map[string]func(*mockCore){
		"current key":     func(c *mockCore) { c.currentErr = errors.New("boom") },
		"key history":     func(c *mockCore) { c.keysErr = errors.New("boom") },
		"pending events":  func(c *mockCore) { c.pendingErr = errors.New("boom") },
		"fund migrations": func(c *mockCore) { c.migErr = errors.New("boom") },
	}
	for name, breakIt := range cases {
		t.Run(name, func(t *testing.T) {
			core := baseCore()
			breakIt(core)
			store := sweepWith(t, &mockStore{ids: []string{"K1", "K2"}}, core)
			assert.Empty(t, store.deleted, "must not delete on incomplete chain state")
		})
	}
}

func TestSweep_NoopWhenSingleOrNoShare(t *testing.T) {
	core := baseCore()
	core.currentErr = errors.New("should not be called")
	store := sweepWith(t, &mockStore{ids: []string{"K2"}}, core)
	assert.Empty(t, store.deleted)
}

func TestSweep_ContinuesAfterDeleteError(t *testing.T) {
	store := &mockStore{ids: []string{"K1", "K2"}, delErr: errors.New("disk error")}
	NewSweeper(Config{Keyshares: store, PushCore: baseCore(), Logger: zerolog.Nop()}).
		sweep(context.Background())
	assert.Empty(t, store.deleted)
}

func TestNewSweeper_DefaultInterval(t *testing.T) {
	s := NewSweeper(Config{Keyshares: &mockStore{}, PushCore: baseCore(), Logger: zerolog.Nop()})
	require.Equal(t, defaultCheckInterval, s.checkInterval)
}
