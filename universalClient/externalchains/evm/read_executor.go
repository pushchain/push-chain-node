package evm

import (
	"context"
	"fmt"
	"math/big"

	ethcommon "github.com/ethereum/go-ethereum/common"

	"github.com/pushchain/push-chain-node/universalClient/externalchains/common"
	"github.com/pushchain/push-chain-node/universalClient/uread"
)

// balanceOfSelector is the 4-byte selector for balanceOf(address).
var balanceOfSelector = []byte{0x70, 0xa0, 0x82, 0x31}

// ExecuteRead implements common.ChainReader for EVM chains.
// All validators must produce byte-identical results, so every query runs at a
// deterministic block height.
func (c *Client) ExecuteRead(ctx context.Context, req *uread.ReadRequest) (*uread.ReadResult, error) {
	env, err := decodeEvmQueryEnvelope(req.Query)
	if err != nil {
		return uread.NewErrorResult(err), nil
	}

	height, err := c.resolveReadHeight(ctx, req, env)
	if err != nil {
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
			return uread.NewErrorResult(decErr), nil
		}
		balance, rpcErr := c.rpcClient.GetBalanceAt(ctx, target, blockNum)
		if rpcErr != nil {
			return nil, rpcErr
		}
		resultData, err = common.EncodeUint256Result(balance)

	case evmQueryERC20Balance:
		token, owner, decErr := decodeERC20BalancePayload(env.Payload)
		if decErr != nil {
			return uread.NewErrorResult(decErr), nil
		}
		callData := append(append([]byte{}, balanceOfSelector...), ethcommon.LeftPadBytes(owner.Bytes(), 32)...)
		ret, rpcErr := c.rpcClient.CallContract(ctx, token, callData, blockNum)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if len(ret) < 32 {
			return uread.NewErrorResult(fmt.Errorf("balanceOf returned %d bytes", len(ret))), nil
		}
		resultData, err = common.EncodeUint256Result(new(big.Int).SetBytes(ret[:32]))

	case evmQueryContractCall:
		target, callData, decErr := decodeContractCallPayload(env.Payload)
		if decErr != nil {
			return uread.NewErrorResult(decErr), nil
		}
		ret, rpcErr := c.rpcClient.CallContract(ctx, target, callData, blockNum)
		if rpcErr != nil {
			// eth_call reverts are deterministic at a pinned height — observable as ERROR.
			return uread.NewErrorResult(rpcErr), nil
		}
		resultData = ret

	case evmQueryStorageSlot:
		target, slot, decErr := decodeStorageSlotPayload(env.Payload)
		if decErr != nil {
			return uread.NewErrorResult(decErr), nil
		}
		value, rpcErr := c.rpcClient.GetStorageAt(ctx, target, slot, blockNum)
		if rpcErr != nil {
			return nil, rpcErr
		}
		var slotValue [32]byte
		copy(slotValue[32-min(len(value), 32):], value)
		resultData = slotValue[:]

	default:
		return uread.NewErrorResult(fmt.Errorf("unknown EvmQueryType %d", env.QueryType)), nil
	}
	if err != nil {
		return uread.NewErrorResult(err), nil
	}

	return &uread.ReadResult{
		Status:              uread.ReadStatusSuccess,
		ResultData:          resultData,
		ObservedBlockHeight: height,
		ObservedBlockHash:   header.Hash().Bytes(),
	}, nil
}

// resolveReadHeight picks the deterministic block height for a read.
// TODO(core): once x/uexecutor pins the height at request creation,
// PinnedBlockHeight is always set and the fallback below must be removed —
// latest-minConfirmations is NOT identical across validators.
func (c *Client) resolveReadHeight(ctx context.Context, req *uread.ReadRequest, env *evmQueryEnvelope) (uint64, error) {
	if req.PinnedBlockHeight > 0 {
		return req.PinnedBlockHeight, nil
	}
	if env.BlockNumber > 0 {
		return env.BlockNumber, nil
	}
	latest, err := c.rpcClient.GetLatestBlock(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get latest block: %w", err)
	}
	conf := uint64(req.MinConfirmations)
	if latest <= conf {
		return 0, fmt.Errorf("chain height %d below min confirmations %d", latest, conf)
	}
	return latest - conf, nil
}
