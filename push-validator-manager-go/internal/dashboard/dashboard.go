package dashboard

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/pushchain/push-chain-node/push-validator-manager-go/internal/metrics"
	"github.com/pushchain/push-chain-node/push-validator-manager-go/internal/node"
	"github.com/pushchain/push-chain-node/push-validator-manager-go/internal/process"
	"github.com/pushchain/push-chain-node/push-validator-manager-go/internal/validator"
)

// keyMap defines keyboard shortcuts
type keyMap struct {
	Quit       key.Binding
	Refresh    key.Binding
	Help       key.Binding
	Up         key.Binding
	Down       key.Binding
	Left       key.Binding
	Right      key.Binding
	Search     key.Binding
	Follow     key.Binding
	Home       key.Binding
	End        key.Binding
}

// ShortHelp implements help.KeyMap for inline help
func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Quit, k.Refresh, k.Help}
}

// FullHelp implements help.KeyMap for full help overlay
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Quit, k.Refresh, k.Help},
		{k.Up, k.Down, k.Left, k.Right},
		{k.Search, k.Follow, k.Home, k.End},
	}
}

// newKeyMap creates default key bindings
func newKeyMap() keyMap {
	return keyMap{
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
		Refresh: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "refresh now"),
		),
		Help: key.NewBinding(
			key.WithKeys("h"),
			key.WithHelp("h", "toggle help"),
		),
		Up: key.NewBinding(
			key.WithKeys("up"),
			key.WithHelp("↑", "scroll up logs"),
		),
		Down: key.NewBinding(
			key.WithKeys("down"),
			key.WithHelp("↓", "scroll down logs"),
		),
		Left: key.NewBinding(
			key.WithKeys("left", "p"),
			key.WithHelp("←/p", "prev page validators"),
		),
		Right: key.NewBinding(
			key.WithKeys("right", "n"),
			key.WithHelp("→/n", "next page validators"),
		),
		Search: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "search logs"),
		),
		Follow: key.NewBinding(
			key.WithKeys("f"),
			key.WithHelp("f", "toggle follow mode"),
		),
		Home: key.NewBinding(
			key.WithKeys("t"),
			key.WithHelp("t", "jump to oldest"),
		),
		End: key.NewBinding(
			key.WithKeys("l"),
			key.WithHelp("l", "jump to latest"),
		),
	}
}

// tickCmd returns a command that sends a tick message after interval
func tickCmd(interval time.Duration) tea.Cmd {
	return tea.Tick(interval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// Dashboard is the main Bubble Tea Model
type Dashboard struct {
	opts     Options
	data     DashboardData
	lastOK   time.Time
	err      error
	stale    bool
	registry *ComponentRegistry
	layout   *Layout
	keys     keyMap
	help     help.Model
	spinner  spinner.Model
	width    int
	height   int
	showHelp bool
	loading  bool

	// Context for cancelling in-flight fetches
	fetchCancel context.CancelFunc

	// Persistent metrics collector for CPU monitoring
	collector *metrics.Collector

	// Caching for expensive operations
	cachedVersion    string
	cachedVersionAt  time.Time
	cachedVersionPID int
}

// New creates a new Dashboard instance
func New(opts Options) *Dashboard {
	// Apply sensible defaults to prevent zero-value bugs
	if opts.RefreshInterval <= 0 {
		opts.RefreshInterval = 2 * time.Second
	}
	if opts.RPCTimeout <= 0 {
		rt := 5 * time.Second
		if 2*opts.RefreshInterval < rt {
			rt = 2 * opts.RefreshInterval
		}
		opts.RPCTimeout = rt
	}

	// Initialize component registry
	registry := NewComponentRegistry()
	registry.Register(NewHeader())
	registry.Register(NewNodeStatus(opts.NoEmoji))
	registry.Register(NewChainStatus(opts.NoEmoji))
	registry.Register(NewNetworkStatus(opts.NoEmoji))
	registry.Register(NewValidatorsList(opts.NoEmoji, opts.Config))
	registry.Register(NewValidatorInfo(opts.NoEmoji))
	registry.Register(NewLogViewer(opts.NoEmoji, opts.Config.HomeDir))

	// Configure layout
	layoutConfig := LayoutConfig{
		Rows: []LayoutRow{
			{Components: []string{"header"}, Weights: []int{100}, MinHeight: 4},
			{Components: []string{"node_status", "chain_status"}, Weights: []int{50, 50}, MinHeight: 10},
			{Components: []string{"network_status", "validator_info"}, Weights: []int{50, 50}, MinHeight: 10},
			{Components: []string{"validators_list"}, Weights: []int{100}, MinHeight: 16},
			{Components: []string{"log_viewer"}, Weights: []int{100}, MinHeight: 12},
		},
	}
	layout := NewLayout(layoutConfig, registry)

	// Initialize spinner
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	return &Dashboard{
		opts:      opts,
		registry:  registry,
		layout:    layout,
		keys:      newKeyMap(),
		help:      help.New(),
		spinner:   s,
		loading:   true,
		showHelp:  false,
		collector: metrics.New(), // Initialize persistent collector for continuous CPU monitoring
	}
}

// Init initializes the dashboard (Bubble Tea lifecycle)
func (m *Dashboard) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		m.fetchCmd(),
		tickCmd(m.opts.RefreshInterval),
	)
}

