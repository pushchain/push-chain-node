# Push Validator Manager - UX Enhancement Plan

## 🎯 Vision
Transform the Push Validator Manager into a professional, intuitive tool with exceptional UX, featuring a real-time TUI dashboard similar to htop for continuous monitoring.

---

## ✅ Current Progress (live tracking)
Status of Phase 0 quick wins implemented in code:

- [x] Fix `go.mod` to `go 1.22`
- [x] Add `status --watch` alias (reuses sync monitor)
- [x] Enhance `status` text formatting with colors/icons
- [ ] Consolidate printer (`cmd/.../printer.go` → `internal/ui/printer.go`)
- [ ] Standardize error handling (prefer `RunE` and return errors over `os.Exit`)

Notes:
- `status --watch` supports `--compact`, `--window`, `--rpc`, `--remote` consistent with sync monitor.
- Interval configuration is not exposed yet (monitor ticks at 1s); see Phase 2 verification.

---

## 🚨 PHASE 0: Critical Fixes & Foundation Alignment (Day 1-2)
*Address immediate issues identified in code review before building new features*

### 0.1 Critical Fixes
**Immediate actions required:**
- **Fix go.mod version**: Change from `go 1.23.6` to `go 1.22` (invalid version blocking builds)
- **Consolidate printer architecture**: Move `cmd/push-validator-manager/printer.go` → `internal/ui/printer.go`
- **Standardize error handling**: Use `RunE` consistently across all commands (return errors, not os.Exit)
- **Add status --watch alias**: Map to existing sync monitor functionality

### 0.2 Architecture Alignment
**Align with existing codebase strengths:**
- Preserve robust process supervision in `internal/process/supervisor`
- Leverage existing sync monitor (`internal/sync/monitor`) for dashboard
- Maintain WebSocket subscription patterns from `internal/node`
- Keep bootstrap flow intact from `internal/bootstrap`

### 0.3 Quick Wins
**High impact, low effort improvements:**
```bash
# These can be done immediately
1. Fix go.mod version (5 minutes)
2. Create unified internal/ui/printer.go (1 hour)
3. Add status --watch alias (15 minutes)
4. Enhance status formatting with colors (30 minutes)
```

**Deliverables:**
- ✅ Valid go.mod for builds
- ✅ Unified printer module location
- ✅ Consistent error handling
- ✅ Foundation for color system

### 0.5 Verification
- Build: `go build ./...` succeeds
- Status: `push-validator-manager status --output json` prints structured fields
- Watch: `push-validator-manager status --watch --compact --window 20` runs the monitor

---

## 📋 PHASE 1: Foundation (Week 1)
*Establish core UX infrastructure and consistent design system*

### 1.1 Color System & Theme Engine
**File:** `internal/ui/colors.go`
- Semantic color palette (success, warning, error, info)
- Status indicators (✓ ⚠ ✗ ℹ ● ○)
- NO_COLOR environment variable support
- Terminal capability detection
- Theme configuration struct

### 1.2 Enhanced Printer Module
**File:** `internal/ui/printer.go`
- **IMPORTANT:** Consolidate existing `cmd/push-validator-manager/printer.go` here
- Extends current printer with color support integration
- Formatted output helpers (tables, boxes, separators)
- Progress bars and spinners
- Status icons and badges
- Respect NO_COLOR, TTY detection, and --output flags

### 1.3 Improved Help System
**File:** `cmd/push-validator-manager/help.go`
- Command grouping (Quick Start, Operations, Validator, Maintenance)
- Rich command descriptions with examples
- Context-aware "Next steps" suggestions
- Common workflows section

### 1.4 Better Error Handling
**File:** `internal/ui/errors.go`
- Structured error messages with:
  - Clear problem statement
  - Possible causes
  - Actionable solutions
  - Relevant commands to try
- Error recovery suggestions

**Deliverables:**
- Colorized output for all commands
- Organized help with examples
- Better error messages
- Consistent visual language

---

## 📊 PHASE 2: Enhanced Status & Monitoring (Week 2)
*Improve static status display and add monitoring capabilities*

