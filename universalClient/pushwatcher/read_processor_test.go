package pushwatcher

import (
	"context"
	"fmt"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/universalClient/externalchains/common"
	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/pushchain/push-chain-node/universalClient/uread"
)

type fakeReadVoter struct {
	votes  map[string]*uread.ReadResult
	txHash string
	err    error
}

func (f *fakeReadVoter) VoteReadResult(ctx context.Context, requestID string, result *uread.ReadResult) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	if f.votes == nil {
		f.votes = make(map[string]*uread.ReadResult)
	}
	f.votes[requestID] = result
	return f.txHash, nil
}

type fakeDestClient struct {
	result *uread.ReadResult
	err    error
}

func (f *fakeDestClient) Start(ctx context.Context) error { return nil }
func (f *fakeDestClient) Stop() error                     { return nil }
func (f *fakeDestClient) IsHealthy() bool                 { return true }
func (f *fakeDestClient) GetTxBuilder() (common.TxBuilder, error) {
	return nil, fmt.Errorf("not supported")
}
func (f *fakeDestClient) GetReadRequestHandler() (common.ReadRequestHandler, error) {
	return f, nil
}
func (f *fakeDestClient) ExecuteRead(ctx context.Context, req *uread.ReadRequest) (*uread.ReadResult, error) {
	return f.result, f.err
}

type fakeChainResolver struct {
	client common.ChainClient
}

func (f *fakeChainResolver) GetClient(chainID string) (common.ChainClient, error) {
	if f.client == nil {
		return nil, fmt.Errorf("no client for %s", chainID)
	}
	return f.client, nil
}

func testReadRequest() *uread.ReadRequest {
	return &uread.ReadRequest{
		RequestID:              "0xabc123",
		DestinationChain:       "eip155:11155111",
		Query:                  []byte{0x01},
		MinConfirmations:       1,
		DestinationBlockHeight: 100,
		CreatedAtHeight:        7,
	}
}

func newTestReadProcessor(t *testing.T, voter readVoter, destClient common.ChainClient) (*ReadProcessor, *common.ChainStore) {
	t.Helper()
	database := newTestDB(t)
	p, err := NewReadProcessor(voter, &fakeChainResolver{client: destClient}, database, 0, zerolog.Nop())
	require.NoError(t, err)
	return p, common.NewChainStore(database)
}

func seedReadRequest(t *testing.T, cs *common.ChainStore, req *uread.ReadRequest) string {
	t.Helper()
	event, err := convertReadRequestEvent(req)
	require.NoError(t, err)
	stored, err := cs.InsertEventIfNotExists(event)
	require.NoError(t, err)
	require.True(t, stored)
	return event.EventID
}

func eventStatus(t *testing.T, cs *common.ChainStore, eventID string) string {
	t.Helper()
	events, err := cs.GetConfirmedEvents(100)
	require.NoError(t, err)
	for i := range events {
		if events[i].EventID == eventID {
			return events[i].Status
		}
	}
	// not CONFIRMED anymore; caller asserts via CAS probes
	return ""
}

func TestReadProcessor_SuccessFlow(t *testing.T) {
	req := testReadRequest()
	result := &uread.ReadResult{
		Status:              uread.ReadStatusSuccess,
		ResultData:          []byte{0xaa},
		ObservedBlockHeight: 100,
	}
	voter := &fakeReadVoter{txHash: "VOTE_TX"}
	p, cs := newTestReadProcessor(t, voter, &fakeDestClient{result: result})
	eventID := seedReadRequest(t, cs, req)

	p.processConfirmedReads(context.Background())

	require.Contains(t, voter.votes, req.RequestID)
	assert.Equal(t, result, voter.votes[req.RequestID])

	// event flipped to COMPLETED with vote tx hash
	rows, err := cs.UpdateEventStatus(eventID, store.StatusCompleted, store.StatusCompleted)
	require.NoError(t, err)
	assert.Equal(t, int64(1), rows)

	// second tick must not re-vote
	voter.votes = nil
	p.processConfirmedReads(context.Background())
	assert.Empty(t, voter.votes)
}

