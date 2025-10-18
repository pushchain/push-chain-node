# Dashboard Implementation Plan - Phase 1

## Overview
Create a real-time, modular dashboard for push-validator-manager with extensible component architecture for easy future additions.

## Changelog

### v2 (Critical Production Fixes)
✅ **Race condition fix:** fetchStartedMsg pattern to avoid mutating model state from Cmd goroutines
✅ **Layout correctness:** LayoutResult with Cell[] to prevent render mismatches
✅ **Fair remainder distribution:** Width allocation sorted by fractional parts
✅ **HTTP client:** Single reusable client with proper Transport timeouts
✅ **First-load UX:** Spinner + stale banner in header
✅ **Utility edge cases:** HumanInt negatives, Percent [0,1] convention, ProgressBar width protection
✅ **Moving average ETA:** Prevents flapping with 10-sample history
✅ **Validator pagination:** Status filtering (bonded/unbonding/unbonded)
✅ **Voting power %:** Cached total with 5min TTL
✅ **PID-based version caching:** Invalidate on process restart
✅ **bubbles/help integration:** Auto-generated help overlay
✅ **TTY graceful degradation:** Static snapshot for CI/pipes
✅ **Icons struct:** Consistent emoji/ASCII fallback
✅ **--rpc-timeout flag:** Configurable RPC timeout
✅ **Header fixed height:** Reserve 3 lines in layout
✅ **Golden tests:** NO_COLOR=1, fixed time, odd widths
✅ **Updated risk checklist:** 50+ ship-blocker items

### v2.1 (Last-Mile Polish)
✅ **Cleaner tea.Sequence:** Direct return (no nested Cmd)
✅ **Single tick chain:** Only tickMsg schedules next tick (no double ticker)
✅ **Cancel ownership:** fetchStartedMsg is sole write location
✅ **Vertical slack distribution:** Fair height allocation across rows
✅ **Truncation utility:** Ellipsize long strings to prevent overflow
✅ **Voting power label:** "Validator (Consensus Power)" for clarity
✅ **Remote RPC option:** --remote-rpc for latency measurement
✅ **Enhanced version cache:** Skip when not running, PID-based invalidation
✅ **Non-blocking help:** Overlay renders without pausing ticks
✅ **Testing enhancements:** timeNow injection, layout gap tests, truncation golden tests
✅ **Utility improvements:** Percent docstring, neutral icon (Unknown state)

### v2.2 (Compilation Correctness)
✅ **Header component interface alignment:** View signature changed from View(w, h, lastOK, err) to canonical View(w, h int) string - moved lastOK/err to struct fields
✅ **RPCTimeout configuration threading:** Added Options.RPCTimeout field, updated fetchCmd() and newHTTPClient() to use configurable timeout instead of hard-coded 5s
✅ **Non-TTY helper methods:** Added FetchDataOnce() and RenderStatic() methods for static snapshot rendering in CI/pipes
✅ **ProgressBar width calculation fix:** Unicode mode now uses full width (no brackets), ASCII mode uses width-2 to account for [ ] - eliminates layout math inconsistency

## 1. Add Dependencies

**Update go.mod:**
- `github.com/charmbracelet/bubbletea` - TUI framework (Model-Update-View pattern)
- `github.com/charmbracelet/lipgloss` - Layout and styling (complements existing color system)
- `github.com/charmbracelet/bubbles` - Reusable components (key bindings, help, spinner)
- `github.com/cespare/xxhash/v2` - Fast hashing for cache invalidation

**Optional (Phase 1: stick to ASCII, defer to later):**
- `github.com/mattn/go-runewidth` - Unicode width calculation (only if unicode borders needed)

```bash
cd push-validator-manager-go
go get github.com/charmbracelet/bubbletea
go get github.com/charmbracelet/lipgloss
go get github.com/charmbracelet/bubbles
go get github.com/cespare/xxhash/v2
# go get github.com/mattn/go-runewidth  # Defer to Phase 2
```

## 2. Modular Component Architecture

**Create `internal/dashboard/` module:**

```
internal/dashboard/
├── dashboard.go           # Main dashboard model & coordinator
├── component.go           # Component interface & registry
├── layout.go              # Responsive layout manager
├── types.go               # Shared types (messages, models)
├── components/
│   ├── header.go          # Dashboard header/title component
│   ├── node_status.go     # Node status panel component
│   ├── chain_status.go    # Chain/sync status panel component
│   ├── network_status.go  # Network/peers panel component
│   └── validator_info.go  # Validator info panel component
└── README.md              # Component development guide
```

### Core Interface Design

```go
// Component interface - all dashboard panels implement this
type Component interface {
    // Bubbletea lifecycle
    Init() tea.Cmd
    Update(msg tea.Msg, data DashboardData) (Component, tea.Cmd)
    View(width, height int) string

    // Metadata
    ID() string
    Title() string
    MinWidth() int   // Minimum width required (asymmetric - width matters more)
    MinHeight() int  // Minimum height required
}

// Registry for dynamic component management
type ComponentRegistry struct {
    components []Component
    layout     LayoutConfig
}

// Easy to add new components:
// registry.Register(NewMyCustomComponent())
```

## 3. Dashboard Components (Phase 1)

### Header Component
- **App title:** "PUSH VALIDATOR DASHBOARD"
- **Current timestamp:** Updates live
- **Quick controls hint:** (q: quit, r: refresh, ?: help)
- **Height:** 3 lines

### Node Status Panel
- **Status indicator:** Running/Stopped with colored icon
- **PID:** Process ID when running
- **RPC:** Connectivity status + URL
- **Uptime:** Calculate from process start time
- **Binary Version:** pchaind version info
- **Size:** ~8 lines

### Chain Status Panel
- **Current Height:** Formatted with thousands separator
- **Remote Height:** Latest network height
- **Sync Progress:** Visual progress bar with percentage
- **Catching Up:** Yes/No indicator
- **ETA to Sync:** Calculated based on sync rate
- **Blocks Behind:** Remote - local height
- **Size:** ~10 lines

### Network Status Panel
- **Peers Connected:** Count with icon
- **Network Latency:** To remote RPC (ms)
- **Chain ID:** Network identifier
- **Node ID:** Tendermint node ID
- **Moniker:** Validator name
- **Size:** ~8 lines

### Validator Info Panel ⭐
- **Title:** "Validator (Consensus Power)" - explicit about power type
- **Validator Address:** Operator address
- **Status:** Bonded/Unbonded/Unbonding
- **Voting Power:** Formatted with percentage of total (12,345,678 upc (0.5%))
- **Commission Rate:** Current commission
- **Jailed:** Yes/No status
- **Delegator Count:** (if available)
- **Size:** ~10 lines

## 4. Layout System

### Responsive Grid Layout

```
┌─────────────────────────────────────────────────┐
│           HEADER (full width)                   │
├──────────────────┬──────────────────────────────┤
│  Node Status     │  Chain Status                │
│  (25% width)     │  (40% width)                 │
│                  │                              │
├──────────────────┼──────────────────────────────┤
│  Network Status  │  Validator Info              │
│  (25% width)     │  (40% width)                 │
│                  │                              │
└──────────────────┴──────────────────────────────┘
```

### Layout Features
- Auto-adjust to terminal size (min 80x24)
- **Honor MinSize before applying weights** (prevents clipping)
- Graceful degradation for small terminals
- Vertical stack mode for narrow terminals (<100 cols)
- Component dropping strategy when layout impossible

### Layout Config with LayoutResult