### 2.1 Rich Status Display
**Update:** `cmd/push-validator-manager/cmd_status.go`
```
╭──── PUSH VALIDATOR STATUS ─────────╮
│ Node:      ✅ Running (pid 1234)   │
│ RPC:       ✅ Listening (:26657)    │
│ Sync:      ⚠️  78.5% (ETA: 15 min)  │
│ Height:    125,432 / 160,000       │
│ Peers:     12 connected            │
╰─────────────────────────────────────╯
```

### 2.2 Sync Progress Enhancement
**Update:** `cmd/push-validator-manager/cmd_sync.go` (refactor from current wiring in `root_cobra.go`)
- Real-time progress bar with percentage
- Block rate calculation
- ETA computation
- Network health indicators
- Peer connection status

### 2.3 Metrics Collection
**File:** `internal/metrics/collector.go`
- System metrics (CPU, RAM, Disk)
- Network metrics (peers, bandwidth)
- Blockchain metrics (height, sync status)
- Validator metrics (if applicable)

### 2.4 Formatted Tables
**File:** `internal/ui/tables.go`
- Validators list with sorting
- Balance display with denominations
- Peer list with connection quality

**Deliverables:**
- Beautiful status output
- Progress bars for sync
- Metrics collection system
- Formatted tables for lists

### 2.5 Verification
- `push-validator-manager status` shows colored block with Node/RPC/Sync/Height
- `push-validator-manager sync --window 60` renders stable bar and ETA
- Non-TTY: piping to file produces plain-text lines without ANSI escapes

---

## 🖥️ PHASE 3: TUI Dashboard (Week 3-4)
*Implement real-time monitoring dashboard*

### 3.1 TUI Framework Setup
**Dependencies:**
```go
github.com/charmbracelet/bubbletea
github.com/charmbracelet/lipgloss
github.com/charmbracelet/bubbles
```

### 3.2 Dashboard Core
**File:** `internal/ui/dashboard/dashboard.go`
- Main dashboard model
- Update loop (2-5 second refresh)
- Component layout manager
- Keyboard event handling

### 3.3 Dashboard Components
**File:** `internal/ui/dashboard/components.go`

#### Dashboard Layout
```
╭──────────────── PUSH VALIDATOR MONITOR ─────────────────╮
│ Uptime: 2d 14h 32m          Refresh: 2s      [Q]uit     │
├──────────────────────────────────────────────────────────┤
│ NODE STATUS                                              │
│ ├─ Process:    ● Running (PID: 45231)                   │
│ ├─ CPU:        ████░░░░░░ 42% (4 cores)                 │
│ ├─ Memory:     ██████░░░░ 61% (3.8GB/6.2GB)            │
│ └─ Disk:       ███░░░░░░░ 34% (142GB/420GB)            │
├──────────────────────────────────────────────────────────┤
│ BLOCKCHAIN SYNC                                          │
│ ├─ Status:     ⚠ Catching Up                            │
│ ├─ Progress:   ████████░░ 78.5%                         │
│ ├─ Height:     125,432 / 160,000                        │
│ ├─ Rate:       ~120 blocks/sec                          │
│ └─ ETA:        ~4 min 32 sec                            │
├──────────────────────────────────────────────────────────┤
│ NETWORK                                                  │
│ ├─ Peers:      12/50 connected                          │
│ ├─ Inbound:    ▼ 1.2 MB/s                               │
│ ├─ Outbound:   ▲ 0.8 MB/s                               │
│ └─ Latency:    23ms (good)                              │
├──────────────────────────────────────────────────────────┤
│ VALIDATOR (if registered)                                │
│ ├─ Status:     ✅ Active                                 │
│ ├─ Voting:     124,892 / 124,892 (100%)                 │
│ ├─ Proposed:   12 blocks                                │
│ └─ Balance:    1,500.25 PUSH                            │
├──────────────────────────────────────────────────────────┤
│ RECENT LOGS                                              │
│ 14:32:01 INF Executed block height=125432               │
│ 14:32:00 INF Committed state height=125431              │
│ 14:31:59 INF Indexed block height=125430                │
╰──────────────────────────────────────────────────────────╯
[L]ogs  [R]estart  [S]top  [B]ackup  [H]elp  [Q]uit
```

#### Component Details

**a) Header Component**
- Title with branding
- Uptime counter
- Refresh rate indicator
- Quick exit option

