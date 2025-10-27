package metrics

import (
    "context"
    "fmt"
    "strings"
    "sync"
    "time"

    "github.com/pushchain/push-chain-node/push-validator-manager/internal/node"
    "github.com/shirou/gopsutil/v3/cpu"
    "github.com/shirou/gopsutil/v3/disk"
    "github.com/shirou/gopsutil/v3/mem"
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

type Node struct {
    ChainID      string
    NodeID       string
    Moniker      string
    RPCListening bool
}

type Snapshot struct {
    System  System
    Network Network
    Chain   Chain
    Node    Node
}

type Collector struct {
	mu         sync.RWMutex
	lastCPU    float64
	cpuRunning bool
}

func New() *Collector {
	c := &Collector{}
	// Start background CPU collection immediately
	go c.updateCPU()
	return c
}

// updateCPU runs in background to continuously update CPU metrics
func (c *Collector) updateCPU() {
	c.cpuRunning = true
	for {
		if percent, err := cpu.Percent(time.Second, false); err == nil && len(percent) > 0 {
			c.mu.Lock()
			c.lastCPU = percent[0]
			c.mu.Unlock()
		}
		// Small sleep to prevent tight loop
		time.Sleep(100 * time.Millisecond)
	}
}

// Collect queries local and remote RPCs to produce minimal metrics without external deps.
func (c *Collector) Collect(ctx context.Context, localRPC, remoteRPC string) Snapshot {
    snap := Snapshot{}
    local := node.New(localRPC)

    // Construct proper HTTP URL from genesis domain if it's just a hostname
    remoteURL := remoteRPC
    if !strings.HasPrefix(remoteRPC, "http://") && !strings.HasPrefix(remoteRPC, "https://") {
        // Default to HTTPS for remote endpoints
        remoteURL = fmt.Sprintf("https://%s:443", remoteRPC)
    }
    remote := node.New(remoteURL)

    // Local status
    if st, err := local.Status(ctx); err == nil {
        snap.Chain.LocalHeight = st.Height
        snap.Chain.CatchingUp = st.CatchingUp
        snap.Node.ChainID = st.Network
        snap.Node.NodeID = st.NodeID
        snap.Node.Moniker = st.Moniker
        snap.Node.RPCListening = true // If we got a response, RPC is listening
    }
    // Remote status
    if st, err := remote.RemoteStatus(ctx, remoteURL); err == nil {
        snap.Chain.RemoteHeight = st.Height
    }
    // Peers count (best-effort)
    if peers, err := local.Peers(ctx); err == nil {
        snap.Network.Peers = len(peers)
    }
    // Latency: time a single remote /status call
    t0 := time.Now()
    if _, err := remote.RemoteStatus(ctx, remoteURL); err == nil {
        snap.Network.LatencyMS = time.Since(t0).Milliseconds()
    }

    // System metrics
    // CPU usage - return cached value from background collection
    c.mu.RLock()
    snap.System.CPUPercent = c.lastCPU
    c.mu.RUnlock()

    // Memory usage
    if vmStat, err := mem.VirtualMemory(); err == nil {
        snap.System.MemUsed = vmStat.Used
        snap.System.MemTotal = vmStat.Total
    }

    // Disk usage - get usage for root filesystem
    if diskStat, err := disk.Usage("/"); err == nil {
        snap.System.DiskUsed = diskStat.Used
        snap.System.DiskTotal = diskStat.Total
    }

    return snap
}