```go
type LayoutConfig struct {
    Rows []LayoutRow
}

type LayoutRow struct {
    Components []string  // Component IDs
    Weights    []int     // Width distribution
    MinHeight  int
}

// Cell represents a positioned component in the final layout
type Cell struct {
    ID   string
    X, Y int
    W, H int
}

// LayoutResult is returned by Compute - concrete positioned components
type LayoutResult struct {
    Cells   []Cell
    Warning string // "Some panels hidden (terminal too narrow)"
}

// Compute builds the final layout with concrete Cell positions
// Includes vertical slack distribution when terminal height > sum of MinHeight
func (l *Layout) Compute(width, height int) LayoutResult {
    result := LayoutResult{Cells: make([]Cell, 0)}

    // Step 1: Calculate base row heights (MinHeight for each)
    rowHeights := make([]int, len(l.config.Rows))
    totalMinHeight := 0
    for i, row := range l.config.Rows {
        rowHeights[i] = row.MinHeight
        totalMinHeight += row.MinHeight
    }

    // Step 2: Distribute vertical slack if available
    verticalSlack := height - totalMinHeight
    if verticalSlack > 0 && len(l.config.Rows) > 0 {
        // Distribute evenly across rows (could also use weights if needed)
        extraPerRow := verticalSlack / len(l.config.Rows)
        remainder := verticalSlack % len(l.config.Rows)

        for i := range rowHeights {
            rowHeights[i] += extraPerRow
            if i < remainder {
                rowHeights[i]++ // Fair remainder distribution
            }
        }
    }

    // Step 3: Build cells with distributed heights
    y := 0
    for i, row := range l.config.Rows {
        // Compute widths with MinSize + weight distribution
        widths, warning := l.computeRowWidths(row, width)
        if warning != "" {
            result.Warning = warning
        }

        // Build cells with actual component IDs
        x := 0
        for j, compID := range row.Components {
            if j < len(widths) {
                result.Cells = append(result.Cells, Cell{
                    ID: compID,
                    X:  x,
                    Y:  y,
                    W:  widths[j],
                    H:  rowHeights[i], // Use distributed height
                })
                x += widths[j]
            }
        }
        y += rowHeights[i]
    }

    return result
}

// computeRowWidths honors MinWidth, distributes by weights, handles remainder
func (l *Layout) computeRowWidths(row LayoutRow, totalWidth int) ([]int, string) {
    widths := make([]int, len(row.Components))

    // Step 1: Satisfy all MinWidth requirements
    remainingWidth := totalWidth
    for i, compID := range row.Components {
        comp := l.registry.Get(compID)
        minW := comp.MinWidth()
        widths[i] = minW
        remainingWidth -= minW
    }

    // Step 2: Check if MinWidth requirements can be satisfied
    if remainingWidth < 0 {
        // Try stack mode or drop components
        return l.handleInsufficientWidth(row, totalWidth)
    }

    // Step 3: Distribute remaining width by weights + remainder
    totalWeight := 0
    for _, w := range row.Weights {
        totalWeight += w
    }

    // Track fractional parts for fair remainder distribution
    type frac struct {
        idx  int
        frac float64
    }
    fracs := make([]frac, len(row.Components))

    distributed := 0
    for i, weight := range row.Weights {
        exact := float64(remainingWidth*weight) / float64(totalWeight)
        extra := int(exact)
        widths[i] += extra
        distributed += extra
        fracs[i] = frac{idx: i, frac: exact - float64(extra)}
    }

    // Distribute remainder (remainingWidth - distributed) to largest fractional parts
    remainder := remainingWidth - distributed
    sort.Slice(fracs, func(i, j int) bool {
        return fracs[i].frac > fracs[j].frac
    })
    for i := 0; i < remainder && i < len(fracs); i++ {
        widths[fracs[i].idx]++
    }

    return widths, ""
}

// handleInsufficientWidth tries stack mode or drops components
func (l *Layout) handleInsufficientWidth(row LayoutRow, width int) ([]int, string) {
    // Try dropping non-essential components
    essential := []string{"header", "node_status", "chain_status"}
    kept := []string{}
    for _, compID := range row.Components {
        if contains(essential, compID) {
            kept = append(kept, compID)
        }
    }

    if len(kept) == 0 {
        kept = row.Components[:1] // Keep at least one
    }

    warning := "Some panels hidden (terminal too narrow)"
    newRow := LayoutRow{Components: kept, Weights: equalWeights(len(kept)), MinHeight: row.MinHeight}
    widths, _ := l.computeRowWidths(newRow, width)
    return widths, warning
}
```

## 5. Real-time Data Flow

### Bubble Tea Message Types (Critical for Correctness)

**Define typed messages for deterministic control flow:**

```go
// Message types for Bubble Tea event loop
type tickMsg time.Time        // Timer tick for periodic refresh
type dataMsg DashboardData    // Successful data fetch
type dataErrMsg struct {      // Failed data fetch
    err error
}
type fetchStartedMsg struct { // Fetch initiated with cancel func
    cancel context.CancelFunc
}
type forceRefreshMsg struct{} // User pressed 'r'
type toggleHelpMsg struct{}   // User pressed '?'

// Tick command - returns next tick message
func tickCmd(interval time.Duration) tea.Cmd {
    return tea.Tick(interval, func(t time.Time) tea.Msg {
        return tickMsg(t)
    })
}
```

### Data Collection

```go
type DashboardData struct {
    // Existing metrics (reuse metrics.Snapshot)
    Metrics metrics.Snapshot

    // Additional data
    NodeInfo struct {
        Running      bool
        PID          int
        Uptime       time.Duration
        BinaryVer    string  // Cached (updated every 5min, not every 2s)
    }

    ValidatorInfo struct {
        Address      string
        Status       string
        VotingPower  int64
        VotingPct    float64
        Commission   string
        Jailed       bool
        Delegators   int
    }

    LastUpdate time.Time
    // Error is NOT stored here - use separate dataErrMsg
}
```

### Update Strategy (Deterministic Tea.Tick Pattern)

**CRITICAL: Use Tea.Tick instead of goroutines to prevent race conditions and maintain deterministic control flow.**

