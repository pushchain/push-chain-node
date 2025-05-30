package keeper

import (
	"context"

	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/push-protocol/push-chain/x/utv/types"
)

type queryServer struct {
	Keeper
}

// NewQuerier returns an implementation of the QueryServer interface
// for the provided Keeper.
func NewQuerier(keeper Keeper) types.QueryServer {
	return &queryServer{Keeper: keeper}
}

var _ types.QueryServer = queryServer{}

// Params implements the QueryServer.Params method.
func (q queryServer) Params(ctx context.Context, _ *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	params, err := q.Keeper.Params.Get(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryParamsResponse{Params: &params}, nil
}

// ChainConfig returns a specific chain configuration
func (q queryServer) ChainConfig(ctx context.Context, req *types.QueryChainConfigRequest) (*types.QueryChainConfigResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}

	if req.ChainId == "" {
		return nil, status.Error(codes.InvalidArgument, "chain ID cannot be empty")
	}

	configData, err := q.Keeper.GetChainConfig(ctx, req.ChainId)
	if err != nil {
		return nil, status.Error(codes.NotFound, "chain configuration not found")
	}

	// Convert from internal ChainConfigData to protobuf ChainConfig
	config := configData.ToProto()

	// Need to create a pointer from our value type
	return &types.QueryChainConfigResponse{
		ChainConfig: &config,
	}, nil
}

// ChainConfigs returns all available chain configurations
func (q queryServer) ChainConfigs(ctx context.Context, req *types.QueryChainConfigsRequest) (*types.QueryChainConfigsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}

	configsData, err := q.Keeper.GetAllChainConfigs(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	// Handle pagination manually
	var (
		start int = 0
		end   int = len(configsData)
		limit int = 100 // Default limit
	)

	// If pagination is provided, use its values
	if req.Pagination != nil {
		if req.Pagination.Offset > 0 {
			start = int(req.Pagination.Offset)
		}
		if req.Pagination.Limit > 0 {
			limit = int(req.Pagination.Limit)
		}
	}

	// Adjust end index
	if start >= end {
		start = end
	}
	if start+limit < end {
		end = start + limit
	}

	// Apply pagination
	paginatedData := configsData[start:end]

	// Convert from internal ChainConfigData to protobuf ChainConfig
	var configs []*types.ChainConfig
	for _, data := range paginatedData {
		// Create a pointer to the converted ChainConfig
		config := data.ToProto()
		configs = append(configs, &config)
	}

	return &types.QueryChainConfigsResponse{
		ChainConfigs: configs,
		Pagination: &query.PageResponse{
			NextKey: nil, // Implement proper next key logic if needed
			Total:   uint64(len(configsData)),
		},
	}, nil
}
