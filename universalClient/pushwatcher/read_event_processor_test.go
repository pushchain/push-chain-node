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
	result     *uread.ReadResult
	err        error
	notStarted bool
}

func (f *fakeDestClient) Start(ctx context.Context) error { return nil }
func (f *fakeDestClient) Stop() error                     { return nil }
func (f *fakeDestClient) IsHealthy() bool                 { return true }
func (f *fakeDestClient) GetTxBuilder() (common.TxBuilder, error) {
	return nil, fmt.Errorf("not supported")
}
func (f *fakeDestClient) GetReadRequestHandler() (common.ReadRequestHandler, error) {
	if f.notStarted {
		return nil, fmt.Errorf("client not started")
	}
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

func newTestReadEventProcessor(t *testing.T, voter readVoter, destClient common.ChainClient) (*ReadEventProcessor, *common.ChainStore) {
	t.Helper()
	database := newTestDB(t)
	p, err := NewReadEventProcessor(voter, &fakeChainResolver{client: destClient}, database, zerolog.Nop())
	require.NoError(t, err)
	return p, common.NewChainStore(database)
}

func seedReadRequest(t *testing.T, cs *common.ChainStore, req *uread.ReadRequest) *store.Event {
	t.Helper()
	event, err := convertReadRequestEvent(req)
	require.NoError(t, err)
	stored, err := cs.InsertEventIfNotExists(event)
	require.NoError(t, err)
	require.True(t, stored)
	return event
}

func assertStatus(t *testing.T, cs *common.ChainStore, eventID, status string) {
	t.Helper()
	rows, err := cs.UpdateEventStatus(eventID, status, status)
	require.NoError(t, err)
	assert.Equal(t, int64(1), rows, "event %s not in status %s", eventID, status)
}

func TestReadEventProcessor_SuccessFlow(t *testing.T) {
	req := testReadRequest()
	result := &uread.ReadResult{
		Status:              uread.ReadStatusSuccess,
		ResultData:          []byte{0xaa},
		ObservedBlockHeight: 100,
	}
	voter := &fakeReadVoter{txHash: "VOTE_TX"}
	p, cs := newTestReadEventProcessor(t, voter, &fakeDestClient{result: result})
	event := seedReadRequest(t, cs, req)

	require.NoError(t, p.HandleEvent(context.Background(), event))

	require.Contains(t, voter.votes, req.RequestID)
	assert.Equal(t, result, voter.votes[req.RequestID])
	assertStatus(t, cs, event.EventID, store.StatusCompleted)
}

func TestReadEventProcessor_VoteFailureKeepsConfirmed(t *testing.T) {
	req := testReadRequest()
	voter := &fakeReadVoter{err: fmt.Errorf("MsgVoteReadResult not available")}
	p, cs := newTestReadEventProcessor(t, voter, &fakeDestClient{result: &uread.ReadResult{Status: uread.ReadStatusSuccess}})
	event := seedReadRequest(t, cs, req)

	require.NoError(t, p.HandleEvent(context.Background(), event))

	assertStatus(t, cs, event.EventID, store.StatusConfirmed)
}

func TestReadEventProcessor_ExecutionFailureRetries(t *testing.T) {
	req := testReadRequest()
	voter := &fakeReadVoter{txHash: "VOTE_TX"}
	p, cs := newTestReadEventProcessor(t, voter, &fakeDestClient{err: fmt.Errorf("rpc down")})
	event := seedReadRequest(t, cs, req)

	require.NoError(t, p.HandleEvent(context.Background(), event))

	assert.Empty(t, voter.votes)
	assertStatus(t, cs, event.EventID, store.StatusConfirmed)
}

func TestReadEventProcessor_UnservedChainRetries(t *testing.T) {
	req := testReadRequest()
	voter := &fakeReadVoter{txHash: "VOTE_TX"}
	p, cs := newTestReadEventProcessor(t, voter, nil)
	event := seedReadRequest(t, cs, req)

	require.NoError(t, p.HandleEvent(context.Background(), event))

	assert.Empty(t, voter.votes)
	assertStatus(t, cs, event.EventID, store.StatusConfirmed)
}

func TestReadEventProcessor_HandlerUnavailableRetries(t *testing.T) {
	req := testReadRequest()
	voter := &fakeReadVoter{txHash: "VOTE_TX"}
	p, cs := newTestReadEventProcessor(t, voter, &fakeDestClient{notStarted: true})
	event := seedReadRequest(t, cs, req)

	require.NoError(t, p.HandleEvent(context.Background(), event))

	assert.Empty(t, voter.votes)
	assertStatus(t, cs, event.EventID, store.StatusConfirmed)
}

func TestReadEventProcessor_CorruptEventReverted(t *testing.T) {
	voter := &fakeReadVoter{txHash: "VOTE_TX"}
	p, cs := newTestReadEventProcessor(t, voter, &fakeDestClient{result: &uread.ReadResult{Status: uread.ReadStatusSuccess}})

	event := &store.Event{
		EventID:          "corrupt-read",
		Type:             store.EventTypeReadRequest,
		ConfirmationType: store.ConfirmationInstant,
		Status:           store.StatusConfirmed,
		EventData:        []byte("not json"),
	}
	stored, err := cs.InsertEventIfNotExists(event)
	require.NoError(t, err)
	require.True(t, stored)

	require.Error(t, p.HandleEvent(context.Background(), event))

	assert.Empty(t, voter.votes)
	assertStatus(t, cs, event.EventID, store.StatusReverted)
}

func TestReadEventProcessor_ExpiredMarkedReverted(t *testing.T) {
	req := testReadRequest()
	req.ExpiryBlockHeight = 50
	voter := &fakeReadVoter{txHash: "VOTE_TX"}
	p, cs := newTestReadEventProcessor(t, voter, &fakeDestClient{result: &uread.ReadResult{Status: uread.ReadStatusSuccess}})
	require.NoError(t, cs.UpdateChainHeight(100)) // push chain past expiry
	event := seedReadRequest(t, cs, req)

	require.NoError(t, p.HandleEvent(context.Background(), event))

	assert.Empty(t, voter.votes)
	assertStatus(t, cs, event.EventID, store.StatusReverted)
}

func TestReadEventProcessor_NotExpiredProcessesNormally(t *testing.T) {
	req := testReadRequest()
	req.ExpiryBlockHeight = 200
	voter := &fakeReadVoter{txHash: "VOTE_TX"}
	p, cs := newTestReadEventProcessor(t, voter, &fakeDestClient{result: &uread.ReadResult{Status: uread.ReadStatusSuccess}})
	require.NoError(t, cs.UpdateChainHeight(100))
	event := seedReadRequest(t, cs, req)

	require.NoError(t, p.HandleEvent(context.Background(), event))

	require.Contains(t, voter.votes, req.RequestID)
	assertStatus(t, cs, event.EventID, store.StatusCompleted)
}
