package module

import (
	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"
	modulev1 "github.com/pushchain/push-chain-node/api/ucallback/v1"
)

// AutoCLIOptions implements the autocli.HasAutoCLIConfig interface.
func (am AppModule) AutoCLIOptions() *autocliv1.ModuleOptions {
	return &autocliv1.ModuleOptions{
		Query: &autocliv1.ServiceCommandDescriptor{
			Service: modulev1.Query_ServiceDesc.ServiceName,
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{
					RpcMethod: "Params",
					Use:       "params",
					Short:     "Query the current consensus parameters",
				},
				{
					RpcMethod: "AllPendingReadRequests",
					Use:       "pending-read-requests",
					Short:     "List read requests awaiting an observation",
				},
				{
					RpcMethod:      "UniversalRead",
					Use:            "universal-read <request-id>",
					Short:          "Query one read request by id",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "request_id"}},
				},
				{
					RpcMethod: "AllAbortedReadRequests",
					Use:       "aborted-read-requests",
					Short:     "List reads the chain gave up on; these need manual intervention",
				},
				{
					RpcMethod:      "ReadsByTx",
					Use:            "reads-by-tx <tx-hash>",
					Short:          "List every read requested by one Push transaction",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "tx_hash"}},
				},
			},
		},
		Tx: &autocliv1.ServiceCommandDescriptor{
			Service: modulev1.Msg_ServiceDesc.ServiceName,
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{
					RpcMethod: "UpdateParams",
					Skip:      false, // set to true if authority gated
				},
				{
					RpcMethod:      "RetryReadExpiry",
					Use:            "retry-read-expiry <request-id>",
					Short:          "Admin: reattempt expiry for an abandoned read",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "request_id"}},
				},
				{
					RpcMethod: "VoteReadResult",
					Use:       "vote-read-result <request-id>",
					Short:     "Vote on the observed outcome of a read request",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "request_id"},
					},
				},
			},
		},
	}
}
