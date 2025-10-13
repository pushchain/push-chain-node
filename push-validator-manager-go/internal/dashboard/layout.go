package dashboard

import (
	"sort"
)

// LayoutConfig defines the dashboard layout structure
type LayoutConfig struct {
	Rows []LayoutRow
}

// LayoutRow defines a single row in the layout
type LayoutRow struct {
	Components []string // Component IDs
	Weights    []int    // Width distribution weights
	MinHeight  int      // Minimum height for this row
}

// Cell represents a positioned component in the final layout
type Cell struct {
	ID   string // Component ID
	X, Y int    // Position
	W, H int    // Dimensions
}

// LayoutResult is returned by Compute - concrete positioned components
type LayoutResult struct {
	Cells   []Cell
	Warning string // e.g., "Some panels hidden (terminal too narrow)"
}

// Layout manages component positioning
type Layout struct {
	config   LayoutConfig
	registry *ComponentRegistry
}

// NewLayout creates a new layout manager
func NewLayout(config LayoutConfig, registry *ComponentRegistry) *Layout {
	return &Layout{
		config:   config,
		registry: registry,
	}
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
	// Only distribute to data rows (skip header at index 0)
	verticalSlack := height - totalMinHeight
	if verticalSlack > 0 && len(l.config.Rows) > 1 {
		// Distribute slack only to data rows (rows 1+), not to header (row 0)
		dataRows := len(l.config.Rows) - 1
		extraPerRow := verticalSlack / dataRows
		remainder := verticalSlack % dataRows

		for i := 1; i < len(rowHeights); i++ { // Start at 1, skip header
			rowHeights[i] += extraPerRow
			if (i - 1) < remainder {
				rowHeights[i]++ // Fair remainder distribution
			}
		}
	}

	// Step 3: Build cells with distributed heights
	y := 0
	for i, row := range l.config.Rows {
		// For header row (index 0), use full width to prevent off-screen rendering
		// For data rows, reserve 4 char margin to prevent border clipping
		usableWidth := width
		if i > 0 {
			usableWidth = width - 4
			if usableWidth < 1 {
				usableWidth = 1
			}
		}
		widths, keptIDs, warning := l.computeRowWidths(row, usableWidth)
		if warning != "" {
			result.Warning = warning
		}

		// Build cells with kept component IDs (handles dropped components)
		x := 0
		for j := range keptIDs {
			result.Cells = append(result.Cells, Cell{
				ID: keptIDs[j],
				X:  x,
				Y:  y,
				W:  widths[j],
				H:  rowHeights[i], // Use distributed height
			})
			x += widths[j]
		}
		y += rowHeights[i]
	}

	return result
}

// computeRowWidths honors MinWidth, distributes by weights, handles remainder
// Returns: widths, kept component IDs, warning message
func (l *Layout) computeRowWidths(row LayoutRow, totalWidth int) ([]int, []string, string) {
	widths := make([]int, len(row.Components))
	keptIDs := append([]string(nil), row.Components...) // Default: keep all

	// Step 1: Satisfy all MinWidth requirements
	remainingWidth := totalWidth
	for i, compID := range row.Components {
		comp := l.registry.Get(compID)
		if comp == nil {
			// Component not found - use default min width
			widths[i] = 20
			remainingWidth -= 20
			continue
		}
		minW := comp.MinWidth()
		widths[i] = minW
		remainingWidth -= minW
	}

	// Step 2: Check if MinWidth requirements can be satisfied
	if remainingWidth < 0 {
		// Try to handle insufficient width
		return l.handleInsufficientWidth(row, totalWidth)
	}

	// Step 3: Distribute remaining width by weights + remainder
	totalWeight := 0
	for _, w := range row.Weights {
		totalWeight += w
	}

	if totalWeight == 0 {
		// No weights specified - distribute evenly
		return widths, keptIDs, ""
	}

	// Track fractional parts for fair remainder distribution
	type frac struct {
		idx  int
		frac float64
	}
	fracs := make([]frac, len(row.Components))

	distributed := 0
	for i, weight := range row.Weights {
		if i >= len(row.Components) {
			break
		}
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

	// Validate total width and trim if needed (safety check for edge cases)
	totalAllocated := 0
	for _, w := range widths {
		totalAllocated += w
	}
	if totalAllocated > totalWidth {
		excess := totalAllocated - totalWidth
		// Trim from rightmost component first, respecting MinWidth
		for i := len(widths) - 1; i >= 0 && excess > 0; i-- {
			comp := l.registry.Get(row.Components[i])
			if comp == nil {
				continue
			}
			canTrim := widths[i] - comp.MinWidth()
			if canTrim <= 0 {
				continue
			}
			trim := excess
			if trim > canTrim {
				trim = canTrim
			}
			widths[i] -= trim
			excess -= trim
		}
	}

	return widths, keptIDs, ""
}

// handleInsufficientWidth tries stack mode or drops components
// Returns: widths, kept component IDs, warning message
func (l *Layout) handleInsufficientWidth(row LayoutRow, width int) ([]int, []string, string) {
	// Keep only essential components
	essential := []string{"header", "node_status", "chain_status"}
	kept := keptEssentials(row.Components, essential)

	if len(kept) == 0 {
		kept = row.Components[:1] // Keep at least one
	}

	// CRITICAL: If we've already reduced to essential/minimal set but still don't fit,
	// we must terminate recursion. Clamp widths to available space.
	if len(kept) >= len(row.Components) || width < 10 {
		// Same component set or terminal too narrow - force clamp to prevent infinite loop
		// Drop components if terminal width cannot allocate at least one column each.
		if width <= 0 {
			return []int{1}, kept[:1], "Terminal too narrow - display truncated"
		}
		if len(kept) > width {
			kept = kept[:width]
		}
		widths := clampEven(kept, width)
		warning := "Terminal too narrow - display truncated"
		return widths, kept, warning
	}

	// Components were dropped - try to fit remaining set
	warning := "Some panels hidden (terminal too narrow)"
	newRow := LayoutRow{
		Components: kept,
		Weights:    equalWeights(len(kept)),
		MinHeight:  row.MinHeight,
	}
	widths, _, _ := l.computeRowWidths(newRow, width) // Recurse with reduced set
	return widths, kept, warning
}

// contains checks if string is in slice
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// keptEssentials filters component IDs to keep only essential ones
func keptEssentials(ids, essential []string) []string {
	kept := []string{}
	for _, id := range ids {
		if contains(essential, id) {
			kept = append(kept, id)
		}
	}
	return kept
}

// clampEven distributes width evenly across components with minimum per-component width
func clampEven(kept []string, width int) []int {
	widths := make([]int, len(kept))
	perComponent := width / len(kept)
	if perComponent < 1 {
		perComponent = 1 // Minimum 1 column per component
	}

	remainder := width - (perComponent * len(kept))
	for i := range widths {
		widths[i] = perComponent
		if i < remainder {
			widths[i]++ // Distribute remainder fairly
		}
	}

	// Adjust if rounding caused the total width to exceed the available space.
	total := 0
	for _, w := range widths {
		total += w
	}
	for i := len(widths) - 1; i >= 0 && total > width; i-- {
		if widths[i] > 1 {
			widths[i]--
			total--
		}
	}
	return widths
}

// equalWeights creates equal weights for n components using unit weights
// The remainder distribution logic in computeRowWidths handles fairness
func equalWeights(n int) []int {
	weights := make([]int, n)
	for i := range weights {
		weights[i] = 1
	}
	return weights
}
