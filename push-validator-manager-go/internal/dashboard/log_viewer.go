package dashboard

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// LogViewer component displays and tails log file with scrolling and search
type LogViewer struct {
	BaseComponent
	logPath    string
	buffer     *ringBuffer
	scrollPos  int          // Current scroll position (0 = bottom/follow mode)
	followMode bool         // Auto-scroll to latest logs
	searchMode bool         // Search input active
	searchTerm string       // Current search filter
	noEmoji    bool
	mu         sync.RWMutex

	// Background log tailer
	cancel context.CancelFunc
}

// ringBuffer is a circular buffer for log lines
type ringBuffer struct {
	lines []string
	size  int
	head  int
	count int
	mu    sync.RWMutex
}

func newRingBuffer(size int) *ringBuffer {
	return &ringBuffer{
		lines: make([]string, size),
		size:  size,
	}
}

func (rb *ringBuffer) Add(line string) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	rb.lines[rb.head] = line
	rb.head = (rb.head + 1) % rb.size
	if rb.count < rb.size {
		rb.count++
	}
}

func (rb *ringBuffer) GetAll() []string {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	result := make([]string, rb.count)
	if rb.count == 0 {
		return result
	}

	// Calculate start position
	start := rb.head - rb.count
	if start < 0 {
		start += rb.size
	}

	for i := 0; i < rb.count; i++ {
		idx := (start + i) % rb.size
		result[i] = rb.lines[idx]
	}

	return result
}

func (rb *ringBuffer) Count() int {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	return rb.count
}

// NewLogViewer creates a new log viewer component
func NewLogViewer(noEmoji bool, homeDir string) *LogViewer {
	logPath := homeDir + "/logs/pchaind.log"

	lv := &LogViewer{
		BaseComponent: BaseComponent{},
		logPath:       logPath,
		buffer:        newRingBuffer(500),
		followMode:    true,
		scrollPos:     0,
		noEmoji:       noEmoji,
		mu:            sync.RWMutex{}, // Explicit mutex initialization
	}

	// Debug log
	fmt.Fprintf(os.Stderr, "DEBUG: LogViewer initialized (logPath=%s)\n", logPath)

	// Start background log tailer AFTER all fields are initialized
	ctx, cancel := context.WithCancel(context.Background())
	lv.cancel = cancel

	// Add delay to ensure component is fully ready
	go func() {
		time.Sleep(100 * time.Millisecond)
		lv.tailLogs(ctx)
	}()

	return lv
}

// ID returns component identifier
func (lv *LogViewer) ID() string {
	return "log_viewer"
}

// Title returns component title
func (lv *LogViewer) Title() string {
	icon := "📜 Logs"
	if lv.noEmoji {
		icon = "Logs"
	}

	if lv.searchMode {
		return fmt.Sprintf("%s [Search: %s]", icon, lv.searchTerm)
	}

	if !lv.followMode {
		return fmt.Sprintf("%s [Paused - %d lines]", icon, lv.buffer.Count())
	}

	return icon
}

// MinWidth returns minimum width
func (lv *LogViewer) MinWidth() int {
	return 40
}

// MinHeight returns minimum height
func (lv *LogViewer) MinHeight() int {
	return 12
}

// Update receives messages
func (lv *LogViewer) Update(msg tea.Msg, data DashboardData) (Component, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return lv.handleKey(msg)
	}

	return lv, nil
}