```go
// Options configures dashboard behavior
type Options struct {
    Config          config.Config
    RefreshInterval time.Duration
    RPCTimeout      time.Duration // Timeout for RPC calls (default: 5s)
    NoColor         bool
    NoEmoji         bool
}

// Dashboard model
type Dashboard struct {
    opts       Options
    data       DashboardData
    lastOK     time.Time
    err        error
    stale      bool
    registry   *ComponentRegistry
    layout     *Layout
    keys       keyMap
    width      int
    height     int
    showHelp   bool

    // Context for cancelling in-flight fetches
    fetchCancel context.CancelFunc
}

// Init starts the first tick and fetch
func (m *Dashboard) Init() tea.Cmd {
    return tea.Batch(
        m.fetchCmd(),
        tickCmd(m.opts.RefreshInterval),
    )
}

// Update handles all messages deterministically
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
        // forceRefreshMsg just calls fetchCmd() without scheduling another tick
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
        return m, nil

    case dataErrMsg:
        // Failed fetch - keep old data, show error, mark stale
        m.err = msg.err
        m.stale = time.Since(m.lastOK) > 10*time.Second
        return m, nil

    case forceRefreshMsg:
        // User pressed 'r' - start new fetch immediately
        return m, m.fetchCmd()
    }

    return m, nil
}

// fetchCmd returns a Cmd that fetches data asynchronously
// CRITICAL: Uses tea.Sequence to emit fetchStartedMsg THEN result
// This ensures cancel func is assigned on UI thread (not in goroutine)
// v2.1: Cleaner pattern - direct return tea.Sequence (no nested Cmd)
func (m *Dashboard) fetchCmd() tea.Cmd {
    // Use configurable RPC timeout from options
    ctx, cancel := context.WithTimeout(context.Background(), m.opts.RPCTimeout)

    // Direct return tea.Sequence - cleaner than nested Cmd wrapper
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

    // Reuse existing collectors
    collector := metrics.New()
    data.Metrics = collector.Collect(ctx, m.opts.Config.RPCLocal, m.opts.Config.GenesisDomain)

    // Fetch node info
    sup := process.New(m.opts.Config.HomeDir)
    data.NodeInfo.Running = sup.IsRunning()
    if pid, ok := sup.PID(); ok {
        data.NodeInfo.PID = pid
    }

    // Get cached binary version (only refresh every 5 min)
    data.NodeInfo.BinaryVer = m.getCachedVersion(ctx)

    // Fetch validator info with pagination
    if data.NodeInfo.Running {
        valInfo, err := m.fetchValidatorInfo(ctx)
        if err == nil {
            data.ValidatorInfo = valInfo
        }
        // Don't fail entire fetch if validator fetch fails
    }

    return data, nil
}

// FetchDataOnce performs a single blocking data fetch for non-TTY mode
// Used by cmd_dashboard.go non-TTY fallback to print static snapshot
func (m *Dashboard) FetchDataOnce(ctx context.Context) (DashboardData, error) {
    return m.fetchData(ctx)
}

// RenderStatic renders a static text snapshot of dashboard data
// Used by cmd_dashboard.go non-TTY fallback for CI/pipe output
func (m *Dashboard) RenderStatic(data DashboardData) string {
    var b strings.Builder

    // Header section
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
    b.WriteString(fmt.Sprintf("  Height: %s\n", HumanInt(data.Metrics.Height)))
    b.WriteString(fmt.Sprintf("  Remote Height: %s\n", HumanInt(data.Metrics.RemoteHeight)))
    if data.Metrics.RemoteHeight > data.Metrics.Height {
        blocksBehind := data.Metrics.RemoteHeight - data.Metrics.Height
        b.WriteString(fmt.Sprintf("  Blocks Behind: %s\n", HumanInt(blocksBehind)))
    }
    b.WriteString(fmt.Sprintf("  Catching Up: %v\n", data.Metrics.CatchingUp))
    b.WriteString("\n")

    // Network Status
    b.WriteString("NETWORK STATUS:\n")
    b.WriteString(fmt.Sprintf("  Peers: %d\n", data.Metrics.Peers))
    b.WriteString(fmt.Sprintf("  Chain ID: %s\n", data.Metrics.ChainID))
    b.WriteString("\n")

    // Validator Status
    if data.ValidatorInfo.Address != "" {
        b.WriteString("VALIDATOR STATUS:\n")
        b.WriteString(fmt.Sprintf("  Address: %s\n", data.ValidatorInfo.Address))
        b.WriteString(fmt.Sprintf("  Status: %s\n", data.ValidatorInfo.Status))
        b.WriteString(fmt.Sprintf("  Voting Power: %s", HumanInt(data.ValidatorInfo.VotingPower)))
        if data.ValidatorInfo.VotingPct > 0 {
            b.WriteString(fmt.Sprintf(" (%s)\n", Percent(data.ValidatorInfo.VotingPct)))
        } else {
            b.WriteString("\n")
        }
        b.WriteString(fmt.Sprintf("  Jailed: %v\n", data.ValidatorInfo.Jailed))
        b.WriteString("\n")
    }

    b.WriteString(fmt.Sprintf("Last Update: %s\n", data.LastUpdate.Format("2006-01-02 15:04:05 MST")))

    return b.String()
}
```

### Key Principles

✅ **Deterministic:** All state changes happen in `Update()`, never in goroutines
✅ **No races:** Context cancellation prevents overlapping fetches
✅ **Responsive:** UI never blocks on I/O
✅ **Testable:** Pure functions, easy to mock
✅ **Debuggable:** Message-based flow is easy to trace

## 6. Styling Integration

### Reuse Existing UI System
- Leverage `ui.ColorConfig` and themes
- Wrap with lipgloss for borders, padding, alignment
- Support --no-color, --no-emoji flags
- Consistent with existing CLI output

### HTTP Client Configuration

**Create single reusable HTTP client with proper timeouts:**

```go
// In dashboard.go or node/client.go
// newHTTPClient creates HTTP client with configurable timeout
func newHTTPClient(timeout time.Duration) *http.Client {
    tr := &http.Transport{
        ResponseHeaderTimeout: 3 * time.Second,
        ExpectContinueTimeout: 1 * time.Second,
        IdleConnTimeout:       30 * time.Second,
        TLSHandshakeTimeout:   3 * time.Second,
        MaxIdleConns:          100,
        MaxIdleConnsPerHost:   10,
    }
    return &http.Client{
        Timeout:   timeout, // Use configurable timeout
        Transport: tr,
    }
}

// Dashboard field
type Dashboard struct {
    // ... other fields ...
    httpClient *http.Client
}

// Initialize in New()
func New(opts Options) *Dashboard {
    return &Dashboard{
        // ...
        httpClient: newHTTPClient(opts.RPCTimeout),
    }
}
```

**Why:** Faster connections, fewer ephemeral sockets, better under packet loss.

### First-Load UX with Spinner

**Add spinner state and loading indicator:**

```go
import "github.com/charmbracelet/bubbles/spinner"

type Dashboard struct {
    // ... other fields ...
    loading    bool
    spinner    spinner.Model
}

func New(opts Options) *Dashboard {
    s := spinner.New()
    s.Spinner = spinner.Dot
    s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

    return &Dashboard{
        // ...
        loading: true,  // Start in loading state
        spinner: s,
    }
}

func (m *Dashboard) Init() tea.Cmd {
    return tea.Batch(
        m.spinner.Tick,  // Start spinner animation
        m.fetchCmd(),
        tickCmd(m.opts.RefreshInterval),
    )
}

func (m *Dashboard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    // ... existing cases ...

    case dataMsg:
        m.data = DashboardData(msg)
        m.lastOK = time.Now()
        m.err = nil
        m.stale = false
        m.loading = false  // Clear loading state
        return m, nil

    case spinner.TickMsg:
        var cmd tea.Cmd
        m.spinner, cmd = m.spinner.Update(msg)
        return m, cmd
    }
    return m, nil
}

// In View()
func (m *Dashboard) View() string {
    if m.loading {
        // Show centered spinner with message
        return lipgloss.Place(
            m.width, m.height,
            lipgloss.Center, lipgloss.Center,
            fmt.Sprintf("%s Connecting to RPC...", m.spinner.View()),
        )
    }
    // ... normal rendering ...
}
```

### Stale Data Banner in Header

**Header component shows stale warning:**

```go
// In components/header.go
type Header struct {
    BaseComponent
    lastOK time.Time  // Updated via Update() method
    err    error      // Updated via Update() method
}

func NewHeader() *Header {
    return &Header{
        BaseComponent: BaseComponent{
            id:    "header",
            title: "PUSH VALIDATOR DASHBOARD",
            minW:  40,
            minH:  3,
        },
    }
}

// Update receives dashboard data and updates internal state
func (h *Header) Update(msg tea.Msg, data DashboardData) (Component, tea.Cmd) {
    // Update lastOK and err from dashboard state
    // (Passed via custom message or accessed from data)
    return h, nil
}

// View matches canonical Component.View(width, height int) signature
func (h *Header) View(w, h int) string {
    timestamp := time.Now().Format("15:04:05 MST")
    title := h.title

    // Stale banner when data > 10s old
    staleBanner := ""
    if !h.lastOK.IsZero() && time.Since(h.lastOK) > 10*time.Second {
        staleBanner = lipgloss.NewStyle().
            Foreground(lipgloss.Color("208")).
            Render(" (STALE)")
    }

    // Error display (truncated to width)
    errorLine := ""
    if h.err != nil {
        errMsg := fmt.Sprintf("⚠ %s", h.err.Error())
        if len(errMsg) > w-10 {
            errMsg = errMsg[:w-13] + "..."
        }
        errorLine = lipgloss.NewStyle().
            Foreground(lipgloss.Color("196")).
            Render(errMsg)
    }

    header := fmt.Sprintf("%s\nLast update: %s%s\n%s",
        title, timestamp, staleBanner, errorLine)

    return lipgloss.NewStyle().
        BorderStyle(lipgloss.RoundedBorder()).
        BorderBottom(true).
        Width(w).
        Render(header)
}
```

