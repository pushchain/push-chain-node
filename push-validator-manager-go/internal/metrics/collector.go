package metrics

import (
    "context"
    "time"

    "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/node"
)

type System struct {
    CPUPercent float64
    MemUsed    uint64
    MemTotal   uint64
    DiskUsed   uint64
    DiskTotal  uint64
}

type Network struct {
    Peers     int
    LatencyMS int64
}

type Chain struct {
    LocalHeight  int64
    RemoteHeight int64
    CatchingUp   bool
}

type Snapshot struct {
    System  System
    Network Network
    Chain   Chain
}

type Collector struct{}

func New() *Collector { return &Collector{} }

// Collect queries local and remote RPCs to produce minimal metrics without external deps.
func (c *Collector) Collect(ctx context.Context, localRPC, remoteRPC string) Snapshot {
    snap := Snapshot{}
    local := node.New(localRPC)
    remote := node.New(remoteRPC)

    // Local status
    if st, err := local.Status(ctx); err == nil {
        snap.Chain.LocalHeight = st.Height
        snap.Chain.CatchingUp = st.CatchingUp
    }
    // Remote status
    if st, err := remote.RemoteStatus(ctx, remoteRPC); err == nil {
        snap.Chain.RemoteHeight = st.Height
    }
    // Peers count (best-effort)
    if peers, err := local.Peers(ctx); err == nil {
        snap.Network.Peers = len(peers)
    }
    // Latency: time a single remote /status call
    t0 := time.Now()
    if _, err := remote.RemoteStatus(ctx, remoteRPC); err == nil {
        snap.Network.LatencyMS = time.Since(t0).Milliseconds()
    }
    return snap
}