// Update handles messages (Bubble Tea lifecycle)
func (m *Dashboard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case fetchStartedMsg:
		// SAFE: assign cancel func on UI thread (not in Cmd goroutine)
		if m.fetchCancel != nil {
			m.fetchCancel() // Cancel any previous fetch
		}
		m.fetchCancel = msg.cancel
		return m, nil

	case tickMsg:
		// CRITICAL: Only tickMsg schedules next tick (prevents double ticker)
		return m, tea.Batch(
			m.fetchCmd(),
			tickCmd(m.opts.RefreshInterval),
		)

	case dataMsg:
		// Successful fetch - update data and clear error
		m.data = DashboardData(msg)
		m.lastOK = time.Now()
		m.err = nil
		m.stale = false
		m.loading = false
		// Update components
		cmds := m.registry.UpdateAll(msg, m.data)
		return m, tea.Batch(cmds...)

	case dataErrMsg:
		// Failed fetch - keep old data, show error, mark stale
		m.err = msg.err
		m.data.Err = msg.err // Set error in data so Header can display it
		m.stale = time.Since(m.lastOK) > 10*time.Second
		m.loading = false
		// Update components to propagate error to Header
		cmds := m.registry.UpdateAll(msg, m.data)
		return m, tea.Batch(cmds...)

	case forceRefreshMsg:
		// User pressed 'r' - start new fetch immediately
		return m, m.fetchCmd()

	case toggleHelpMsg:
		m.showHelp = !m.showHelp
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	return m, nil
}

// View renders the dashboard (Bubble Tea lifecycle)
func (m *Dashboard) View() string {
	// Add recovery for View method panics
	defer func() {
		if r := recover(); r != nil {
			if m.opts.Debug {
				fmt.Fprintf(os.Stderr, "Debug: View() panic recovered: %v\n", r)
			}
		}
	}()

	// Guard against zero-size render before first WindowSizeMsg
	if m.width <= 0 || m.height <= 1 {
		return ""
	}

	// Safety check for nil pointers
	if m.registry == nil || m.layout == nil {
		if m.opts.Debug {
			fmt.Fprintf(os.Stderr, "Debug: Registry or layout is nil\n")
		}
		return "Initializing dashboard..."
	}

	if m.loading {
		// Show centered spinner with message
		return lipgloss.Place(
			m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			fmt.Sprintf("%s Connecting to RPC...", m.spinner.View()),
		)
	}

	if m.showHelp {
		// Overlay command help - non-blocking render
		helpView := getCommandHelpText()
		return lipgloss.Place(
			m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				Padding(1, 2).
				Render(helpView),
		)
	}

	// DON'T reserve space for spacer - use full height
	result := m.layout.Compute(m.width, m.height)

	// Build rowMap with ALL cells (including header)
	rowMap := make(map[int][]Cell)
	for _, cell := range result.Cells {
		rowMap[cell.Y] = append(rowMap[cell.Y], cell)
	}

	// Sort Y coordinates
	ys := make([]int, 0, len(rowMap))
	for y := range rowMap {
		ys = append(ys, y)
	}
	sort.Ints(ys)

	// Render all rows in order
	var rows []string
	for _, y := range ys {
		cells := rowMap[y]
		sort.Slice(cells, func(i, j int) bool { return cells[i].X < cells[j].X })

		var rowCells []string
		for _, cell := range cells {
            if comp := m.registry.Get(cell.ID); comp != nil {
                s := comp.View(cell.W, cell.H)
                rowCells = append(rowCells, s)
            }
		}

        if len(rowCells) > 0 {
            joined := lipgloss.JoinHorizontal(lipgloss.Top, rowCells...)
            rows = append(rows, joined)
        }
	}

	// Join all rows WITHOUT any spacer
	output := lipgloss.JoinVertical(lipgloss.Left, rows...)

	// Show layout warning if present
	if result.Warning != "" {
		output += fmt.Sprintf("\n⚠ %s\n", result.Warning)
	}

	// Add footer with controls
	footerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Bold(false)
	footer := footerStyle.Render("Controls: h for help | Ctrl+C to exit")
	output = lipgloss.JoinVertical(lipgloss.Left, output, footer)

	return output
}