### Component Styling Pattern with Performance Optimization

**Use hash-based caching to prevent re-rendering identical content:**

```go
package components

import (
    "github.com/cespare/xxhash/v2"
    "github.com/charmbracelet/lipgloss"
)

// BaseComponent provides caching infrastructure
type BaseComponent struct {
    id       string
    title    string
    minW     int
    minH     int

    // Performance optimization
    lastHash uint64
    cached   string
}

func (c *BaseComponent) ID() string { return c.id }
func (c *BaseComponent) Title() string { return c.title }
func (c *BaseComponent) MinWidth() int { return c.minW }
func (c *BaseComponent) MinHeight() int { return c.minH }

// Example component with caching
type NodeStatus struct {
    BaseComponent
    data NodeData
}

func (c *NodeStatus) View(w, h int) string {
    // Build plain content
    content := c.renderContent(w, h)

    // Cheap change detection - avoid re-styling if content unchanged
    h64 := xxhash.Sum64String(content)
    if h64 == c.lastHash && c.cached != "" {
        return c.cached
    }

    // Content changed - re-render with styling
    style := lipgloss.NewStyle().
        Border(lipgloss.RoundedBorder()).
        BorderForeground(lipgloss.Color("63")).
        Padding(0, 1).
        Width(w).
        Height(h)

    c.lastHash = h64
    c.cached = style.Render(content)
    return c.cached
}

func (c *NodeStatus) renderContent(w, h int) string {
    // Build plain text content without styling
    // This is what gets hashed for change detection
    return fmt.Sprintf("Node: %s\nPID: %d\n...", c.data.Status, c.data.PID)
}
```

**Why this matters:**
- At 2s refresh, components re-render frequently
- lipgloss styling is expensive (string allocation, ANSI codes)
- Most refreshes: data unchanged → hash identical → return cached
- Typical speedup: 5-10x for unchanged panels

### Utility Package for Common Formatting

**Create `internal/dashboard/util.go` to avoid duplication:**

```go
package dashboard

import (
    "fmt"
    "strings"
    "time"
)

// HumanInt formats integers with thousands separators (handles negatives)
func HumanInt(n int64) string {
    sign := ""
    if n < 0 {
        sign = "-"
        n = -n
    }

    s := strconv.FormatInt(n, 10)
    if len(s) <= 3 {
        return sign + s
    }

    var result strings.Builder
    for i, c := range reverse(s) {
        if i > 0 && i%3 == 0 {
            result.WriteRune(',')
        }
        result.WriteRune(c)
    }
    return sign + reverse(result.String())
}

// Percent formats percentage - takes fraction in [0,1], returns formatted %
// IMPORTANT: Input convention is [0,1], not [0,100]
// Example: Percent(0.123) → "12.3%"
func Percent(fraction float64) string {
    if fraction < 0 {
        return "0.0%"
    }
    if fraction > 1 {
        return "100.0%"
    }
    return fmt.Sprintf("%.1f%%", fraction*100)
}

// truncateWithEllipsis caps string length to prevent overflow in fixed-width cells
// v2.1: Prevents border breaks when content exceeds available width
func truncateWithEllipsis(s string, maxLen int) string {
    if maxLen <= 0 {
        return ""
    }
    if maxLen == 1 {
        return "…"
    }
    runes := []rune(s)
    if len(runes) <= maxLen {
        return s
    }
    return string(runes[:maxLen-1]) + "…"
}

// ProgressBar creates ASCII progress bar (protects against width < 3)
func ProgressBar(fraction float64, width int, noEmoji bool) string {
    if fraction < 0 {
        fraction = 0
    }
    if fraction > 1 {
        fraction = 1
    }
    if width < 3 {
        // Too narrow for meaningful bar
        return fmt.Sprintf("%.0f%%", fraction*100)
    }

    // Calculate bar width - ASCII mode needs room for brackets
    barWidth := width
    if noEmoji {
        barWidth = width - 2 // Account for [ ] in ASCII mode only
    }

    filled := int(float64(barWidth) * fraction)
    if filled > barWidth {
        filled = barWidth
    }

    if noEmoji {
        // ASCII-only mode with brackets
        return "[" + strings.Repeat("=", filled) + strings.Repeat(" ", barWidth-filled) + "]"
    }

    // Unicode mode uses full width (no brackets)
    return strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
}

// DurationShort formats duration concisely
func DurationShort(d time.Duration) string {
    if d < time.Minute {
        return fmt.Sprintf("%ds", int(d.Seconds()))
    }
    if d < time.Hour {
        return fmt.Sprintf("%dm", int(d.Minutes()))
    }
    if d < 24*time.Hour {
        h := int(d.Hours())
        m := int(d.Minutes()) % 60
        if m == 0 {
            return fmt.Sprintf("%dh", h)
        }
        return fmt.Sprintf("%dh%dm", h, m)
    }
    days := int(d.Hours()) / 24
    h := int(d.Hours()) % 24
    if h == 0 {
        return fmt.Sprintf("%dd", days)
    }
    return fmt.Sprintf("%dd%dh", days, h)
}

// ETACalculator maintains moving average for stable ETA
type ETACalculator struct {
    samples []struct {
        blocksBehind int64
        timestamp    time.Time
    }
    maxSamples int
}

func NewETACalculator() *ETACalculator {
    return &ETACalculator{maxSamples: 10}
}

func (e *ETACalculator) AddSample(blocksBehind int64) {
    e.samples = append(e.samples, struct {
        blocksBehind int64
        timestamp    time.Time
    }{blocksBehind, time.Now()})

    if len(e.samples) > e.maxSamples {
        e.samples = e.samples[1:]
    }
}

func (e *ETACalculator) Calculate() string {
    if len(e.samples) < 2 {
        return "calculating..."
    }

    // Compute rate from first to last sample
    first := e.samples[0]
    last := e.samples[len(e.samples)-1]

    blocksDelta := first.blocksBehind - last.blocksBehind
    timeDelta := last.timestamp.Sub(first.timestamp).Seconds()

    if timeDelta < 1 || blocksDelta <= 0 {
        return "calculating..."
    }

    rate := float64(blocksDelta) / timeDelta
    if rate < 0.01 { // Less than 1 block per 100 seconds
        return "calculating..."
    }

    seconds := float64(last.blocksBehind) / rate
    if seconds < 0 {
        return "—"
    }

    return DurationShort(time.Duration(seconds * float64(time.Second)))
}

// Icons struct for consistent emoji/ASCII fallback
// v2.1: Added Unknown for neutral/indeterminate states
type Icons struct {
    OK      string
    Warn    string
    Err     string
    Peer    string
    Block   string
    Unknown string // v2.1: Neutral icon for unknown/indeterminate states
}

func NewIcons(noEmoji bool) Icons {
    if noEmoji {
        return Icons{
            OK:      "[OK]",
            Warn:    "[!]",
            Err:     "[X]",
            Peer:    "#",
            Block:   "#",
            Unknown: "[?]", // v2.1: Neutral ASCII
        }
    }
    return Icons{
        OK:      "✓",
        Warn:    "⚠",
        Err:     "✗",
        Peer:    "🔗",
        Block:   "📦",
        Unknown: "◯", // v2.1: Neutral circle emoji
    }
}

func reverse(s string) string {
    runes := []rune(s)
    for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
        runes[i], runes[j] = runes[j], runes[i]
    }
    return string(runes)
}
```

