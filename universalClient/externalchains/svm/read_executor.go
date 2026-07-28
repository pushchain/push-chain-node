package svm

import (
	"context"
	"encoding/binary"
	"fmt"
	"math/big"

	"github.com/gagliardetto/solana-go"

	"github.com/pushchain/push-chain-node/universalClient/externalchains/common"
	"github.com/pushchain/push-chain-node/universalClient/uread"
)

// splTokenAmountOffset is the byte offset of the u64 amount in an SPL token account.
const splTokenAmountOffset = 64

// ExecuteRead implements common.ChainReader for Solana chains.
//
// Determinism caveat: Solana RPC cannot query state at an exact past slot, only
// ">= minSlot" via minContextSlot, so ObservedBlockHeight may differ across
// validators. TODO(core): ballot key must cover ResultData only (drop
// slot/hash) for solana, or quorum will never converge — flagged in
// docs/read-from-chains-implementation-plan.md.
func (c *Client) ExecuteRead(ctx context.Context, req *uread.ReadRequest) (*uread.ReadResult, error) {
	env, err := decodeSolanaQueryEnvelope(req.Query)
	if err != nil {
		return uread.NewErrorResult(err), nil
	}

	if len(req.Owner) != solana.PublicKeyLength {
		return uread.NewErrorResult(fmt.Errorf("owner must be a 32-byte pubkey, got %d bytes", len(req.Owner))), nil
	}
	account := solana.PublicKeyFromBytes(req.Owner)

	minSlot := max(env.MinSlot, req.PinnedBlockHeight)

	switch env.QueryType {
	case solanaQueryLamportBalance:
		balance, slot, rpcErr := c.rpcClient.GetBalanceWithSlot(ctx, account)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if slot < minSlot {
			return nil, fmt.Errorf("observed slot %d below min slot %d", slot, minSlot)
		}
		resultData, encErr := common.EncodeUint256Result(new(big.Int).SetUint64(balance))
		if encErr != nil {
			return uread.NewErrorResult(encErr), nil
		}
		return &uread.ReadResult{
			Status:              uread.ReadStatusSuccess,
			ResultData:          resultData,
			ObservedBlockHeight: slot,
		}, nil

	case solanaQuerySPLTokenAccount:
		data, owner, found, slot, rpcErr := c.rpcClient.GetAccountInfoWithSlot(ctx, account, minSlot)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if !found {
			return uread.NewErrorResult(fmt.Errorf("token account %s not found", account)), nil
		}
		if !owner.Equals(solana.TokenProgramID) && !owner.Equals(solana.Token2022ProgramID) {
			return uread.NewErrorResult(fmt.Errorf("account %s is not owned by a token program", account)), nil
		}
		if len(data) < splTokenAmountOffset+8 {
			return uread.NewErrorResult(fmt.Errorf("token account data too short: %d bytes", len(data))), nil
		}
		amount := binary.LittleEndian.Uint64(data[splTokenAmountOffset : splTokenAmountOffset+8])
		resultData, encErr := common.EncodeUint256Result(new(big.Int).SetUint64(amount))
		if encErr != nil {
			return uread.NewErrorResult(encErr), nil
		}
		return &uread.ReadResult{
			Status:              uread.ReadStatusSuccess,
			ResultData:          resultData,
			ObservedBlockHeight: slot,
		}, nil

	case solanaQueryRawAccountData:
		data, _, found, slot, rpcErr := c.rpcClient.GetAccountInfoWithSlot(ctx, account, minSlot)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if !found {
			return uread.NewErrorResult(fmt.Errorf("account %s not found", account)), nil
		}
		return &uread.ReadResult{
			Status:              uread.ReadStatusSuccess,
			ResultData:          data,
			ObservedBlockHeight: slot,
		}, nil

	default:
		return uread.NewErrorResult(fmt.Errorf("unknown SolanaQueryType %d", env.QueryType)), nil
	}
}
