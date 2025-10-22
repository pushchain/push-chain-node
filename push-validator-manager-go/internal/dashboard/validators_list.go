package dashboard

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/pushchain/push-chain-node/push-validator-manager-go/internal/config"
	"github.com/pushchain/push-chain-node/push-validator-manager-go/internal/validator"
)

// rewardsFetchedMsg indicates rewards have been fetched and cache is updated
type rewardsFetchedMsg struct{}

// ValidatorsList component shows network validators
type ValidatorsList struct {
	BaseComponent
	data               DashboardData
	icons              Icons
	cfg                config.Config    // Config for RPC queries
	currentPage        int               // Current page (0-based)
	pageSize           int               // Items per page
	myValidatorAddress string            // Address of current node's validator (if any)
	showEVMAddress     bool              // Toggle between Cosmos and EVM address display
	evmAddressCache    map[string]string // Cache for fetched EVM addresses
	fetchingEVMCache   bool              // Flag to prevent duplicate concurrent fetches
	rewardsCache       map[string]struct {
		Commission    string
		Outstanding   string
	} // Cache for fetched rewards
	rewardsCacheMu  sync.Mutex // Protects rewardsCache from concurrent access
	fetchingRewards bool       // Flag to prevent duplicate concurrent rewards fetches
	sortedValidators []struct {
		Moniker              string
		Status               string
		VotingPower          int64
		Commission           string
		CommissionRewards    string
		OutstandingRewards   string
		Address              string
		EVMAddress           string
	} // Sorted validators array shared between render and fetch
}

// NewValidatorsList creates a new validators list component
func NewValidatorsList(noEmoji bool, cfg config.Config) *ValidatorsList {
	return &ValidatorsList{
		BaseComponent:   BaseComponent{},
		icons:           NewIcons(noEmoji),
		cfg:             cfg,
		currentPage:     0,
		pageSize:        5,
		evmAddressCache: make(map[string]string),
		rewardsCache: make(map[string]struct {
			Commission    string
			Outstanding   string
		}),
	}
}

// ID returns component identifier
func (c *ValidatorsList) ID() string {
	return "validators_list"
}

// Title returns component title
func (c *ValidatorsList) Title() string {
	totalValidators := len(c.data.NetworkValidators.Validators)
	if totalValidators == 0 {
		return "Network Validators"
	}

	totalPages := (totalValidators + c.pageSize - 1) / c.pageSize
	if totalPages > 1 {
		return fmt.Sprintf("Network Validators (Page %d/%d)", c.currentPage+1, totalPages)
	}
	return "Network Validators"
}

// MinWidth returns minimum width
func (c *ValidatorsList) MinWidth() int {
	return 30
}

// MinHeight returns minimum height
func (c *ValidatorsList) MinHeight() int {
	return 16
}

// Update receives dashboard data
func (c *ValidatorsList) Update(msg tea.Msg, data DashboardData) (Component, tea.Cmd) {
	c.data = data
	c.myValidatorAddress = data.MyValidator.Address

	// Update sorted validators array whenever data changes
	if len(c.data.NetworkValidators.Validators) > 0 {
		c.sortedValidators = c.getSortedValidators()
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		return c.handleKey(msg)
	case rewardsFetchedMsg:
		// Rewards have been fetched, clear the flag and return a no-op command
		// The no-op command itself will trigger a re-render by the parent dashboard
		c.fetchingRewards = false
		return c, tea.Batch() // Batch with no commands still triggers re-render
	}

	// Trigger rewards fetching for current page if not already fetching
	// Only fetch if: data is available AND not currently fetching
	// Individual validators check cache before fetching (skip if already cached)
	if len(c.sortedValidators) > 0 && !c.fetchingRewards {
		c.fetchingRewards = true // Set flag IMMEDIATELY to prevent duplicate fetches
		return c, c.fetchPageRewardsCmd()
	}

	return c, nil
}

// getSortedValidators returns validators sorted by status and voting power
func (c *ValidatorsList) getSortedValidators() []struct {
	Moniker              string
	Status               string
	VotingPower          int64
	Commission           string
	CommissionRewards    string
	OutstandingRewards   string
	Address              string
	EVMAddress           string
} {
	validators := make([]struct {
		Moniker              string
		Status               string
		VotingPower          int64
		Commission           string
		CommissionRewards    string
		OutstandingRewards   string
		Address              string
		EVMAddress           string
	}, len(c.data.NetworkValidators.Validators))
	copy(validators, c.data.NetworkValidators.Validators)

	// Helper to get status sort order
	statusOrder := func(status string) int {
		switch status {
		case "BONDED":
			return 1
		case "UNBONDING":
			return 2
		case "UNBONDED":
			return 3
		default:
			return 4
		}
	}

	sort.Slice(validators, func(i, j int) bool {
		// Sort by status first (BONDED < UNBONDING < UNBONDED)
		iOrder := statusOrder(validators[i].Status)
		jOrder := statusOrder(validators[j].Status)
		if iOrder != jOrder {
			return iOrder < jOrder
		}
		// Within same status, sort by voting power (highest first)
		return validators[i].VotingPower > validators[j].VotingPower
	})

	return validators
}