### Keyboard Controls with Structured Keymap

**Define keymap in `dashboard.go` for maintainability:**

```go
import "github.com/charmbracelet/bubbles/key"

type keyMap struct {
    Quit    key.Binding
    Refresh key.Binding
    Help    key.Binding
}

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
            key.WithKeys("?"),
            key.WithHelp("?", "toggle help"),
        ),
    }
}

// In Dashboard.Update():
func (m *Dashboard) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
    switch {
    case key.Matches(msg, m.keys.Quit):
        return m, tea.Quit

    case key.Matches(msg, m.keys.Refresh):
        return m, func() tea.Msg { return forceRefreshMsg{} }

    case key.Matches(msg, m.keys.Help):
        m.showHelp = !m.showHelp
        return m, nil
    }

    return m, nil
}
```

**Benefits:**
- Centralized key definitions
- Easy to generate help text from keymap
- Simple to add new shortcuts
- Consistent key handling

### Help Overlay with bubbles/help

**Integrate help view for automatic help generation:**

```go
import "github.com/charmbracelet/bubbles/help"

type Dashboard struct {
    // ... other fields ...
    keys     keyMap
    help     help.Model
    showHelp bool
}

func New(opts Options) *Dashboard {
    return &Dashboard{
        // ...
        keys:     newKeyMap(),
        help:     help.New(),
        showHelp: false,
    }
}

// ShortHelp implements help.KeyMap for inline help
func (k keyMap) ShortHelp() []key.Binding {
    return []key.Binding{k.Quit, k.Refresh, k.Help}
}

// FullHelp implements help.KeyMap for full help overlay
func (k keyMap) FullHelp() [][]key.Binding {
    return [][]key.Binding{
        {k.Quit, k.Refresh},
        {k.Help},
    }
}

// In View()
// v2.1: Help overlay is non-blocking - doesn't pause tick stream
// Help is rendered via lipgloss.Place overlay, ticks continue in background
func (m *Dashboard) View() string {
    // ... render components ...

    if m.showHelp {
        // Overlay full help - IMPORTANT: non-blocking render
        // Dashboard continues to update in background (tick stream not paused)
        helpView := m.help.View(m.keys)
        return lipgloss.Place(
            m.width, m.height,
            lipgloss.Center, lipgloss.Center,
            lipgloss.NewStyle().
                Border(lipgloss.RoundedBorder()).
                Padding(1, 2).
                Render(helpView),
        )
    }

    // ... normal view ...
}
```

**Why:** Help text stays in sync with key bindings automatically.

## 7. Add Dashboard Command

**Create:** `cmd/push-validator-manager/cmd_dashboard.go`

```go
package main

import (
    "github.com/spf13/cobra"
    tea "github.com/charmbracelet/bubbletea"

    "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/dashboard"
)

var (
    dashboardRefreshInterval time.Duration
    dashboardRPCTimeout      time.Duration
)

var dashboardCmd = &cobra.Command{
    Use:   "dashboard",
    Short: "Launch interactive real-time dashboard",
    Long:  "Real-time monitoring dashboard with node, chain, network, and validator status",
    RunE: func(cmd *cobra.Command, args []string) error {
        cfg := loadCfg()

        // Initialize dashboard with config
        dash := dashboard.New(dashboard.Options{
            Config:          cfg,
            RefreshInterval: dashboardRefreshInterval,
            RPCTimeout:      dashboardRPCTimeout,
            NoColor:         flagNoColor,
            NoEmoji:         flagNoEmoji,
        })

        // Start bubbletea program
        p := tea.NewProgram(dash, tea.WithAltScreen())
        _, err := p.Run()
        return err
    },
}

func init() {
    dashboardCmd.Flags().DurationVar(&dashboardRefreshInterval, "refresh-interval", 2*time.Second, "Data refresh interval")
    dashboardCmd.Flags().DurationVar(&dashboardRPCTimeout, "rpc-timeout", 5*time.Second, "RPC call timeout")
}
```

**Register in:** `cmd/push-validator-manager/root_cobra.go`

```go
func init() {
    // ... existing commands ...

    rootCmd.AddCommand(dashboardCmd)  // Add this line
}
```

### Command Flags
- `--refresh-interval duration` (default: 2s)
- `--rpc-timeout duration` (default: 5s, min of 5s or 2×refresh-interval)
- `--remote-rpc string` (v2.1: optional separate RPC for remote height/latency measurement)
  - When set, queries this RPC for network height comparison
  - Enables "Blocks behind" calculation and latency display
  - Falls back to genesis domain if not specified
- Inherits all global flags (--home, --rpc, --no-color, --no-emoji, etc.)

### TTY Behavior & Cross-Platform Support

**Gracefully degrade when stdout is not a TTY:**

```go
// In cmd_dashboard.go
func dashboardRun(cmd *cobra.Command, args []string) error {
    cfg := loadCfg()

    // Check if stdout is a TTY
    if !term.IsTerminal(int(os.Stdout.Fd())) {
        // Not a TTY - render static snapshot and exit
        p := getPrinter()
        p.Warning("Dashboard requires a TTY terminal")
        p.Info("For non-interactive mode, use: push-validator-manager status")

        // Optionally print one static snapshot
        dash := dashboard.New(dashboard.Options{
            Config:          cfg,
            RefreshInterval: dashboardRefreshInterval,
            RPCTimeout:      dashboardRPCTimeout,
            NoColor:         true,
            NoEmoji:         true,
        })

        // Fetch data once
        ctx, cancel := context.WithTimeout(context.Background(), dashboardRPCTimeout)
        defer cancel()
        data, err := dash.FetchDataOnce(ctx)
        if err != nil {
            return err
        }

        // Print static view
        fmt.Println(dash.RenderStatic(data))
        return nil
    }

    // TTY available - launch interactive dashboard
    dash := dashboard.New(dashboard.Options{
        Config:          cfg,
        RefreshInterval: dashboardRefreshInterval,
        RPCTimeout:      dashboardRPCTimeout,
        NoColor:         flagNoColor || os.Getenv("NO_COLOR") != "",
        NoEmoji:         flagNoEmoji || os.Getenv("TERM") == "dumb",
    })

    // Use AltScreen only for proper TTY
    p := tea.NewProgram(dash, tea.WithAltScreen())
    _, err := p.Run()
    return err
}
```

**Why:** Dashboard works in CI/scripts (prints static snapshot) while providing rich TUI in terminals.

## 8. Keyboard Controls

### Essential Controls
- `q` / `Ctrl+C` - Quit dashboard
- `r` - Force refresh now
- `?` - Toggle help overlay

### Future-ready (extensible)
- `↑↓←→` / `tab` - Navigate between panels
- `enter` - Expand/focus selected panel
- `esc` - Return to overview
- `/` - Search/filter

## 9. Extensibility Guide

### Adding New Components

**Document in:** `internal/dashboard/README.md`

```markdown
# Adding New Dashboard Components

## Step 1: Create Component File

Create `internal/dashboard/components/my_component.go`:

```go
package components

import (
    tea "github.com/charmbracelet/bubbletea"
    "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/dashboard"
)

type MyComponent struct {
    title string
    data  MyData
}

func NewMyComponent() *MyComponent {
    return &MyComponent{title: "My Panel"}
}