**b) Node Status Panel**
- Process status (PID, uptime)
- Resource usage bars (CPU, RAM, Disk)
- Visual health indicators
- Resource alerts when thresholds exceeded

**c) Blockchain Panel**
- Sync progress with animation
- Current/target height
- Blocks per second calculation
- Time to completion estimate

**d) Network Panel**
- Peer count with min/max recommendations
- Bandwidth usage (inbound/outbound)
- Network latency measurement
- Connection quality indicator

**e) Validator Panel** (conditional)
- Validator registration status
- Voting participation rate
- Blocks proposed counter
- Balance and rewards tracking

**f) Logs Panel**
- Last 5-10 log entries
- Scrollable with keyboard
- Severity-based color coding
- Auto-scroll option

**g) Alerts Panel** (when active)
```
┌─ ALERTS ─────────────────────────────────────┐
│ ⚠ Disk space low (< 100GB free)             │
│ ⚠ Peer count below recommended (< 10)       │
└──────────────────────────────────────────────┘
```

### 3.4 Dashboard Command
**File:** `cmd/push-validator-manager/cmd_dashboard.go`

**IMPORTANT Implementation Notes:**
- `status --watch` should alias to dashboard (matches documentation)
- Reuse existing `internal/sync/monitor` for data source
- Use existing WebSocket subscription from `internal/node`
- Start with minimal Bubble Tea model, add components incrementally
- Use build tags for soft dependency to keep base binary lean

### 3.4.1 Build Tags (explicit)
- Tag name: `dashboard`
- Default build (no tag): `status --watch` uses line-based monitor (current implementation)
- With tag (`-tags dashboard`): enables Bubble Tea dashboard UI
```bash
# Examples
go build -tags dashboard ./...
push-validator-manager status --watch
```

```bash
# Launch full dashboard
push-validator-manager status --watch

# Compact mode for smaller terminals
push-validator-manager status --watch --compact

# Update interval customization
push-validator-manager status --watch --interval 5s
```

**TUI Implementation Strategy:**
1. Start minimal - just render status panels
2. Pull metrics from existing `node.Status()` calls
3. Reuse WebSocket header subscription for live updates
4. Add gopsutil metrics later (keep as optional dependency)
5. Respect NO_COLOR and non-TTY environments

### 3.5 Keyboard Navigation
- `q` / `Ctrl+C` - Quit dashboard
- `p` - Pause/Resume updates
- `l` - Switch to full logs view
- `r` - Quick restart (with confirmation)
- `s` - Quick stop (with confirmation)
- `h` / `?` - Show help overlay
- `↑↓` - Navigate/scroll logs
- `Tab` - Switch between panels
- `d` - Toggle detailed/compact view

**Deliverables:**
- Full TUI dashboard with real-time updates
- Multiple view modes (full, compact)
- Keyboard navigation
- Resource monitoring
- Interactive controls

### 3.6 Verification
- Terminal resize does not crash; layout reflows
- `q` exits cleanly and restores cursor visibility
- CPU usage under 5% on idle dashboard

---

## 🎮 PHASE 4: Interactive Features (Week 5)
*Add interactivity and smart assistance*

### 4.1 Interactive Prompts
**File:** `internal/ui/prompts.go`
- Yes/No confirmations with defaults
- Text input with validation
- Single/Multi select menus
- Password input (masked)
- Progress feedback during operations

Example:
```
⚠️  Stop Validator Node?
This will stop the validator process.
Continue? (y/N): _
```

### 4.2 Command Suggestions
**File:** `internal/ui/suggestions.go`
- Context-aware hints based on state
- Command completion helpers
- "Did you mean?" for typos
- Next logical step suggestions

Examples:
```
After init:  → Next: Run 'push-validator-manager start' to begin
During sync: → Monitor with 'push-validator-manager sync'
On error:    → Try 'push-validator-manager logs' for details
```

### 4.3 Installation Wizard
**Integration with:** `install.sh`
```
🚀 Push Validator Manager Setup
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Prerequisites Check
  ✅ Go 1.21+ installed
  ✅ Git available
  ✅ Network connectivity
  ✅ Sufficient disk space (150GB)

Installation Progress
  [1/5] Building manager...       ████████░░ 80%
  [2/5] Downloading pchaind...    ⏳
  [3/5] Initializing node...      ⏳
  [4/5] Configuring network...    ⏳
  [5/5] Starting sync...          ⏳

Current: Compiling Go binary (push-validator-manager)...
Time elapsed: 2m 14s
```