// handleKey processes keyboard input
func (lv *LogViewer) handleKey(msg tea.KeyMsg) (Component, tea.Cmd) {
	lv.mu.Lock()
	defer lv.mu.Unlock()

	if lv.searchMode {
		switch msg.String() {
		case "esc":
			lv.searchMode = false
			lv.searchTerm = ""
		case "enter":
			lv.searchMode = false
		case "backspace":
			if len(lv.searchTerm) > 0 {
				lv.searchTerm = lv.searchTerm[:len(lv.searchTerm)-1]
			}
		default:
			// Add to search term
			if len(msg.String()) == 1 {
				lv.searchTerm += msg.String()
			}
		}
		return lv, nil
	}

	switch msg.String() {
	case "/":
		lv.searchMode = true
		lv.searchTerm = ""

	case "f":
		lv.followMode = !lv.followMode
		if lv.followMode {
			lv.scrollPos = 0
		}

	case "up":
		if lv.followMode {
			lv.followMode = false
		}
		lv.scrollPos++
		// Bound scrollPos to buffer size to prevent overflow
		if lv.scrollPos > lv.buffer.Count() {
			lv.scrollPos = lv.buffer.Count()
		}

	case "down":
		lv.scrollPos--
		if lv.scrollPos <= 0 {
			lv.scrollPos = 0
			lv.followMode = true
		}

	case "t":  // 't' for 'top' - jump to oldest logs
		lv.followMode = false
		lv.scrollPos = lv.buffer.Count()

	case "l":  // 'l' for 'latest' - jump to newest logs
		lv.followMode = true
		lv.scrollPos = 0
	}

	return lv, nil
}

// View renders the log viewer
func (lv *LogViewer) View(w, h int) string {
	// Add panic recovery to prevent dashboard crashes
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "PANIC in LogViewer.View: %v\n", r)
		}
	}()

	lv.mu.RLock()
	defer lv.mu.RUnlock()

	// Style
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("63")).
		Padding(0, 1)

	content := lv.renderContent(w, h)

	// Check cache
	if lv.CheckCacheWithSize(content, w, h) {
		return lv.GetCached()
	}

	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}

	// Account for border
	borderWidth := 2
	contentWidth := w - borderWidth
	if contentWidth < 0 {
		contentWidth = 0
	}

	// Don't use MaxHeight - let border render fully
	// The layout system already allocates the right amount of space
	rendered := style.Width(contentWidth).Render(content)
	lv.UpdateCache(rendered)
	return rendered
}

// renderContent builds log content
func (lv *LogViewer) renderContent(w, h int) string {
	// Add panic recovery with detailed error info
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "PANIC in LogViewer.renderContent: %v (w=%d, h=%d)\n", r, w, h)
		}
	}()

	inner := w - 4
	if inner < 0 {
		inner = 0
	}

	// Title
	title := FormatTitle(lv.Title(), inner)

	// Get all lines
	allLines := lv.buffer.GetAll()

	// Filter by search term
	var filteredLines []string
	if lv.searchTerm != "" {
		searchLower := strings.ToLower(lv.searchTerm)
		for _, line := range allLines {
			if strings.Contains(strings.ToLower(line), searchLower) {
				filteredLines = append(filteredLines, line)
			}
		}
	} else {
		filteredLines = allLines
	}

	// Calculate visible lines accounting for:
	// - Title (1 line)
	// - Footer (1 line)
	// - Border padding (2 lines for top/bottom)
	// - Extra spacing to prevent border overlap (1 line)
	availableLines := h - 5
	if availableLines < 1 {
		availableLines = 1
	}

	// Apply scroll position
	totalLines := len(filteredLines)
	var visibleLines []string

	if totalLines == 0 {
		visibleLines = []string{"(no logs yet)"}
	} else {
		// Calculate slice range based on scroll position
		endIdx := totalLines - lv.scrollPos
		startIdx := endIdx - availableLines

		// Bounds checking to prevent slice panic
		if endIdx < 0 {
			endIdx = 0
		}
		if endIdx > totalLines {
			endIdx = totalLines
		}
		if startIdx < 0 {
			startIdx = 0
		}
		// CRITICAL: Ensure startIdx <= endIdx to prevent panic
		if startIdx > endIdx {
			startIdx = endIdx
		}

		visibleLines = filteredLines[startIdx:endIdx]
	}

	// Render lines with color coding
	var styledLines []string
	for _, line := range visibleLines {
		styledLine := lv.styleLogLine(line, inner)
		styledLines = append(styledLines, styledLine)
	}

	content := strings.Join(styledLines, "\n")

	// Add footer hint
	footer := lv.renderFooter()

	return fmt.Sprintf("%s\n%s\n%s", title, content, footer)
}