// Implement Component interface
func (c *MyComponent) ID() string { return "my_component" }
func (c *MyComponent) Title() string { return c.title }
func (c *MyComponent) MinSize() (int, int) { return 30, 10 }

func (c *MyComponent) Init() tea.Cmd { return nil }

func (c *MyComponent) Update(msg tea.Msg, data dashboard.DashboardData) (dashboard.Component, tea.Cmd) {
    // Handle updates
    return c, nil
}

func (c *MyComponent) View(w, h int) string {
    // Render component with lipgloss
    return "My content"
}
```

## Step 2: Register Component

In `internal/dashboard/dashboard.go`:

```go
func New(opts Options) *Dashboard {
    // ... existing code ...

    // Register components
    registry.Register(components.NewHeader())
    registry.Register(components.NewNodeStatus())
    registry.Register(components.NewChainStatus())
    registry.Register(components.NewNetworkStatus())
    registry.Register(components.NewValidatorInfo())
    registry.Register(components.NewMyComponent())  // Add this

    // ...
}
```

## Step 3: Add to Layout

Configure layout in dashboard initialization:

```go
layout := LayoutConfig{
    Rows: []LayoutRow{
        {Components: []string{"header"}, Weights: []int{100}},
        {Components: []string{"node_status", "chain_status"}, Weights: []int{30, 70}},
        {Components: []string{"network_status", "validator_info"}, Weights: []int{30, 70}},
        {Components: []string{"my_component"}, Weights: []int{100}},  // Add this
    },
}
```

Done! Your component will appear in the dashboard.
```

## 10. Implementation Files

### New Files to Create

```
push-validator-manager-go/
├── internal/dashboard/
│   ├── dashboard.go           # Main dashboard model
│   ├── component.go           # Component interface & registry
│   ├── layout.go              # Layout manager
│   ├── types.go               # Shared types
│   ├── README.md              # Extension guide
│   └── components/
│       ├── header.go          # Header component
│       ├── node_status.go     # Node status panel
│       ├── chain_status.go    # Chain status panel
│       ├── network_status.go  # Network status panel
│       └── validator_info.go  # Validator info panel
└── cmd/push-validator-manager/
    └── cmd_dashboard.go       # Dashboard command
```

### Files to Modify

```
push-validator-manager-go/
├── go.mod                     # Add bubbletea + lipgloss
├── go.sum                     # Auto-generated
└── cmd/push-validator-manager/
    └── root_cobra.go          # Register dashboard command
```

## 11. Data Sources

### Existing Data (Reuse)
- `internal/metrics/collector.go` - Chain, network metrics
- `internal/process/supervisor.go` - Node status, PID
- `internal/node/client.go` - RPC queries

### New Data Needed
- **Uptime:** Track process start time (via supervisor)
- **Validator Info:** Query via Cosmos staking endpoints (with pagination)
- **Binary Version:** Cache `pchaind version` output (refresh every 5 min)

### Validator Info Pagination Strategy (Critical)

**IMPORTANT: Cosmos `/validators` endpoint is paginated. Must handle correctly:**

```go
// In internal/node/client.go - add new method
func (c *Client) FetchValidatorInfo(ctx context.Context, operatorAddr string) (ValidatorInfo, error) {
    // Strategy 1: Direct lookup if we know operator address
    // Endpoint: /cosmos/staking/v1beta1/validators/{validatorAddr}
    if operatorAddr != "" {
        resp, err := c.httpGet(ctx, fmt.Sprintf("/cosmos/staking/v1beta1/validators/%s", operatorAddr))
        if err == nil {
            return parseValidatorInfo(resp)
        }
    }

    // Strategy 2: Paginate through all validators (fallback)
    // Use conservative limit to keep UI snappy
    const maxPages = 5
    const pageSize = 100

    // Filter by status to reduce result set (bonded validators first)
    statuses := []string{"BOND_STATUS_BONDED", "BOND_STATUS_UNBONDING", "BOND_STATUS_UNBONDED"}

    for _, status := range statuses {
        nextKey := ""
        for page := 0; page < maxPages; page++ {
            url := fmt.Sprintf("/cosmos/staking/v1beta1/validators?pagination.limit=%d&status=%s", pageSize, status)
            if nextKey != "" {
                url += fmt.Sprintf("&pagination.key=%s", nextKey)
            }

            resp, err := c.httpGet(ctx, url)
            if err != nil {
                continue // Try next status
            }

            // Check if our validator is in this page
            valInfo, found := findOurValidator(resp, operatorAddr)
            if found {
                // Calculate voting power % using cached total
                totalVP, _ := d.getCachedTotalVotingPower(ctx)
                if totalVP > 0 {
                    valInfo.VotingPct = float64(valInfo.VotingPower) / float64(totalVP)
                }
                return valInfo, nil
            }

            // Get next page key
            nextKey = getNextPageKey(resp)
            if nextKey == "" {
                break // No more pages for this status
            }
        }
    }

    return ValidatorInfo{}, fmt.Errorf("validator not found in first %d pages", maxPages*len(statuses))
}
```

**Alternative approach if operator address unknown:**

```go
// Get our node's validator address from local keyring
func (d *Dashboard) getOurValidatorAddress(ctx context.Context) (string, error) {
    // Option 1: Query local keyring for validator key
    // pchaind keys show <key-name> --bech val

    // Option 2: Query node for validator pubkey, derive address
    // /validators endpoint returns validator pubkeys

    // Option 3: Store in config during registration
    // Most reliable for Phase 1
    return d.opts.Config.ValidatorAddress, nil
}
```

### Data Collection Pattern with Caching

```go
type Dashboard struct {
    // ... other fields ...

    // Caching for expensive operations
    cachedVersion       string
    cachedVersionAt     time.Time
    cachedVersionPID    int     // Invalidate when PID changes
    cachedValAddr       string
    cachedTotalVotingPower int64
    cachedTotalVPAt     time.Time
}

// Version caching - only fetch every 5 minutes OR when PID changes
// v2.1: Skip entirely when node not running (optimization)
func (d *Dashboard) getCachedVersion(ctx context.Context, running bool, currentPID int) string {
    // v2.1: Don't call pchaind version when node is stopped
    if !running {
        return "—" // Return placeholder when not running
    }

    // Invalidate cache if PID changed (process restarted)
    // v2.1: Immediate refetch after restart for updated version
    if currentPID != d.cachedVersionPID {
        d.cachedVersion = ""
        d.cachedVersionPID = currentPID
        d.cachedVersionAt = time.Time{} // Force immediate fetch
    }

    if time.Since(d.cachedVersionAt) < 5*time.Minute && d.cachedVersion != "" {
        return d.cachedVersion
    }

    // Fetch version (can be slow - 200-500ms typical)
    cmd := exec.CommandContext(ctx, "pchaind", "version")
    out, err := cmd.Output()
    if err == nil {
        d.cachedVersion = strings.TrimSpace(string(out))
        d.cachedVersionAt = time.Now()
    }

    return d.cachedVersion
}

// Total voting power caching - 5 min TTL
func (d *Dashboard) getCachedTotalVotingPower(ctx context.Context) (int64, error) {
    if time.Since(d.cachedTotalVPAt) < 5*time.Minute && d.cachedTotalVotingPower > 0 {
        return d.cachedTotalVotingPower, nil
    }

    // Fetch from /cosmos/staking/v1beta1/pool or similar
    cli := node.New(d.opts.Config.RPCLocal)
    total, err := cli.FetchTotalVotingPower(ctx)
    if err == nil {
        d.cachedTotalVotingPower = total
        d.cachedTotalVPAt = time.Now()
    }

    return total, err
}

// Validator info with pagination
func (d *Dashboard) fetchValidatorInfo(ctx context.Context) (ValidatorInfo, error) {
    // Get our validator address (cached or from config)
    if d.cachedValAddr == "" {
        addr, err := d.getOurValidatorAddress(ctx)
        if err != nil {
            return ValidatorInfo{}, err
        }
        d.cachedValAddr = addr
    }

    cli := node.New(d.opts.Config.RPCLocal)
    return cli.FetchValidatorInfo(ctx, d.cachedValAddr)
}
```

