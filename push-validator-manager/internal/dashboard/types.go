package dashboard

import (
	"context"
	"time"

	"github.com/pushchain/push-chain-node/push-validator-manager/internal/config"
	"github.com/pushchain/push-chain-node/push-validator-manager/internal/metrics"
)

// Message types for Bubble Tea event loop - ensures deterministic control flow

// tickMsg is sent periodically to trigger data refresh
type tickMsg time.Time

// dataMsg contains successfully fetched dashboard data
type dataMsg DashboardData

// dataErrMsg contains an error from a failed data fetch
type dataErrMsg struct {
	err error
}

// fetchStartedMsg is sent when a fetch begins, carrying the cancel function
// This ensures cancel func is assigned on UI thread, not in Cmd goroutine
type fetchStartedMsg struct {
	cancel context.CancelFunc
}

// forceRefreshMsg is sent when user presses 'r' to refresh immediately
type forceRefreshMsg struct{}

// toggleHelpMsg is sent when user presses '?' to toggle help overlay
type toggleHelpMsg struct{}

// DashboardData aggregates all data shown in the dashboard
type DashboardData struct {
	// Reuse existing metrics collector
	Metrics metrics.Snapshot

	// Node process information
	NodeInfo struct {
		Running   bool
		PID       int
		Uptime    time.Duration
		BinaryVer string // Cached version (5min TTL)
	}

	// My validator status
	MyValidator struct {
		IsValidator                  bool
		Address                      string
		Moniker                      string
		Status                       string
		VotingPower                  int64
		VotingPct                    float64 // Percentage of total voting power [0,1]
		Commission                   string
		CommissionRewards            string // Accumulated commission rewards
		OutstandingRewards           string // Total outstanding rewards
		Jailed                       bool
		SlashingInfo                 struct {
			JailReason  string // "Downtime", "Double Sign", or "Unknown"
			JailedUntil string // RFC3339 formatted timestamp
			Tombstoned  bool   // Whether validator is permanently jailed (double sign)
			MissedBlocks int64  // Number of missed blocks
		}
		SlashingInfoError              string // Error message if slashing info fetch failed
		ValidatorExistsWithSameMoniker bool   // True if a different validator uses this node's moniker
		ConflictingMoniker            string // The moniker that conflicts
	}

	// Network validators list
	NetworkValidators struct {
		Validators []struct {
			Moniker              string
			Status               string
			VotingPower          int64
			Commission           string
			CommissionRewards    string // Accumulated commission rewards
			OutstandingRewards   string // Total outstanding rewards
			Address              string // Cosmos address (pushvaloper...)
			EVMAddress           string // EVM address (0x...)
			Jailed               bool   // Whether validator is jailed
		}
		Total int
	}

	// Connected peers list
	PeerList []struct {
		ID   string
		Addr string
	}

	LastUpdate time.Time
	Err        error // Last fetch error (for display in header)
}

// Options configures dashboard behavior
type Options struct {
	Config          config.Config
	RefreshInterval time.Duration
	RPCTimeout      time.Duration // Timeout for RPC calls (default: 5s)
	NoColor         bool
	NoEmoji         bool
	Debug           bool // Enable debug output
}