### 4.4 Confirmation Dialogs
- Show current state before changes
- Highlight dangerous operations
- Provide undo/recovery information
- Include estimated impact

**Deliverables:**
- Interactive prompt system
- Smart command suggestions
- Enhanced installation wizard
- Safety confirmations

---

## 🚀 PHASE 5: Advanced Features (Week 6)
*Polish and power-user features*

### 5.1 Debug Mode
**Flag:** `--debug` or `-d`
```bash
push-validator-manager --debug status
```
Shows:
- Verbose RPC logging
- Request/response details
- Performance timing
- System calls trace

### 5.2 Output Formats
```bash
# JSON output for automation
push-validator-manager status --output json

# YAML for configuration
push-validator-manager status --output yaml

# CSV for spreadsheets
push-validator-manager validators --output csv

# Quiet mode for scripts
push-validator-manager status --quiet
```

### 5.3 Configuration Management
**File:** `internal/config/ui_config.go`
```yaml
# ~/.pchain/ui.conf
theme: default
dashboard:
  refresh_rate: 2s
  show_alerts: true
  compact_logs: false
colors:
  enabled: true
  scheme: default
```

### 5.4 Batch Operations
**File:** `cmd/push-validator-manager/cmd_batch.go`
```yaml
# ops.yaml
operations:
  - command: stop
  - wait: 5s
  - command: backup
  - command: reset
    confirm: true
  - command: init
    args:
      moniker: "my-validator"
  - command: start
```

```bash
# Execute batch
push-validator-manager batch --file ops.yaml

# Interactive batch mode
push-validator-manager batch --interactive
```

### 5.5 Shell Completion
- Bash: `push-validator-manager completion bash`
- Zsh: `push-validator-manager completion zsh`
- Fish: `push-validator-manager completion fish`
- PowerShell: `push-validator-manager completion powershell`

**Deliverables:**
- Debug capabilities
- Multiple output formats
- User preferences system
- Batch operations
- Shell completions

---

## 🔄 PHASE 6: Integration & Polish (Week 7)
*Final integration and quality improvements*

### 6.1 Unified Experience
- Consistent colors across all commands
- Standardized error handling
- Uniform progress indicators
- Cohesive keyboard shortcuts

### 6.2 Performance Optimization
- Efficient metric collection (caching)
- Optimized TUI rendering (partial updates)
- Reduced RPC calls (batching)
- Memory-efficient log handling

### 6.3 Documentation
- Comprehensive README
- Man pages for each command
- Interactive help system
- Common troubleshooting guide
- Video tutorials (external)

### 6.4 Testing Strategy
- Unit tests for UI components
- Integration tests for flows
- Terminal emulation tests
- Cross-platform validation (macOS, Linux, WSL)
- Performance benchmarks

### 6.5 Accessibility
- Screen reader compatibility
- High contrast mode option
- Keyboard-only navigation
- Clear focus indicators
- Descriptive error messages

**Deliverables:**
- Polished, cohesive UX
- Optimized performance
- Complete documentation
- Test coverage
- Accessibility compliance

---

## 🏗️ Code Architecture Alignment

### Existing Code Strengths to Preserve
Based on code review, these components are well-designed and should be leveraged:

1. **Process Supervision** (`internal/process/supervisor`)
   - Robust start/stop/restart with PID management
   - Proper signal handling (Unix)
   - State-sync bootstrapping logic
   - Keep this as-is, just add color to output

2. **Sync Monitor** (`internal/sync/monitor`)
   - TTY detection already implemented
   - WebSocket header tracking working well
   - Moving average rate calculation
   - Reuse for dashboard data source

3. **RPC/WebSocket Client** (`internal/node`)
   - Clean separation of concerns
   - Proper WebSocket close handshake
   - Strong test coverage
   - Use for dashboard metrics collection

4. **Bootstrap Flow** (`internal/bootstrap`)
   - Genesis download logic solid
   - Peer discovery with fallback
   - State sync trust params working
   - Enhance with progress indicators only

### Architecture Improvements Needed