// getCommandHelpText returns formatted help text showing all available commands
func getCommandHelpText() string {
	var help strings.Builder

	help.WriteString("Push Validator Manager\n")
	help.WriteString("Manage a Push Chain validator node: init, start, status, sync, and admin tasks.\n")
	help.WriteString("──────────────────────────────────────────────────────\n\n")

	help.WriteString("USAGE\n")
	help.WriteString("  push-validator-manager <command> [flags]\n\n")

	help.WriteString("Quick Start\n")
	help.WriteString("  push-validator-manager start              Start the node process\n")
	help.WriteString("  push-validator-manager status             Show node/rpc/sync status\n")
	help.WriteString("  push-validator-manager dashboard          Live dashboard with metrics\n")
	help.WriteString("  push-validator-manager sync               Monitor sync progress live\n\n")

	help.WriteString("Operations\n")
	help.WriteString("  push-validator-manager stop               Stop the node process\n")
	help.WriteString("  push-validator-manager restart            Restart the node process\n")
	help.WriteString("  push-validator-manager logs               Tail node logs\n\n")

	help.WriteString("Validator\n")
	help.WriteString("  push-validator-manager validators         List validators (--output json)\n")
	help.WriteString("  push-validator-manager balance [addr]     Check account balance\n")
	help.WriteString("  push-validator-manager register-validator Register this node as validator\n\n")

	help.WriteString("Maintenance\n")
	help.WriteString("  push-validator-manager backup             Create config/state backup\n")
	help.WriteString("  push-validator-manager reset              Reset chain data (keeps addr book)\n")
	help.WriteString("  push-validator-manager full-reset         Complete reset (deletes ALL)\n\n")

	help.WriteString("Utilities\n")
	help.WriteString("  push-validator-manager doctor             Run diagnostic checks\n")
	help.WriteString("  push-validator-manager peers              List connected peers\n")
	help.WriteString("  push-validator-manager version            Show version information\n\n")

	help.WriteString("Press 'q', 'h', or 'esc' to close help")

	return help.String()
}

// handleKey processes keyboard input
func (m *Dashboard) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// If help is showing, allow closing it with q, h, or esc
	if m.showHelp {
		switch msg.String() {
		case "q", "h", "esc":
			return m, func() tea.Msg { return toggleHelpMsg{} }
		}
		// Ignore other keys while help is showing
		return m, nil
	}

	switch {
	case key.Matches(msg, m.keys.Quit):
		if m.fetchCancel != nil {
			m.fetchCancel() // Cancel in-flight fetch
		}
		return m, tea.Quit

	case key.Matches(msg, m.keys.Refresh):
		return m, func() tea.Msg { return forceRefreshMsg{} }

	case key.Matches(msg, m.keys.Help):
		return m, func() tea.Msg { return toggleHelpMsg{} }

	case key.Matches(msg, m.keys.Up), key.Matches(msg, m.keys.Down),
		key.Matches(msg, m.keys.Left), key.Matches(msg, m.keys.Right),
		key.Matches(msg, m.keys.Search), key.Matches(msg, m.keys.Follow),
		key.Matches(msg, m.keys.Home), key.Matches(msg, m.keys.End):
		// Forward to components (log viewer and validators list)
		cmds := m.registry.UpdateAll(msg, m.data)
		return m, tea.Batch(cmds...)
	}

	// Also forward other keys to components (for search input)
	if msg.Type == tea.KeyRunes || msg.Type == tea.KeyBackspace || msg.Type == tea.KeyEscape || msg.Type == tea.KeyEnter {
		cmds := m.registry.UpdateAll(msg, m.data)
		return m, tea.Batch(cmds...)
	}

	return m, nil
}

// fetchCmd returns a Cmd that fetches data asynchronously
func (m *Dashboard) fetchCmd() tea.Cmd {
	// Use configurable RPC timeout from options
	ctx, cancel := context.WithTimeout(context.Background(), m.opts.RPCTimeout)

	// Direct return tea.Sequence - cleaner pattern
	return tea.Sequence(
		func() tea.Msg { return fetchStartedMsg{cancel: cancel} },
		func() tea.Msg {
			defer cancel()
			data, err := m.fetchData(ctx)
			if err != nil {
				return dataErrMsg{err: err}
			}
			return dataMsg(data)
		},
	)
}