// handleKey processes keyboard input for pagination and toggles
func (c *ValidatorsList) handleKey(msg tea.KeyMsg) (Component, tea.Cmd) {
	totalValidators := len(c.data.NetworkValidators.Validators)

	switch msg.String() {
	case "e":
		// Toggle between Cosmos and EVM address display
		c.showEVMAddress = !c.showEVMAddress
		// If toggling to EVM and cache is empty, start fetching addresses
		if c.showEVMAddress && len(c.evmAddressCache) == 0 && !c.fetchingEVMCache {
			return c, c.fetchEVMAddressesCmd()
		}
		return c, nil
	}

	if totalValidators == 0 {
		return c, nil
	}

	totalPages := (totalValidators + c.pageSize - 1) / c.pageSize

	switch msg.String() {
	case "left", "p":
		// Previous page
		if c.currentPage > 0 {
			c.currentPage--
			c.fetchingRewards = false // Reset flag to allow fetching for new page
		}
	case "right", "n":
		// Next page
		if c.currentPage < totalPages-1 {
			c.currentPage++
			c.fetchingRewards = false // Reset flag to allow fetching for new page
		}
	}

	return c, nil
}

// View renders the component with caching
func (c *ValidatorsList) View(w, h int) string {
	// Render with styling
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("63")).
		Padding(0, 1)

	content := c.renderContent(w)

	// Check cache
	if c.CheckCacheWithSize(content, w, h) {
		return c.GetCached()
	}

	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}

	// Account for border width (2 chars: left + right) to prevent overflow
	borderWidth := 2
	contentWidth := w - borderWidth
	if contentWidth < 0 {
		contentWidth = 0
	}

	rendered := style.Width(contentWidth).Render(content)
	c.UpdateCache(rendered)
	return rendered
}

// renderContent builds plain text content
func (c *ValidatorsList) renderContent(w int) string {
	var lines []string

	// Interior width after accounting for rounded border (2 chars) and padding (2 chars).
	inner := w - 4
	if inner < 0 {
		inner = 0
	}

	// Check if validator data is available
	if c.data.NetworkValidators.Total == 0 {
		return fmt.Sprintf("%s\n\n%s Loading validators...", FormatTitle(c.Title(), inner), c.icons.Warn)
	}

	// Use pre-sorted validators from Update()
	// This ensures fetch and render use the same validator order
	validators := c.sortedValidators
	if len(validators) == 0 {
		return fmt.Sprintf("%s\n\n%s Loading validators...", FormatTitle(c.Title(), inner), c.icons.Warn)
	}

	// Table header - show different label based on toggle
	addressLabel := "ADDRESS"
	if c.showEVMAddress {
		addressLabel = "ADDRESS (EVM)"
	} else {
		addressLabel = "ADDRESS (COSMOS)"
	}
	headerLine := fmt.Sprintf("%-40s %-11s %-9s %-11s %-18s %-18s %s", "MONIKER", "STATUS", "STAKE(PC)", "COMMISSION%", "COMMISSION_REWARDS", "OUTSTANDING_REWARDS", addressLabel)
	lines = append(lines, headerLine)
	// Create separator line that matches the header width
	lines = append(lines, strings.Repeat("─", len(headerLine)))

	// Calculate pagination bounds
	totalValidators := len(validators)
	startIdx := c.currentPage * c.pageSize
	endIdx := startIdx + c.pageSize
	if endIdx > totalValidators {
		endIdx = totalValidators
	}

	// Bounds check
	if startIdx >= totalValidators {
		startIdx = 0
		c.currentPage = 0
		endIdx = c.pageSize
		if endIdx > totalValidators {
			endIdx = totalValidators
		}
	}

	// Display validators for current page
	for i := startIdx; i < endIdx; i++ {
		v := validators[i]

		// Check if this is our validator
		isOurValidator := c.myValidatorAddress != "" && v.Address == c.myValidatorAddress

		// Show full moniker with indicator if our validator
		moniker := v.Moniker
		if isOurValidator {
			moniker = moniker + " [My Validator]"
		}
		// Truncate if still too long (40 chars max for display)
		moniker = truncateWithEllipsis(moniker, 40)

		// Show full status
		status := v.Status

		// Format voting power (compact display)
		powerStr := fmt.Sprintf("%s", HumanInt(v.VotingPower))

		// Commission percentage (already formatted from staking query)
		commission := v.Commission
		if len(commission) > 5 {
			commission = commission[:5]
		}

		// Commission rewards - check cache first, then data
		commRewards := v.CommissionRewards
		if commRewards == "" {
			c.rewardsCacheMu.Lock()
			cached, exists := c.rewardsCache[v.Address]
			c.rewardsCacheMu.Unlock()
			if exists {
				commRewards = cached.Commission
			} else {
				commRewards = "—"
			}
		}

		// Outstanding rewards - check cache first, then data
		outRewards := v.OutstandingRewards
		if outRewards == "" {
			c.rewardsCacheMu.Lock()
			cached, exists := c.rewardsCache[v.Address]
			c.rewardsCacheMu.Unlock()
			if exists {
				outRewards = cached.Outstanding
			} else {
				outRewards = "—"
			}
		}

		// Select address based on toggle
		address := v.Address
		if c.showEVMAddress {
			// Try to get from cache first, fallback to data, then placeholder
			cachedAddr := c.getEVMAddressFromCache(v.Address)
			if cachedAddr != "" {
				address = cachedAddr
			} else if v.EVMAddress != "" {
				address = v.EVMAddress
			} else {
				address = "—"
			}
		}

		// Build row with flexible-width columns
		line := fmt.Sprintf("%-40s %-11s %-9s %-11s %-18s %-18s %s",
			moniker, status, powerStr, commission, commRewards, outRewards, address)

		// Apply highlighting to own validator rows
		if isOurValidator {
			// Bold + bright green for own validator
			highlightStyle := lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("10")) // Bright green
			line = highlightStyle.Render(line)
		}

		lines = append(lines, line)
	}

	lines = append(lines, "")

	// Add pagination footer with toggle info
	totalPages := (totalValidators + c.pageSize - 1) / c.pageSize
	var footer string
	if totalPages > 1 {
		footer = fmt.Sprintf("← / →: change page | e: toggle EVM/Cosmos | Total: %d validators", c.data.NetworkValidators.Total)
	} else {
		footer = fmt.Sprintf("e: toggle EVM/Cosmos | Total: %d validators", c.data.NetworkValidators.Total)
	}
	lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render(footer))

	return fmt.Sprintf("%s\n%s", FormatTitle(c.Title(), inner), joinLines(lines, "\n"))
}

