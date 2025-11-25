package ui

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/nxadm/tail"
	"golang.org/x/term"
)

// LogUIOptions configures the TUI log viewer
type LogUIOptions struct {
	LogPath    string // Path to pchaind.log
	BgKey      byte   // Key to background (default: 'b')
	ShowFooter bool   // Enable footer (default: true)
	NoColor    bool   // Respect --no-color
}

// RunLogUI starts the interactive log viewer with a sticky footer.
// In TUI mode:
//   - Ctrl+C stops the node and exits
//   - BgKey (default 'b') detaches the viewer while keeping the node running
//
// Automatically falls back to plain tail for non-TTY environments.
func RunLogUI(ctx context.Context, opts LogUIOptions) error {
	debug := os.Getenv("DEBUG_TUI") != ""

	// 1. TTY check
	stdin := int(os.Stdin.Fd())
	stdout := int(os.Stdout.Fd())
	stdinTTY := term.IsTerminal(stdin)
	stdoutTTY := term.IsTerminal(stdout)

	if !stdinTTY || !stdoutTTY || !opts.ShowFooter {
		if debug {
			fmt.Fprintf(os.Stderr, "[DEBUG] TUI fallback: stdin_tty=%v stdout_tty=%v footer=%v\n",
				stdinTTY, stdoutTTY, opts.ShowFooter)
		}
		return tailFollow(ctx, opts.LogPath)
	}

	// 2. Terminal size check
	rows, cols, err := term.GetSize(stdout)
	if err != nil || rows < 5 || cols < 20 {
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cannot detect terminal size: %v; showing plain logs.\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "Terminal too small for TUI (rows=%d cols=%d, need 5x20+); showing plain logs.\n", rows, cols)
		}
		return tailFollow(ctx, opts.LogPath)
	}

	if debug {
		fmt.Fprintf(os.Stderr, "[DEBUG] TUI mode activating: terminal=%dx%d\n", cols, rows)
	}

	// 3. Enter raw mode
	oldState, err := term.MakeRaw(stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Cannot enable TUI mode; showing plain logs.")
		return tailFollow(ctx, opts.LogPath)
	}

	// 4. CRITICAL: Always restore terminal on ALL exit paths
	defer func() {
		term.Restore(stdin, oldState)     // restore cooked mode
		fmt.Fprint(os.Stdout, "\x1b[?7h") // re-enable line wrap
	}()

	// 5. Setup: disable line wrap only (no scroll region for now)
	fmt.Fprint(os.Stdout, "\x1b[?7l") // disable line wrap

	// 6. Show startup message (use \r\n in raw mode)
	fmt.Fprint(os.Stdout, "\r\n")
	fmt.Fprint(os.Stdout, "TUI mode active - Press Ctrl+C to STOP NODE | Press 'b' to detach\r\n")
	fmt.Fprint(os.Stdout, strings.Repeat("-", min(cols, 80))+"\r\n")
	fmt.Fprint(os.Stdout, "\r\n")

	// 8. Context with cancel for signals
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// 9. Setup signal handling (SIGTERM, SIGHUP only)
	// SIGINT is handled via raw stdin, SIGWINCH removed (no footer to update)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sigCh)

	go func() {
		for sig := range sigCh {
			switch sig {
			case syscall.SIGTERM, syscall.SIGHUP:
				cancel() // graceful exit
			}
		}
	}()

	// 10. Start log streaming in goroutine
	logErr := make(chan error, 1)
	go func() {
		logErr <- streamLogs(ctx, opts.LogPath, os.Stdout)
	}()

	// 11. Start keyboard listener with debouncing
	keyCh := listenKeys(ctx)

	// 12. Main loop: handle keys and check for errors
	for {
		select {
		case <-ctx.Done():
			return nil

		case err := <-logErr:
			if err != nil && err != context.Canceled {
				fmt.Fprintf(os.Stdout, "\r\nLog streaming error: %v\r\n", err)
				time.Sleep(1 * time.Second)
			}
			return err

		case key := <-keyCh:
			switch key {
			case 3: // Ctrl+C - STOP NODE
				fmt.Fprint(os.Stdout, "\r\nStopping node… ")
				_ = stopNode(ctx, os.Stdout, opts.NoColor)
				return nil

			case opts.BgKey, byte(unicode.ToUpper(rune(opts.BgKey))): // 'b' or 'B' - DETACH VIEWER
				fmt.Fprint(os.Stdout, "\r\nDetaching to background…\r\n")
				return nil
			}
		}
	}
}