1. **Config Management**
   - Externalize hardcoded values (chain ID, domains)
   - Centralize via an embedded JSON file: `internal/config/chain_metadata.json` using `//go:embed`
   - Merge order: Defaults → embedded JSON → env → flags
   - Keep backward compatibility with existing env vars
   - Provide `--chain-config` to override the embedded defaults

2. **TOML Editing**
   - Current: Regex-based (works but brittle)
   - Future: Migrate to `pelletier/go-toml`
   - For now: Add comprehensive tests

3. **Platform Support**
   - Current: Unix-oriented (signals, paths)
   - If Windows needed: Add build tags
   - WSL works fine as-is

4. **Output Format Abstraction**
   - Current: Only json|text
   - Add abstraction layer for yaml/csv/quiet
   - Keep backward compatibility
   - Non-TTY default remains text; `--quiet` suppresses non-essential lines

---

## 🧪 Testing Strategy

### Current Test Coverage
**Well Tested:**
- WebSocket client (`internal/node`)
- Sync monitor (`internal/sync`)
- State sync provider (`internal/statesync`)

### Testing Gaps to Fill

1. **Golden Tests for Formatting**
   ```go
   // internal/ui/printer_test.go
   func TestStatusFormatting(t *testing.T) {
       // Compare output against golden files
   }
   ```

2. **ConfigStore Mutations**
   ```go
   // internal/files/configstore_test.go
   func TestSetInSection(t *testing.T) {
       // Test all edge cases for TOML editing
   }
   ```

3. **Command Layer Testing**
   ```go
   // cmd/push-validator-manager/*_test.go
   - Test help output formatting
   - Test error handling consistency
   - Test flag parsing
   ```

4. **TUI Dashboard Testing**
   - Use `teatest` package for Bubble Tea
   - Test keyboard navigation
   - Test component updates
   - Mock metrics collection

### Testing Approach
1. **Unit tests**: 80% coverage target for new code
2. **Integration tests**: Key flows (init, start, sync)
3. **Golden tests**: All formatted output
4. **Cross-platform**: macOS, Linux, WSL
5. **Performance**: Dashboard < 5% CPU benchmark

---

## 📦 Implementation Details

### Dependencies
```go
// go.mod additions
require (
    github.com/charmbracelet/bubbletea v0.25.0
    github.com/charmbracelet/lipgloss v0.9.1
    github.com/charmbracelet/bubbles v0.17.1
    // Optional: only if switching away from existing ANSI utilities
    // github.com/fatih/color v1.16.0
    github.com/olekukonko/tablewriter v0.0.5
    github.com/AlecAivazis/survey/v2 v2.3.7
    github.com/shirou/gopsutil/v3 v3.23.12  // System metrics
)
```