// fetchData does the actual blocking I/O (called from fetchCmd)
func (m *Dashboard) fetchData(ctx context.Context) (DashboardData, error) {
	data := DashboardData{LastUpdate: time.Now()}

	// Use persistent collector for continuous CPU monitoring
	data.Metrics = m.collector.Collect(ctx, m.opts.Config.RPCLocal, m.opts.Config.GenesisDomain)

	// Fetch peer details
	local := node.New(m.opts.Config.RPCLocal)
	if peers, err := local.Peers(ctx); err == nil {
		data.PeerList = make([]struct {
			ID   string
			Addr string
		}, len(peers))
		for i, p := range peers {
			data.PeerList[i].ID = p.ID
			data.PeerList[i].Addr = p.Addr
		}
	}

	// Fetch node info
	sup := process.New(m.opts.Config.HomeDir)
	data.NodeInfo.Running = sup.IsRunning()
	if pid, ok := sup.PID(); ok {
		data.NodeInfo.PID = pid
	}

	// Get uptime if node is running
	if data.NodeInfo.Running {
		if uptime, ok := sup.Uptime(); ok {
			data.NodeInfo.Uptime = uptime
		}
	}

	// Get cached binary version (only refresh every 5 min)
	data.NodeInfo.BinaryVer = m.getCachedVersion(ctx, data.NodeInfo.Running, data.NodeInfo.PID)

	// Fetch validator data (cached 30s)
	if valList, err := validator.GetCachedValidatorsList(ctx, m.opts.Config); err == nil {
		// Convert validator.ValidatorInfo to dashboard format
		data.NetworkValidators.Total = valList.Total
		data.NetworkValidators.Validators = make([]struct {
			Moniker              string
			Status               string
			VotingPower          int64
			Commission           string
			CommissionRewards    string // Accumulated commission rewards
			OutstandingRewards   string // Total outstanding rewards
			Address              string // Cosmos address (pushvaloper...)
			EVMAddress           string // EVM address (0x...)
		}, len(valList.Validators))

		for i, v := range valList.Validators {
			data.NetworkValidators.Validators[i].Moniker = v.Moniker
			data.NetworkValidators.Validators[i].Status = v.Status
			data.NetworkValidators.Validators[i].VotingPower = v.VotingPower
			data.NetworkValidators.Validators[i].Commission = v.Commission
			data.NetworkValidators.Validators[i].Address = v.OperatorAddress
			// EVM address will be fetched on-demand when user toggles to show it
			data.NetworkValidators.Validators[i].EVMAddress = ""
			// Rewards will be fetched lazily when page is displayed
			data.NetworkValidators.Validators[i].CommissionRewards = ""
			data.NetworkValidators.Validators[i].OutstandingRewards = ""
		}
	}

	// Fetch my validator status (cached 30s)
	if myVal, err := validator.GetCachedMyValidator(ctx, m.opts.Config); err == nil {
		data.MyValidator.IsValidator = myVal.IsValidator
		data.MyValidator.Address = myVal.Address
		data.MyValidator.Moniker = myVal.Moniker
		data.MyValidator.Status = myVal.Status
		data.MyValidator.VotingPower = myVal.VotingPower
		data.MyValidator.VotingPct = myVal.VotingPct
		data.MyValidator.Commission = myVal.Commission
		data.MyValidator.Jailed = myVal.Jailed
		data.MyValidator.ValidatorExistsWithSameMoniker = myVal.ValidatorExistsWithSameMoniker
		data.MyValidator.ConflictingMoniker = myVal.ConflictingMoniker

		// Fetch rewards for my validator if registered
		if myVal.IsValidator && myVal.Address != "" {
			if commRwd, outRwd, err := validator.GetValidatorRewards(ctx, m.opts.Config, myVal.Address); err == nil {
				data.MyValidator.CommissionRewards = commRwd
				data.MyValidator.OutstandingRewards = outRwd
			} else {
				// Set placeholders on error
				data.MyValidator.CommissionRewards = "—"
				data.MyValidator.OutstandingRewards = "—"
			}
		}
	}

	return data, nil
}