### Cross-Platform Considerations

**Ensure dashboard works everywhere:**

```go
// In cmd_dashboard.go
func initDashboard(cfg config.Config) (*dashboard.Dashboard, error) {
    // Check if stdout is a TTY
    if !term.IsTerminal(int(os.Stdout.Fd())) {
        return nil, fmt.Errorf("dashboard requires a TTY terminal (use 'status' command instead)")
    }

    opts := dashboard.Options{
        Config:          cfg,
        RefreshInterval: dashboardRefreshInterval,
        NoColor:         flagNoColor || os.Getenv("NO_COLOR") != "",
        NoEmoji:         flagNoEmoji || os.Getenv("TERM") == "dumb",
    }

    return dashboard.New(opts), nil
}

// In dashboard.go - AltScreen only for TTY
func (d *Dashboard) Run() error {
    teaOpts := []tea.ProgramOption{}

    // Only use AltScreen if we have a proper TTY
    if term.IsTerminal(int(os.Stdout.Fd())) {
        teaOpts = append(teaOpts, tea.WithAltScreen())
    }

    p := tea.NewProgram(d, teaOpts...)
    _, err := p.Run()
    return err
}
```

**Platform-specific border handling:**

```go
// In components - respect --no-color for ASCII borders
func (c *BaseComponent) borderStyle(noColor bool) lipgloss.Border {
    if noColor || os.Getenv("TERM") == "dumb" {
        // ASCII-only borders for compatibility
        return lipgloss.Border{
            Top:         "-",
            Bottom:      "-",
            Left:        "|",
            Right:       "|",
            TopLeft:     "+",
            TopRight:    "+",
            BottomLeft:  "+",
            BottomRight: "+",
        }
    }

    // Unicode rounded borders
    return lipgloss.RoundedBorder()
}
```

## 12. Testing Strategy

### Unit Tests
- Each component: `*_test.go` files
- Layout engine: various terminal sizes
- Component registry: add/remove/lookup

### Integration Tests
- Full dashboard initialization
- Data flow through components
- Layout rendering

### Manual Testing Checklist
- [ ] Terminal resize handling
- [ ] Slow/failed RPC responses
- [ ] No validator registered scenario
- [ ] Network disconnection
- [ ] Very small terminal (80x24)
- [ ] Very large terminal (200x60)
- [ ] Color/emoji disabled modes
- [ ] Non-TTY output (pipes, CI)

### Golden Tests

**Create snapshot tests with fixed inputs:**

```go
// layout_test.go
func TestLayout_OddWidths(t *testing.T) {
    // Test remainder distribution with odd widths
    widths := []int{101, 127, 99, 83}

    for _, w := range widths {
        result := layout.Compute(w, 40)
        // Verify no gaps: sum of cell widths == total width
        total := 0
        for _, cell := range result.Cells {
            total += cell.W
        }
        assert.Equal(t, w, total, "width mismatch for %d cols", w)
    }
}

// component_test.go
// v2.1: Use injectable timeNow for deterministic tests
var timeNow = time.Now // Default to real time

func TestComponent_GoldenSnapshot(t *testing.T) {
    // Use fixed time for deterministic output
    now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
    timeNow = func() time.Time { return now }
    defer func() { timeNow = time.Now }() // Restore

    comp := NewNodeStatus()
    comp.Update(testData)

    // Render with NO_COLOR=1
    os.Setenv("NO_COLOR", "1")
    output := comp.View(80, 10)

    // Compare with golden file
    goldenFile := "testdata/node_status_golden.txt"
    if *update {
        os.WriteFile(goldenFile, []byte(output), 0644)
    }
    golden, _ := os.ReadFile(goldenFile)
    assert.Equal(t, string(golden), output)
}

// v2.1: Layout gap tests - ensure no width/height gaps
func TestLayout_NoWidthGaps(t *testing.T) {
    widths := []int{80, 101, 127, 200}

    for _, w := range widths {
        result := layout.Compute(w, 40)

        // Row 1: header (full width)
        assert.Equal(t, w, result.Cells[0].W, "header width mismatch")

        // Row 2: node_status + chain_status
        row2Total := result.Cells[1].W + result.Cells[2].W
        assert.Equal(t, w, row2Total, "row 2 width gap at %d cols", w)

        // Row 3: network_status + validator_info
        row3Total := result.Cells[3].W + result.Cells[4].W
        assert.Equal(t, w, row3Total, "row 3 width gap at %d cols", w)
    }
}

func TestLayout_NoHeightGaps(t *testing.T) {
    heights := []int{24, 30, 40, 60}

    for _, h := range heights {
        result := layout.Compute(80, h)

        // Sum all row heights
        totalHeight := 0
        for _, cell := range result.Cells {
            if cell.Y+cell.H > totalHeight {
                totalHeight = cell.Y + cell.H
            }
        }

        // Should utilize full terminal height (with vertical slack distribution)
        assert.Equal(t, h, totalHeight, "height gap at %d rows", h)
    }
}

// v2.1: Truncation utility tests
func TestTruncateWithEllipsis(t *testing.T) {
    tests := []struct {
        input    string
        maxLen   int
        expected string
    }{
        {"short", 10, "short"},
        {"exactly10", 10, "exactly10"},
        {"toolongstring", 10, "toolongst…"},
        {"unicode测试文本", 8, "unicode…"},
        {"x", 1, "…"},
        {"test", 0, ""},
        {"", 5, ""},
    }

    for _, tt := range tests {
        got := truncateWithEllipsis(tt.input, tt.maxLen)
        assert.Equal(t, tt.expected, got, "truncate(%q, %d)", tt.input, tt.maxLen)
    }
}
```

**Why:** Prevent visual regressions, validate layout algorithm correctness, ensure utility functions handle edge cases.

## 13. Success Criteria

- ✅ Dashboard launches with 5 panels (header + 4 data panels)
- ✅ Real-time updates every 2s
- ✅ Responsive to terminal resize
- ✅ Keyboard controls work (q, r, ?)
- ✅ Graceful error handling (network failures, no validator, etc.)
- ✅ Clear extension pattern demonstrated
- ✅ Reuses existing UI/metrics code (no duplication)
- ✅ No breaking changes to existing commands
- ✅ Works with --no-color and --no-emoji flags
- ✅ Documentation for adding components

## 14. Future Extension Examples

### Example 1: Log Feed Component

```go
// Add to components/log_feed.go
type LogFeedComponent struct {
    lines []string
}

func (c *LogFeedComponent) View(w, h int) string {
    // Show last N log lines, scrollable
    return renderLogFeed(c.lines, w, h)
}

// Register and add to layout - done!
```

### Example 2: Performance Metrics Component

```go
// Add to components/performance.go
type PerformanceComponent struct {
    blockTimes []time.Duration
    txCounts   []int
}

func (c *PerformanceComponent) View(w, h int) string {
    // Show avg block time, tx throughput
    return renderPerformanceMetrics(c.blockTimes, c.txCounts, w, h)
}
```

### Example 3: Alert/Warning Component

```go
// Add to components/alerts.go
type AlertsComponent struct {
    alerts []Alert
}

func (c *AlertsComponent) View(w, h int) string {
    // Show important warnings/issues
    return renderAlerts(c.alerts, w, h)
}
```

