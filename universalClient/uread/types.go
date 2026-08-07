// Package uread is a TEMPORARY package: it mirrors the read-request proto types
// x/uexecutor will generate (proto/uexecutor/v1/read_request.proto + tx.proto).
//
// TODO(core): once core lands, replace every uread.* reference with the
// generated uexecutortypes equivalents and delete this package.
package uread

// ReadRequest mirrors the pending read request tracked by x/uexecutor.
type ReadRequest struct {
	RequestID              string // uint256 as 0x-prefixed hex (from ReadRequested event)
	DestinationChain       string // CAIP-2, e.g. "eip155:1", "solana:mainnet-beta"; web2 uses "web2:https"
	Owner                  []byte // ReadSpec.account.owner (20-byte addr / 32-byte pubkey)
	Query                  []byte // chain-specific envelope, abi.encode(...)
	MinConfirmations       uint16
	DestinationBlockHeight uint64 // destination chain height the read is made at; not applicable for web2
	ExpiryBlockHeight      uint64 // Push chain height at which the request expires
	CreatedAtHeight        uint64 // Push chain height at which the request was created
}

// ReadStatus is the observed outcome a validator votes on.
type ReadStatus int32

const (
	ReadStatusSuccess ReadStatus = 1
	ReadStatusError   ReadStatus = 2
)

// ReadResult is the canonical observation submitted via MsgVoteReadResult.
// All fields must be byte-identical across validators for quorum.
type ReadResult struct {
	Status              ReadStatus
	ResultData          []byte
	ObservedBlockHeight uint64 // block number (EVM) or slot (SVM)
	ObservedBlockHash   []byte // 32 bytes; empty when the chain cannot pin one deterministically
	ErrorMsg            string // local diagnostic only — never part of the ballot
}

// NewErrorResult builds an ERROR observation. ResultData stays empty so all
// validators voting ERROR converge on the same ballot regardless of local error text.
func NewErrorResult(err error) *ReadResult {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	return &ReadResult{Status: ReadStatusError, ErrorMsg: msg}
}