// getCachedVersion fetches version with caching (5min TTL + PID-based invalidation)
func (m *Dashboard) getCachedVersion(ctx context.Context, running bool, currentPID int) string {
	// Don't call pchaind version when node is stopped
	if !running {
		return "—"
	}

	// Invalidate cache if PID changed (process restarted)
	if currentPID != m.cachedVersionPID {
		m.cachedVersion = ""
		m.cachedVersionPID = currentPID
		m.cachedVersionAt = time.Time{} // Force immediate fetch
	}

	if time.Since(m.cachedVersionAt) < 5*time.Minute && m.cachedVersion != "" {
		return m.cachedVersion
	}

	// First check if pchaind exists in PATH
	pchainPath, err := exec.LookPath("pchaind")
	if err != nil {
		if m.opts.Debug {
			fmt.Fprintf(os.Stderr, "Debug: pchaind not found in PATH: %v\n", err)
		}
		m.cachedVersion = "pchaind not found"
		return m.cachedVersion
	}

	// Fetch version (can be slow - 200-500ms typical)
	cmd := exec.CommandContext(ctx, pchainPath, "version")
	out, err := cmd.Output()
	if err == nil {
		m.cachedVersion = strings.TrimSpace(string(out))
		m.cachedVersionAt = time.Now()
	} else {
		if m.opts.Debug {
			fmt.Fprintf(os.Stderr, "Debug: Failed to get pchaind version: %v\n", err)
		}
		m.cachedVersion = "version error"
	}

	return m.cachedVersion
}

// FetchDataOnce performs a single blocking data fetch for non-TTY mode
func (m *Dashboard) FetchDataOnce(ctx context.Context) (DashboardData, error) {
	return m.fetchData(ctx)
}

// RenderStatic renders a static text snapshot of dashboard data
func (m *Dashboard) RenderStatic(data DashboardData) string {
	var b strings.Builder

	b.WriteString("=== PUSH VALIDATOR STATUS ===\n\n")

	// Node Status
	b.WriteString("NODE STATUS:\n")
	if data.NodeInfo.Running {
		b.WriteString(fmt.Sprintf("  Status: Running (PID: %d)\n", data.NodeInfo.PID))
		b.WriteString(fmt.Sprintf("  Version: %s\n", data.NodeInfo.BinaryVer))
	} else {
		b.WriteString("  Status: Stopped\n")
	}
	b.WriteString(fmt.Sprintf("  RPC: %s\n", m.opts.Config.RPCLocal))
	b.WriteString("\n")

	// Chain Status
	b.WriteString("CHAIN STATUS:\n")
	b.WriteString(fmt.Sprintf("  Height: %s\n", HumanInt(data.Metrics.Chain.LocalHeight)))
	if data.Metrics.Chain.RemoteHeight > 0 {
		b.WriteString(fmt.Sprintf("  Remote Height: %s\n", HumanInt(data.Metrics.Chain.RemoteHeight)))
	}
	if data.Metrics.Chain.RemoteHeight > data.Metrics.Chain.LocalHeight {
		blocksBehind := data.Metrics.Chain.RemoteHeight - data.Metrics.Chain.LocalHeight
		b.WriteString(fmt.Sprintf("  Blocks Behind: %s\n", HumanInt(blocksBehind)))
	}
	b.WriteString(fmt.Sprintf("  Catching Up: %v\n", data.Metrics.Chain.CatchingUp))
	b.WriteString("\n")

	// Network Status
	b.WriteString("NETWORK STATUS:\n")
	b.WriteString(fmt.Sprintf("  Peers: %d\n", data.Metrics.Network.Peers))
	b.WriteString(fmt.Sprintf("  Chain ID: %s\n", data.Metrics.Node.ChainID))
	b.WriteString("\n")

	// Validator Status
	if data.MyValidator.IsValidator {
		b.WriteString("VALIDATOR STATUS:\n")
		b.WriteString(fmt.Sprintf("  Moniker: %s\n", data.MyValidator.Moniker))
		b.WriteString(fmt.Sprintf("  Status: %s\n", data.MyValidator.Status))
		b.WriteString(fmt.Sprintf("  Voting Power: %s", HumanInt(data.MyValidator.VotingPower)))
		if data.MyValidator.VotingPct > 0 {
			b.WriteString(fmt.Sprintf(" (%s)\n", Percent(data.MyValidator.VotingPct)))
		} else {
			b.WriteString("\n")
		}
		b.WriteString(fmt.Sprintf("  Jailed: %v\n", data.MyValidator.Jailed))
		b.WriteString("\n")
	}

	b.WriteString(fmt.Sprintf("Last Update: %s\n", data.LastUpdate.Format("2006-01-02 15:04:05 MST")))

	return b.String()
}
