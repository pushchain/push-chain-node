package pushcore

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// grpcDefaultMaxRecvMsgSize is the receive cap grpc-go applies when a client
// sets none. The whole point of maxPushCoreRecvMsgSize is that the bound is a
// choice made here rather than this inherited default.
const grpcDefaultMaxRecvMsgSize = 4 * 1024 * 1024

// serveResponseOfSize starts a gRPC server on a loopback port that answers any
// request with a pending-outbounds response carrying a payload of payloadBytes,
// and returns its address.
func serveResponseOfSize(t *testing.T, payloadBytes int) string {
	t.Helper()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	resp := &uexecutortypes.QueryAllPendingOutboundsResponse{
		Outbounds: []*uexecutortypes.OutboundTx{{
			Id:      "oversized",
			Payload: strings.Repeat("a", payloadBytes),
		}},
	}

	srv := grpc.NewServer(grpc.UnknownServiceHandler(func(_ interface{}, stream grpc.ServerStream) error {
		var req uexecutortypes.QueryAllPendingOutboundsRequest
		if err := stream.RecvMsg(&req); err != nil {
			return err
		}
		return stream.SendMsg(resp)
	}))

	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	return lis.Addr().String()
}

func queryPendingOutbounds(t *testing.T, endpoint string) (*uexecutortypes.QueryAllPendingOutboundsResponse, error) {
	t.Helper()

	conn, err := createGRPCConnection(endpoint)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	return uexecutortypes.NewQueryClient(conn).
		AllPendingOutbounds(ctx, &uexecutortypes.QueryAllPendingOutboundsRequest{})
}

func TestPushCoreRecvMsgSizeIsExplicit(t *testing.T) {
	// A bound equal to the library default would be indistinguishable from
	// setting nothing at all.
	require.Greater(t, maxPushCoreRecvMsgSize, grpcDefaultMaxRecvMsgSize,
		"the receive bound must be a deliberate value, not grpc-go's implicit default")

	t.Run("a response inside the bound is accepted", func(t *testing.T) {
		// Above grpc-go's default, below ours: this only succeeds because the
		// dial options carry an explicit MaxCallRecvMsgSize.
		const size = 5 * 1024 * 1024
		require.Greater(t, size, grpcDefaultMaxRecvMsgSize)
		require.Less(t, size, maxPushCoreRecvMsgSize)

		resp, err := queryPendingOutbounds(t, serveResponseOfSize(t, size))
		require.NoError(t, err)
		require.Len(t, resp.Outbounds, 1)
		require.Len(t, resp.Outbounds[0].Payload, size)
	})

	t.Run("a response past the bound fails as ResourceExhausted", func(t *testing.T) {
		resp, err := queryPendingOutbounds(t, serveResponseOfSize(t, maxPushCoreRecvMsgSize+1))

		require.Nil(t, resp, "no partial response may be handed to the caller")
		require.Error(t, err)

		st, ok := status.FromError(err)
		require.True(t, ok, "the failure must be a gRPC status, not an opaque error: %v", err)
		require.Equal(t, codes.ResourceExhausted, st.Code())
		require.Contains(t, st.Message(), "8388608",
			"the error must name the bound that was applied")
	})
}