func TestReadProcessor_VoteFailureKeepsConfirmed(t *testing.T) {
	req := testReadRequest()
	voter := &fakeReadVoter{err: fmt.Errorf("MsgVoteReadResult not available")}
	p, cs := newTestReadProcessor(t, voter, &fakeDestClient{result: &uread.ReadResult{Status: uread.ReadStatusSuccess}})
	eventID := seedReadRequest(t, cs, req)

	p.processConfirmedReads(context.Background())

	assert.Equal(t, store.StatusConfirmed, eventStatus(t, cs, eventID))
}

func TestReadProcessor_ExecutionFailureRetries(t *testing.T) {
	req := testReadRequest()
	voter := &fakeReadVoter{txHash: "VOTE_TX"}
	p, cs := newTestReadProcessor(t, voter, &fakeDestClient{err: fmt.Errorf("rpc down")})
	eventID := seedReadRequest(t, cs, req)

	p.processConfirmedReads(context.Background())

	assert.Empty(t, voter.votes)
	assert.Equal(t, store.StatusConfirmed, eventStatus(t, cs, eventID))
}

func TestReadProcessor_UnservedChainRetries(t *testing.T) {
	req := testReadRequest()
	voter := &fakeReadVoter{txHash: "VOTE_TX"}
	p, cs := newTestReadProcessor(t, voter, nil)
	eventID := seedReadRequest(t, cs, req)

	p.processConfirmedReads(context.Background())

	assert.Empty(t, voter.votes)
	assert.Equal(t, store.StatusConfirmed, eventStatus(t, cs, eventID))
}

func TestReadProcessor_CorruptEventReverted(t *testing.T) {
	voter := &fakeReadVoter{txHash: "VOTE_TX"}
	p, cs := newTestReadProcessor(t, voter, &fakeDestClient{result: &uread.ReadResult{Status: uread.ReadStatusSuccess}})

	stored, err := cs.InsertEventIfNotExists(&store.Event{
		EventID:          "corrupt-read",
		Type:             store.EventTypeReadRequest,
		ConfirmationType: store.ConfirmationInstant,
		Status:           store.StatusConfirmed,
		EventData:        []byte("not json"),
	})
	require.NoError(t, err)
	require.True(t, stored)

	p.processConfirmedReads(context.Background())

	assert.Empty(t, voter.votes)
	rows, err := cs.UpdateEventStatus("corrupt-read", store.StatusReverted, store.StatusReverted)
	require.NoError(t, err)
	assert.Equal(t, int64(1), rows)
}

func TestReadProcessor_IgnoresOtherEventTypes(t *testing.T) {
	voter := &fakeReadVoter{txHash: "VOTE_TX"}
	p, cs := newTestReadProcessor(t, voter, &fakeDestClient{result: &uread.ReadResult{Status: uread.ReadStatusSuccess}})

	stored, err := cs.InsertEventIfNotExists(&store.Event{
		EventID:          "tss-event",
		Type:             store.EventTypeKeygen,
		ConfirmationType: store.ConfirmationInstant,
		Status:           store.StatusConfirmed,
		EventData:        []byte("{}"),
	})
	require.NoError(t, err)
	require.True(t, stored)

	p.processConfirmedReads(context.Background())

	assert.Empty(t, voter.votes)
	assert.Equal(t, store.StatusConfirmed, eventStatus(t, cs, "tss-event"))
}

func TestReadProcessor_StartStop(t *testing.T) {
	p, _ := newTestReadProcessor(t, &fakeReadVoter{}, nil)

	require.NoError(t, p.Start(context.Background()))
	assert.Equal(t, ErrAlreadyRunning, p.Start(context.Background()))
	require.NoError(t, p.Stop())
	assert.Equal(t, ErrNotRunning, p.Stop())
}
