package evm

import (
	"context"
	"fmt"
	"math/big"

	"github.com/pushchain/push-chain-node/universalClient/externalchains/common"
	ucallbacktypes "github.com/pushchain/push-chain-node/x/ucallback/types"
)

// ExecuteRead implements common.ChainReader for EVM chains.
// All validators must produce byte-identical results, so every query runs at the
// height pinned in the request; execution is gated until that height has
// min_confirmations confirmations so a reorg cannot invalidate the read.
func (c *Client) ExecuteRead(ctx context.Context, req *ucallbacktypes.ReadRequest) (*ucallbacktypes.ReadResult, error) {
	env, err := decodeEvmQueryEnvelope(req.Query)
	if err != nil {
		return common.NewReadErrorResult(err), nil
	}

	height := req.DestinationBlockHeight
	if height == 0 {
		height = env.BlockNumber
	}
	if height == 0 {
		return common.NewReadErrorResult(fmt.Errorf("read request has no target height")), nil
	}

	if err := c.gateHeightConfirmed(ctx, height, uint64(req.MinConfirmations)); err != nil {
		return nil, err
	}
	blockNum := new(big.Int).SetUint64(height)

	header, err := c.rpcClient.GetHeaderByNumber(ctx, blockNum)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch header at %d: %w", height, err)
	}

	var resultData []byte
	switch env.QueryType {
	case evmQueryAccountBalance:
		target, decErr := decodeAccountBalancePayload(env.Payload)
		if decErr != nil {
			return common.NewReadErrorResult(decErr), nil
		}
		balance, rpcErr := c.rpcClient.GetBalanceAt(ctx, target, blockNum)
		if rpcErr != nil {
			return nil, rpcErr
		}
		resultData, err = common.EncodeUint256Result(balance)

	case evmQueryContractCall:
		target, callData, decErr := decodeContractCallPayload(env.Payload)
		if decErr != nil {
			return common.NewReadErrorResult(decErr), nil
		}
		ret, rpcErr := c.rpcClient.CallContract(ctx, target, callData, blockNum)
		if rpcErr != nil {
			// eth_call reverts are deterministic at a pinned height — observable as ERROR.
			return common.NewReadErrorResult(rpcErr), nil
		}
		resultData = ret

	case evmQueryStorageSlot:
		target, slot, decErr := decodeStorageSlotPayload(env.Payload)
		if decErr != nil {
			return common.NewReadErrorResult(decErr), nil
		}
		value, rpcErr := c.rpcClient.GetStorageAt(ctx, target, slot, blockNum)
		if rpcErr != nil {
			return nil, rpcErr
		}
		var slotValue [32]byte
		copy(slotValue[32-min(len(value), 32):], value)
		resultData = slotValue[:]

	default:
		return common.NewReadErrorResult(fmt.Errorf("unknown EvmQueryType %d", env.QueryType)), nil
	}
	if err != nil {
		return common.NewReadErrorResult(err), nil
	}

	return &ucallbacktypes.ReadResult{
		Status:              ucallbacktypes.ReadStatus_READ_STATUS_SUCCESS,
		ResultData:          resultData,
		ObservedBlockHeight: height,
		ObservedBlockHash:   header.Hash().Bytes(),
	}, nil
}

// gateHeightConfirmed blocks execution until the target height has at least
// minConfirmations confirmations. An error is transient: the processor keeps
// the event CONFIRMED and retries next tick.
func (c *Client) gateHeightConfirmed(ctx context.Context, height, minConfirmations uint64) error {
	latest, err := c.rpcClient.GetLatestBlock(ctx)
	if err != nil {
		return fmt.Errorf("failed to get latest block: %w", err)
	}
	if latest < height+minConfirmations {
		return fmt.Errorf("height %d needs %d confirmations, chain at %d; not final yet", height, minConfirmations, latest)
	}
	return nil
}