### File Structure
```
internal/
├── ui/
│   ├── colors.go          # Color system & themes
│   ├── printer.go         # Enhanced formatted output
│   ├── errors.go          # Error formatting & suggestions
│   ├── tables.go          # Table formatting utilities
│   ├── prompts.go         # Interactive prompts
│   ├── suggestions.go     # Smart command suggestions
│   ├── progress.go        # Progress bars & spinners
│   └── dashboard/
│       ├── dashboard.go   # Main TUI controller
│       ├── components.go  # UI component definitions
│       ├── styles.go      # Lipgloss style definitions
│       ├── layout.go      # Layout management
│       └── events.go      # Event handling
├── metrics/
│   ├── collector.go       # Metrics collection service
│   ├── system.go          # System resource metrics
│   ├── blockchain.go      # Blockchain-specific metrics
│   └── network.go         # Network metrics
└── config/
    └── ui_config.go       # UI preferences management

cmd/push-validator-manager/
├── cmd_dashboard.go       # Dashboard command implementation
├── cmd_batch.go           # Batch operations command
├── help.go                # Enhanced help system
└── (existing commands updated with new UI)

---

## 🧭 Task Breakdown (actionable)

### Phase 0
- [ ] Move `cmd/.../printer.go` → `internal/ui/printer.go` and update imports
- [ ] Change command handlers to return errors (`RunE`) instead of `os.Exit`

### Phase 1
- [ ] Implement `internal/ui/errors.go` with structured guidance
- [ ] Add `cmd/push-validator-manager/help.go` with grouped help and examples

### Phase 2
- [ ] Extract `cmd_sync.go` from `root_cobra.go`; add `--interval` flag to monitor
- [ ] Add `internal/ui/tables.go` for validators/peers tables

### Phase 3
- [ ] Add `internal/ui/dashboard/*` with build tag `dashboard`
- [ ] Wire `status --watch` to Bubble Tea when built with tag

### Testing
- [ ] Golden tests for status formatting (`internal/ui/printer_test.go`)
- [ ] Tests for configstore mutations, backup, and command flag parsing
```

---

## 🎯 Success Metrics

### User Experience
- **Clarity**: Information immediately understandable
- **Efficiency**: Common tasks < 3 commands
- **Delight**: Professional, polished feel
- **Reliability**: Consistent behavior, clear feedback

### Technical
- **Performance**: Dashboard < 5% CPU usage
- **Responsiveness**: UI updates < 100ms
- **Compatibility**: Works on 95% of terminals
- **Stability**: Zero crashes in normal operation

### Adoption
- **Learning Curve**: New users productive in < 5 minutes
- **Documentation**: All features documented with examples
- **Support**: Common issues self-resolvable

---

## 🚦 Risk Mitigation

### Technical Risks
1. **Terminal Compatibility**
   - Test on: iTerm2, Terminal.app, GNOME Terminal, Windows Terminal, WSL
   - Fallback to simple mode when features unavailable

2. **Performance Impact**
   - Profile dashboard CPU/memory usage
   - Implement update throttling
   - Add caching layer for metrics

3. **Dependency Management**
   - Vendor critical dependencies
   - Minimize external library usage
   - Regular security audits

### User Experience Risks
1. **Complexity Creep**
   - Focus on essential features
   - Hide advanced options
   - Progressive disclosure

2. **Breaking Changes**
   - Maintain core command structure
   - Add new features as flags/subcommands
   - Clear migration guides

---

## 🏁 Definition of Done

Each phase is complete when:
1. All code implemented and tested
2. Documentation updated
3. Integration tests passing
4. Performance benchmarks met
5. Accessibility verified
6. Cross-platform tested

---

## 🚀 Quick Wins Implementation Order

Based on code review feedback, here's the prioritized implementation approach:

### Week 1 Sprint (Critical Foundation)
**Day 1-2: Phase 0 - Critical Fixes**
- [ ] Fix go.mod version (5 min)
- [ ] Unify printer architecture (1 hour)
- [ ] Add status --watch alias (15 min)
- [ ] Standardize error handling (2 hours)

**Day 3-5: Foundation Setup**
- [ ] Implement color system (internal/ui/colors.go)
- [ ] Enhance status display with colors
- [ ] Add debug/quiet flags
- [ ] Create help.go with grouped commands

### Week 2 Sprint (Enhancement)
- [ ] Implement formatted tables
- [ ] Add progress bars to sync
- [ ] Create output format abstraction
- [ ] Write golden tests for formatting
- [ ] Add configstore tests

### Week 3-4 Sprint (TUI Dashboard)
- [ ] Create minimal Bubble Tea skeleton
- [ ] Wire to existing sync monitor
- [ ] Add dashboard components incrementally
- [ ] Implement keyboard navigation
- [ ] Add compact mode
- [ ] Use build tags for soft dependency

### Week 5 Sprint (Interactive & Polish)
- [ ] Interactive prompts system
- [ ] Command suggestions
- [ ] Installation wizard enhancement
- [ ] Batch operations

### Week 6-7 Sprint (Testing & Documentation)
- [ ] Comprehensive testing
- [ ] Performance optimization
- [ ] Documentation updates
- [ ] Cross-platform validation

---

## 📅 Original Timeline (Adjusted)

- **Day 1-2**: Phase 0 - Critical Fixes
- **Week 1**: Phase 1 - Foundation
- **Week 2**: Phase 2 - Enhanced Status
- **Week 3-4**: Phase 3 - TUI Dashboard
- **Week 5**: Phase 4 - Interactive Features
- **Week 6**: Phase 5 - Advanced Features
- **Week 7**: Phase 6 - Polish & Integration
- **Week 8**: Buffer for testing and refinement

Total estimated time: 8 weeks for complete implementation

---

*This plan transforms the Push Validator Manager into a best-in-class tool with exceptional UX, focusing on clarity, efficiency, and user delight.*
