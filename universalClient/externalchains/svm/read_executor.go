package svm

import (
	"context"
	"encoding/binary"
	"fmt"
	"math/big"

	"github.com/gagliardetto/solana-go"

	"github.com/pushchain/push-chain-node/universalClient/externalchains/common"
	ucallbacktypes "github.com/pushchain/push-chain-node/x/ucallback/types"
)

// splTokenAmountOffset is the byte offset of the u64 amount in an SPL token account.
const splTokenAmountOffset = 64

// ExecuteRead implements common.ChainReader for Solana chains.
//
// Solana cannot query state at an exact past slot, so reads run at finalized
// commitment with minContextSlot as a staleness floor. ObservedBlockHeight (the
// context slot) may differ across validators; core's ballot key covers the
// result value only, never the observed slot.
func (c *Client) ExecuteRead(ctx context.Context, req *ucallbacktypes.ReadRequest) (*ucallbacktypes.ReadResult, error) {
	env, err := decodeSolanaQueryEnvelope(req.Query)
	if err != nil {
		return common.NewReadErrorResult(err), nil
	}

	if len(req.Owner) != solana.PublicKeyLength {
		return common.NewReadErrorResult(fmt.Errorf("owner must be a 32-byte pubkey, got %d bytes", len(req.Owner))), nil
	}
	account := solana.PublicKeyFromBytes(req.Owner)

	minSlot := max(env.MinSlot, req.DestinationBlockHeight)

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
			return common.NewReadErrorResult(encErr), nil
		}
		return &ucallbacktypes.ReadResult{
			Status:              ucallbacktypes.ReadStatus_READ_STATUS_SUCCESS,
			ResultData:          resultData,
			ObservedBlockHeight: slot,
		}, nil

	case solanaQuerySPLTokenAccount:
		data, owner, found, slot, rpcErr := c.rpcClient.GetAccountInfoWithSlot(ctx, account, minSlot)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if !found {
			return common.NewReadErrorResult(fmt.Errorf("token account %s not found", account)), nil
		}
		if !owner.Equals(solana.TokenProgramID) && !owner.Equals(solana.Token2022ProgramID) {
			return common.NewReadErrorResult(fmt.Errorf("account %s is not owned by a token program", account)), nil
		}
		if len(data) < splTokenAmountOffset+8 {
			return common.NewReadErrorResult(fmt.Errorf("token account data too short: %d bytes", len(data))), nil
		}
		amount := binary.LittleEndian.Uint64(data[splTokenAmountOffset : splTokenAmountOffset+8])
		resultData, encErr := common.EncodeUint256Result(new(big.Int).SetUint64(amount))
		if encErr != nil {
			return common.NewReadErrorResult(encErr), nil
		}
		return &ucallbacktypes.ReadResult{
			Status:              ucallbacktypes.ReadStatus_READ_STATUS_SUCCESS,
			ResultData:          resultData,
			ObservedBlockHeight: slot,
		}, nil

	case solanaQueryRawAccountData:
		data, _, found, slot, rpcErr := c.rpcClient.GetAccountInfoWithSlot(ctx, account, minSlot)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if !found {
			return common.NewReadErrorResult(fmt.Errorf("account %s not found", account)), nil
		}
		return &ucallbacktypes.ReadResult{
			Status:              ucallbacktypes.ReadStatus_READ_STATUS_SUCCESS,
			ResultData:          data,
			ObservedBlockHeight: slot,
		}, nil

	default:
		return common.NewReadErrorResult(fmt.Errorf("unknown SolanaQueryType %d", env.QueryType)), nil
	}
}