All follow the same pattern - no framework changes needed!

## 15. Implementation Order

1. **Dependencies:** Add bubbletea + lipgloss to go.mod
2. **Core framework:** `component.go`, `types.go`, `layout.go`
3. **Main dashboard:** `dashboard.go` with basic structure
4. **Header component:** Simple static header
5. **Node status component:** Reuse existing process.Supervisor
6. **Chain status component:** Reuse existing metrics.Snapshot
7. **Network status component:** Reuse existing metrics.Snapshot
8. **Validator info component:** New RPC queries
9. **Dashboard command:** `cmd_dashboard.go` + registration
10. **Testing:** Unit tests for each component
11. **Documentation:** README.md extension guide

## 16. Timeline Estimate

- **Dependencies + Core Framework:** 2-3 hours
- **Components (5 total):** 6-8 hours (1-1.5h each)
- **Dashboard Command + Integration:** 2-3 hours
- **Testing + Polish:** 3-4 hours
- **Documentation:** 1-2 hours

**Total:** ~15-20 hours for complete Phase 1

---

## 17. Comprehensive Risk Checklist (Ship-Blockers)

**These MUST be verified before Phase 1 release:**

### Core Correctness
- [ ] **Tea.Tick pattern implemented** (not goroutines) - prevents race conditions
- [ ] **Typed messages** (tickMsg, dataMsg, dataErrMsg, fetchStartedMsg) - deterministic flow
- [ ] **fetchStartedMsg pattern** - cancel func assigned on UI thread (not in Cmd goroutine)
- [ ] **tea.Sequence** used to emit fetchStartedMsg then result
- [ ] **Context cancellation** on quit/refresh - no hanging RPC calls
- [ ] **No panics** on terminal resize (tested <80×24, >200×60)
- [ ] **RPC timeouts** < refresh interval (5s timeout, 2s refresh)
- [ ] **Single HTTP client** with proper Transport timeouts

### Validator Data
- [ ] **Pagination implemented** for Cosmos staking endpoints
- [ ] **Direct lookup** via operator address (primary strategy)
- [ ] **Fallback pagination** with status filtering (bonded → unbonding → unbonded)
- [ ] **Page limit** (max 5 pages × 100 validators × 3 statuses)
- [ ] **Validator address** stored in config or derived correctly
- [ ] **Voting power %** calculated using cached total VP (5 min TTL)
- [ ] **Graceful handling** when validator not found (show "Not registered")

### Performance
- [ ] **Hash-based caching** prevents unnecessary re-renders
- [ ] **Version caching** (5 min TTL, not every 2s)
- [ ] **PID change invalidates** cached version immediately
- [ ] **Total voting power cached** with 5 min TTL
- [ ] **Moving average ETA** prevents flapping (10 samples)
- [ ] **ETA threshold** (< 0.01 blocks/s shows "calculating...")
- [ ] **Progress bar** clamped to 0-100%
- [ ] **No blocking** in Update() - all I/O in Cmd functions
- [ ] **First-load spinner** with centered message

### Error Handling
- [ ] **Stale data** shown with warning banner in header (> 10s old)
- [ ] **Previous data** retained on fetch errors
- [ ] **Error messages** displayed in header (truncated to terminal width)
- [ ] **Network failures** don't crash dashboard
- [ ] **Missing validator** doesn't fail entire refresh
- [ ] **Loading state** shows spinner (not blank screen)

### Cross-Platform
- [ ] **TTY detection** works (graceful degradation in CI/pipes)
- [ ] **Non-TTY** prints static snapshot and exits 0
- [ ] **AltScreen** only enabled for proper TTY
- [ ] **--no-color** produces readable ASCII borders
- [ ] **--no-emoji** uses text-only icons (Icons struct)
- [ ] **TERM=dumb** falls back to ASCII mode
- [ ] **Windows** tested (or --ascii flag provided)

### Layout
- [ ] **LayoutResult** with Cell[] prevents render mismatches
- [ ] **MinWidth/MinHeight** honored before weight distribution
- [ ] **Remainder distribution** fair (sorted by fractional parts)
- [ ] **No width gaps** (sum of cell widths == terminal width)
- [ ] **Component dropping** reflected in LayoutResult.Cells
- [ ] **Layout warning** shown when components hidden
- [ ] **Header fixed height** (3 lines reserved)
- [ ] **Size changes** handled without panic

### Keyboard & UX
- [ ] **q/Ctrl+C** quits immediately
- [ ] **r** forces refresh (cancels in-flight fetch)
- [ ] **?** toggles help overlay
- [ ] **Keymap** centralized for easy help generation
- [ ] **Help text** accurate and up-to-date

### Testing
- [ ] **Unit tests** for each component (at least 1 per panel)
- [ ] **Layout tests** with various sizes (80×24, 120×40, 200×60)
- [ ] **Mock data** for offline testing
- [ ] **Error scenarios** tested (RPC down, validator missing)
- [ ] **Resize handling** tested interactively

### Documentation
- [ ] **Extension guide** in dashboard/README.md
- [ ] **Component template** provided
- [ ] **Example** of adding new component
- [ ] **Keymap usage** documented
- [ ] **Troubleshooting** section for common issues

## 18. Updated File Structure

```
push-validator-manager-go/
├── go.mod                     # Add: bubbletea, lipgloss, bubbles, xxhash
├── go.sum                     # Auto-generated
├── internal/
│   └── dashboard/
│       ├── dashboard.go       # Main model (Tea.Tick pattern)
│       ├── component.go       # Component interface & registry
│       ├── layout.go          # Responsive layout with MinSize
│       ├── types.go           # Message types (tickMsg, dataMsg, etc.)
│       ├── util.go            # Formatting utilities (HumanInt, ProgressBar, etc.)
│       ├── README.md          # Extension guide
│       └── components/
│           ├── header.go      # Header with timestamp + error
│           ├── node_status.go # BaseComponent + caching
│           ├── chain_status.go
│           ├── network_status.go
│           └── validator_info.go  # With pagination
├── cmd/push-validator-manager/
│   ├── cmd_dashboard.go       # Dashboard command (TTY check, keymap)
│   └── root_cobra.go          # Register dashboard command
└── internal/node/
    └── client.go              # Add: FetchValidatorInfo with pagination
```

## 19. Implementation Priorities (Ordered)

**Phase 1A: Foundation (Week 1)**
1. Dependencies + util.go
2. types.go (messages)
3. component.go (interface + BaseComponent with caching)
4. layout.go (MinSize + weights)
5. dashboard.go (Tea.Tick pattern, fetchCmd)

**Phase 1B: Components (Week 1-2)**
6. header.go (timestamp, error display)
7. node_status.go (reuse process.Supervisor)
8. chain_status.go (reuse metrics + ETA calculation)
9. network_status.go (reuse metrics)
10. validator_info.go (NEW: pagination implementation)

**Phase 1C: Integration (Week 2)**
11. cmd_dashboard.go (TTY check, keymap, cross-platform)
12. Validator pagination in node/client.go
13. Version caching in dashboard.go

**Phase 1D: Testing (Week 2-3)**
14. Unit tests (components, layout, util)
15. Manual testing (resize, errors, platforms)
16. Documentation (README.md extension guide)

## Notes

- All components are self-contained and testable
- Layout system supports any number of components
- Data collection is centralized and efficient
- Existing code is reused wherever possible
- Extension path is clear and simple
- No breaking changes to existing functionality
- **Production-ready**: Incorporates Bubble Tea best practices
- **Performance optimized**: Hash-based caching, version caching, no blocking
- **Robust error handling**: Stale data display, graceful degradation
- **Cross-platform**: TTY detection, ASCII fallbacks, Windows-compatible