// styleLogLine applies color coding based on log level
func (lv *LogViewer) styleLogLine(line string, maxWidth int) string {
	// Don't truncate - let terminal handle line wrapping
	// This allows users to see full log messages

	if lv.noEmoji {
		return line
	}

	// Detect log level and apply color
	var style lipgloss.Style

	// Pattern matching for common log levels
	lowerLine := strings.ToLower(line)

	if strings.Contains(lowerLine, "error") || strings.Contains(lowerLine, "fatal") || strings.Contains(lowerLine, "panic") {
		style = lipgloss.NewStyle().Foreground(lipgloss.Color("196")) // Red
	} else if strings.Contains(lowerLine, "warn") || strings.Contains(lowerLine, "warning") {
		style = lipgloss.NewStyle().Foreground(lipgloss.Color("226")) // Yellow
	} else if strings.Contains(lowerLine, "info") {
		style = lipgloss.NewStyle().Foreground(lipgloss.Color("82")) // Green
	} else if strings.Contains(lowerLine, "debug") || strings.Contains(lowerLine, "trace") {
		style = lipgloss.NewStyle().Foreground(lipgloss.Color("240")) // Gray
	} else {
		// Default color
		return line
	}

	return style.Render(line)
}

// renderFooter shows control hints
func (lv *LogViewer) renderFooter() string {
	if lv.searchMode {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("241")).
			Render("Enter to apply | Esc to cancel")
	}

	var hints string
	if lv.followMode {
		hints = "↑/↓: scroll | f: pause | /: search | t: oldest"
	} else {
		hints = "↑/↓: scroll | f: live | /: search | l: latest | t: oldest"
	}

	return lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render(hints)
}

// tailLogs runs in background to tail log file
func (lv *LogViewer) tailLogs(ctx context.Context) {
	// Wait for log file to exist
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if _, err := os.Stat(lv.logPath); err == nil {
			break
		}
		time.Sleep(1 * time.Second)
	}

	// Read initial backlog (last 100 lines)
	if err := lv.loadBacklog(100); err != nil {
		// Ignore error, file might not exist yet
	}

	// Start tailing
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if err := lv.followFile(ctx); err != nil {
			time.Sleep(1 * time.Second)
			continue
		}
	}
}

// loadBacklog reads last N lines from log file
func (lv *LogViewer) loadBacklog(n int) error {
	f, err := os.Open(lv.logPath)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// Allow long log lines (up to 512 KiB)
	bufSize := 512 * 1024
	scanner.Buffer(make([]byte, bufSize), bufSize)

	var lines []string
	for scanner.Scan() {
		line := scanner.Text()
		if len(lines) == n {
			lines = lines[1:]
		}
		lines = append(lines, line)
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	// Add to buffer
	for _, line := range lines {
		lv.buffer.Add(line)
	}

	return nil
}

// followFile tails the log file
func (lv *LogViewer) followFile(ctx context.Context) error {
	f, err := os.Open(lv.logPath)
	if err != nil {
		return err
	}
	defer f.Close()

	// Seek to end
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return err
	}

	// Allow long log lines (up to 512 KiB)
	bufSize := 512 * 1024
	reader := bufio.NewReaderSize(f, bufSize)

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		line, err := reader.ReadString('\n')
		if err == io.EOF {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		if err != nil {
			return err
		}

		// Add to buffer (strip newline)
		lv.buffer.Add(strings.TrimSuffix(line, "\n"))
	}
}

// Close stops the background tailer
func (lv *LogViewer) Close() {
	if lv.cancel != nil {
		lv.cancel()
	}
}

// Compile-time check that LogViewer implements Component
var _ Component = (*LogViewer)(nil)