// stopNode calls push-validator-manager stop using the same binary
func stopNode(ctx context.Context, w io.Writer, noColor bool) error {
	exe, err := os.Executable()
	if err != nil {
		exe = "push-validator" // fallback to PATH
	}

	c, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(c, exe, "stop")
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard

	if err := cmd.Run(); err != nil {
		fmt.Fprintf(w, "failed (%v)\n", err)
		return err
	}

	fmt.Fprint(w, "done\n")
	return nil
}

// renderFooter draws the 3-line footer at the bottom of the screen
func renderFooter(w io.Writer, rows, cols int, noColor bool) {
	if cols < 1 {
		cols = 1
	}

	// Never print more characters than we have columns
	divLen := cols
	if divLen > 80 {
		divLen = 80
	}

	div := strings.Repeat("─", divLen)
	if noColor {
		div = strings.Repeat("-", divLen)
	} else {
		div = "\x1b[2m" + div + "\x1b[0m" // dim gray
	}

	controls := "Press Ctrl+C to STOP NODE | Press 'b' to run in background"
	if len(controls) > cols {
		// Truncate safely to terminal width
		controls = controls[:cols]
	}

	// Clear both footer lines to avoid stale characters when resizing smaller
	fmt.Fprintf(w, "\x1b[%d;1H\x1b[2K%s", rows-2, div)      // divider
	fmt.Fprintf(w, "\x1b[%d;1H\x1b[2K%s", rows-1, controls) // controls
	fmt.Fprintf(w, "\x1b[%d;1H", rows)                      // spacer
}

// listenKeys reads keypresses from stdin with debouncing
func listenKeys(ctx context.Context) <-chan byte {
	keyCh := make(chan byte, 16)
	go func() {
		defer close(keyCh)
		buf := make([]byte, 1)
		lastKey := time.Now()

		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			n, err := os.Stdin.Read(buf)
			if err != nil || n == 0 {
				return
			}

			// No debounce for Ctrl+C (immediate stop)
			if buf[0] == 3 {
				keyCh <- buf[0]
				continue
			}

			// Debounce: ignore keys within 150ms of last key
			if time.Since(lastKey) < 150*time.Millisecond {
				continue
			}
			lastKey = time.Now()

			keyCh <- buf[0]
		}
	}()
	return keyCh
}

// streamLogs follows the log file with rotation support using github.com/nxadm/tail
func streamLogs(ctx context.Context, logPath string, out io.Writer) error {
	// Wait for log file creation (up to 5 seconds)
	for i := 0; i < 50; i++ {
		if _, err := os.Stat(logPath); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}

	t, err := tail.TailFile(logPath, tail.Config{
		Follow:    true,  // keep following
		ReOpen:    true,  // handle rotation
		MustExist: false, // don't error if file doesn't exist yet
		Poll:      false, // use inotify/kqueue (efficient)
	})
	if err != nil {
		return fmt.Errorf("failed to tail log: %w", err)
	}
	defer t.Cleanup()

	for {
		select {
		case <-ctx.Done():
			return nil
		case line := <-t.Lines:
			if line == nil {
				return nil
			}
			if line.Err != nil {
				return line.Err
			}
			// Use \r\n in raw mode for proper line breaks
			fmt.Fprintf(out, "%s\r\n", line.Text)
		}
	}
}

// tailFollow is a simple fallback for non-TTY environments
// It shells out to tail with -F/-f fallback for portability
func tailFollow(ctx context.Context, logPath string) error {
	// Try -F first (follows rotation), fallback to -f for BSD/macOS
	cmd := exec.CommandContext(ctx, "tail", "-F", logPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		// Fallback to -f for minimal systems
		cmd = exec.CommandContext(ctx, "tail", "-f", logPath)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}
	return nil
}

// min returns the smaller of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