// fetchEVMAddressesCmd returns a command to fetch EVM addresses in background
func (c *ValidatorsList) fetchEVMAddressesCmd() tea.Cmd {
	return func() tea.Msg {
		c.fetchingEVMCache = true
		defer func() { c.fetchingEVMCache = false }()

		// Fetch EVM addresses for all validators with generous timeout
		for _, v := range c.data.NetworkValidators.Validators {
			if v.Address == "" {
				continue
			}
			// Check cache first
			if _, exists := c.evmAddressCache[v.Address]; exists {
				continue
			}
			// Fetch with a timeout per address (3 seconds each)
			evmAddr := validator.GetEVMAddress(context.Background(), v.Address)
			c.evmAddressCache[v.Address] = evmAddr
		}
		return nil
	}
}

// getEVMAddressFromCache returns cached EVM address or empty string
func (c *ValidatorsList) getEVMAddressFromCache(address string) string {
	if addr, exists := c.evmAddressCache[address]; exists {
		return addr
	}
	return ""
}

// getStatusIcon returns appropriate icon for validator status
func (c *ValidatorsList) getStatusIcon(status string) string {
	switch status {
	case "BONDED":
		return c.icons.OK
	case "UNBONDING":
		return c.icons.Warn
	case "UNBONDED":
		return c.icons.Err
	default:
		return c.icons.Warn
	}
}

// fetchPageRewardsCmd returns a command to fetch rewards for current page in parallel
func (c *ValidatorsList) fetchPageRewardsCmd() tea.Cmd {
	return func() tea.Msg {
		// Get validators for current page from SORTED array (same order as render)
		totalValidators := len(c.sortedValidators)
		startIdx := c.currentPage * c.pageSize
		endIdx := startIdx + c.pageSize
		if endIdx > totalValidators {
			endIdx = totalValidators
		}

		if startIdx >= totalValidators {
			return nil
		}

		// Fetch rewards in parallel using goroutines
		var wg sync.WaitGroup
		for i := startIdx; i < endIdx; i++ {
			v := c.sortedValidators[i]
			if v.Address == "" {
				continue
			}

			// Skip if already cached
			c.rewardsCacheMu.Lock()
			_, exists := c.rewardsCache[v.Address]
			c.rewardsCacheMu.Unlock()
			if exists {
				continue
			}

			wg.Add(1)
			go func(addr string) {
				defer wg.Done()

				// Fetch with a 5-second timeout per validator
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				if commRwd, outRwd, err := validator.GetValidatorRewards(ctx, c.cfg, addr); err == nil {
					c.rewardsCacheMu.Lock()
					c.rewardsCache[addr] = struct {
						Commission    string
						Outstanding   string
					}{
						Commission:    commRwd,
						Outstanding:   outRwd,
					}
					c.rewardsCacheMu.Unlock()
				}
			}(v.Address)
		}

		wg.Wait()
		return rewardsFetchedMsg{}
	}
}
