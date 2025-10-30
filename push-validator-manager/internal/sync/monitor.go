package syncmon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/pushchain/push-chain-node/push-validator-manager/internal/node"
)

type Options struct {
	LocalRPC     string
	RemoteRPC    string
	LogPath      string
	Window       int
	Compact      bool
	Out          io.Writer     // default os.Stdout
	Interval     time.Duration // refresh interval for progress updates
	Quiet        bool          // minimal, non-emoji, non-TTY style output
	Debug        bool          // extra diagnostic prints
	StuckTimeout time.Duration // timeout for detecting stalled sync
}

type pt struct {
	h int64
	t time.Time
}

const defaultStuckTimeout = 2 * time.Minute

var ErrSyncStuck = errors.New("sync stuck: no progress detected")

// Run performs two-phase monitoring: snapshot spinner from logs, then WS header progress.
func Run(ctx context.Context, opts Options) error {
	if opts.Out == nil {
		opts.Out = os.Stdout
	}
	if opts.Window <= 0 {
		opts.Window = 30
	}
	if opts.StuckTimeout <= 0 {
		opts.StuckTimeout = defaultStuckTimeout
	}
	lastProgress := newAtomicTime(time.Now())

	tty := isTTY()
	if opts.Quiet {
		tty = false
	}
	hideCursor(opts.Out, tty)
	defer showCursor(opts.Out, tty)

	if tty && stdinIsTTY() {
		go swallowEnter(opts.Out)
	}

	// Start log tailer if log path provided
	snapCh := make(chan string, 16)
	stopLog := make(chan struct{})
	if opts.LogPath != "" {
		go tailStatesync(ctx, opts.LogPath, snapCh, stopLog)
	} else {
		close(snapCh)
	}

	// Phase 1: snapshot progress indicator until acceptance/quiet
	phase1Done := make(chan struct{})
	var phase1Err error
	var sawSnapshot bool
	var sawAccepted bool
	go func() {
		defer close(phase1Done)
		lastEvent := time.Now()
		sawSnapshot = false
		ticker := time.NewTicker(120 * time.Millisecond)
		defer ticker.Stop()

		steps := []string{
			"Discovering snapshots",
			"Downloading snapshot",
			"Restoring database",
			"Verifying and completing",
		}
		currentStep := 1
		maxStep := len(steps)
		printed := make([]bool, maxStep)
		completed := make([]bool, maxStep)

		spinnerFrames := []rune{'⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'}
		spinnerIndex := 0

		printStep := func(idx int, done bool) {
			if idx < 0 || idx >= maxStep {
				return
			}
			if done {
				if completed[idx] {
					return
				}
				completed[idx] = true
			} else if printed[idx] && !completed[idx] {
				return
			}
			line := renderStepIndicator(idx+1, maxStep, steps[idx], opts.Quiet, done)
			if done || !tty {
				if done && tty {
					// Clear spinner line before printing completed step
					fmt.Fprintf(opts.Out, "\r\033[K%s\n", line)
				} else {
					fmt.Fprintln(opts.Out, line)
				}
				printed[idx] = true
			} else {
				fmt.Fprintf(opts.Out, "\r\033[K%s %c", line, spinnerFrames[spinnerIndex])
				printed[idx] = true
			}
		}

		printStep(currentStep-1, false)

		for {
			select {
			case <-ctx.Done():
				phase1Err = ctx.Err()
				return
			case line, ok := <-snapCh:
				if !ok {
					return
				}
				low := strings.ToLower(line)
				if strings.Contains(low, "state sync failed") || strings.Contains(low, "state sync aborted") {
					phase1Err = fmt.Errorf("state sync failed: %s", strings.TrimSpace(line))
					return
				}
				switch {
				case strings.Contains(low, "discovering snapshots"):
					currentStep = 1
				case strings.Contains(low, "fetching snapshot chunk"):
					currentStep = 2
				case strings.Contains(low, "applied snapshot chunk") || strings.Contains(low, "restoring"):
					currentStep = 3
				case strings.Contains(low, "snapshot accepted") ||
					 strings.Contains(low, "snapshot restored") ||
					 strings.Contains(low, "restored snapshot") ||
					 strings.Contains(low, "switching to blocksync") ||
					 strings.Contains(low, "switching to block sync"):
					currentStep = 4
					sawAccepted = true
				}
				if strings.Contains(low, "statesync") || strings.Contains(low, "state sync") || strings.Contains(low, "snapshot") {
					lastEvent = time.Now()
					lastProgress.Update()
					sawSnapshot = true
				}
				if currentStep < 1 {
					currentStep = 1
				}
				if currentStep > maxStep {
					currentStep = maxStep
				}
				for i := 0; i < currentStep-1; i++ {
					printStep(i, true)
				}
				printStep(currentStep-1, sawAccepted && currentStep == maxStep)
			case <-ticker.C:
				if opts.StuckTimeout > 0 && lastProgress.Since() > opts.StuckTimeout {
					phase1Err = ErrSyncStuck
					return
				}
				if !completed[currentStep-1] {
					spinnerIndex = (spinnerIndex + 1) % len(spinnerFrames)
					line := renderStepIndicator(currentStep, maxStep, steps[currentStep-1], opts.Quiet, false)
					if tty {
						fmt.Fprintf(opts.Out, "\r\033[K%s %c", line, spinnerFrames[spinnerIndex])
					}
				}
				// Smart completion: if Step 3 stuck with no new logs for 3s, check RPC
				if currentStep == 3 && sawSnapshot && time.Since(lastEvent) > 3*time.Second {
					if isSyncedQuick(opts.LocalRPC) {
						currentStep = maxStep
						sawAccepted = true
						for i := 0; i < maxStep; i++ {
							printStep(i, true)
						}
						return
					}
				}
				if sawAccepted && time.Since(lastEvent) > 5*time.Second {
					for i := 0; i < maxStep; i++ {
						printStep(i, true)
					}
					return
				}
				if !sawSnapshot {
					if isSyncedQuick(opts.LocalRPC) {
						for i := 0; i < maxStep; i++ {
							printStep(i, true)
						}
						return
					}
				} else if isSyncedQuick(opts.LocalRPC) {
					currentStep = maxStep
					for i := 0; i < maxStep; i++ {
						printStep(i, true)
					}
					return
				}
			}
		}
	}()

	// Wait for phase 1 to complete or error
	<-phase1Done
	close(stopLog)
	if phase1Err != nil {
		return phase1Err
	}
	if sawAccepted {
		fmt.Fprintln(opts.Out, "")
		fmt.Fprintln(opts.Out, "  \033[92m✓\033[0m Snapshot restored. Switching to block sync...")
	}
	lastProgress.Update()

	// Phase 2: WS header subscription + progress bar
	local := strings.TrimRight(opts.LocalRPC, "/")
	if local == "" {
		local = "http://127.0.0.1:26657"
	}
	hostport := hostPortFromURL(local)
	// Wait for RPC up to 60s
	if !waitTCP(hostport, 60*time.Second) {
		return fmt.Errorf("RPC not listening on %s", hostport)
	}
	cli := node.New(local)
	headers, err := cli.SubscribeHeaders(ctx)
	if err != nil {
		return fmt.Errorf("ws subscribe: %w", err)
	}

	// Remote (denominator) via WebSocket headers
	remote := strings.TrimRight(opts.RemoteRPC, "/")
	if remote == "" {
		remote = local
	}
	remoteCli := node.New(remote)
	remoteHeaders, remoteWSErr := remoteCli.SubscribeHeaders(ctx)

	buf := make([]pt, 0, opts.Window)
	var lastRemote int64
	var baseH int64
	var baseRemote int64
	var barPrinted bool
	var firstBarTime time.Time
	var holdStarted bool
	var lastPeers int
	var lastLatency int64
	var lastMetricsAt time.Time
	// minimum time to show the bar even if already synced
	const minShow = 15 * time.Second
	// Print initial line to claim space
	if tty {
		fmt.Fprint(opts.Out, "\r")
	}
	iv := opts.Interval
	if iv <= 0 {
		iv = 1 * time.Second
	}
	tick := time.NewTicker(iv)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case rhd, ok := <-remoteHeaders:
			if remoteWSErr == nil && ok {
				lastProgress.Update()
				lastRemote = rhd.Height
				var cur int64
				if len(buf) > 0 {
					cur = buf[len(buf)-1].h
				}
				if cur == 0 {
					ctx2, cancel2 := context.WithTimeout(context.Background(), 800*time.Millisecond)
					st, err := cli.Status(ctx2)
					cancel2()
					if err == nil {
						cur = st.Height
					}
				}
				if cur > 0 && baseH != 0 {
					percent := 0.0
					if lastRemote > 0 {
						percent = float64(cur) / float64(lastRemote) * 100
					}
					percent = floor2(percent)
					if cur < lastRemote && percent >= 100 {
						percent = 99.99
					}
					line := renderProgressWithQuiet(percent, cur, lastRemote, opts.Quiet)
					rate, eta := progressRateAndETA(buf, cur, lastRemote)
					lineWithETA := line
					if eta != "" {
						lineWithETA += eta
					}
					if tty {
						extra := ""
						if lastPeers > 0 {
							extra += fmt.Sprintf(" | peers: %d", lastPeers)
						}
						if lastLatency > 0 {
							extra += fmt.Sprintf(" | rtt: %dms", lastLatency)
						}
						fmt.Fprintf(opts.Out, "\r\033[K  %s%s", lineWithETA, extra)
					} else {
						if opts.Quiet {
							fmt.Fprintf(opts.Out, "  height=%d/%d rate=%.2f%s peers=%d rtt=%dms\n", cur, lastRemote, rate, eta, lastPeers, lastLatency)
						} else {
							fmt.Fprintf(opts.Out, "  %s height=%d/%d rate=%.2f blk/s%s peers=%d rtt=%dms\n", time.Now().Format(time.Kitchen), cur, lastRemote, rate, eta, lastPeers, lastLatency)
						}
					}
					if !barPrinted {
						fmt.Fprintln(opts.Out, "")
						firstBarTime = time.Now()
						holdStarted = true
						barPrinted = true
					}
				}
			}
		case h, ok := <-headers:
			if !ok {
				return nil
			}
			lastProgress.Update()
			buf = append(buf, pt{h.Height, time.Now()})
			if len(buf) > opts.Window {
				buf = buf[1:]
			}
			// Render progress
			var cur = h.Height
			// Establish baseline once we know remote height and have at least one header
			if baseH == 0 && lastRemote > 0 && len(buf) > 0 {
				baseH = buf[0].h
				baseRemote = lastRemote
				if baseRemote <= baseH {
					baseRemote = baseH + 1000
				}
			}
			var percent float64
			if lastRemote > 0 {
				// Use baseline calculation only when there's meaningful progress to track
				if baseH > 0 && lastRemote > baseH && (lastRemote-baseH) > 100 {
					denom := float64(lastRemote - baseH)
					if denom > 0 {
						percent = float64(cur-baseH) / denom * 100
					}
				} else {
					// Direct calculation for already-synced or near-synced nodes
					percent = float64(cur) / float64(lastRemote) * 100
				}
			}
			// Avoid rounding up to 100.00 before actually matching remote
			percent = floor2(percent)
			if cur < lastRemote && percent >= 100 {
				percent = 99.99
			}
			// Compute moving rate from recent headers and derive ETA string.
			rate, eta := progressRateAndETA(buf, cur, lastRemote)
			// Periodically refresh peers and remote latency (every ~5s)
			if time.Since(lastMetricsAt) > 5*time.Second {
				lastMetricsAt = time.Now()
				ctxp, cancelp := context.WithTimeout(context.Background(), 1200*time.Millisecond)
				if plist, err := cli.Peers(ctxp); err == nil {
					lastPeers = len(plist)
				}
				cancelp()
				t0 := time.Now()
				ctxl, cancell := context.WithTimeout(context.Background(), 1200*time.Millisecond)
				_, _ = remoteCli.RemoteStatus(ctxl, remote)
				cancell()
				lastLatency = time.Since(t0).Milliseconds()
			}
			// Only render the bar once baseline exists
			if baseH == 0 {
				break
			}
			line := renderProgressWithQuiet(percent, cur, lastRemote, opts.Quiet)
			lineWithETA := line
			if eta != "" {
				lineWithETA += eta
			}
			if tty {
				extra := ""
				if lastPeers > 0 {
					extra += fmt.Sprintf(" | peers: %d", lastPeers)
				}
				if lastLatency > 0 {
					extra += fmt.Sprintf(" | rtt: %dms", lastLatency)
				}
				fmt.Fprintf(opts.Out, "\r\033[K%s%s", lineWithETA, extra)
			} else {
				if opts.Quiet {
					fmt.Fprintf(opts.Out, "height=%d/%d rate=%.2f%s peers=%d rtt=%dms\n", cur, lastRemote, rate, eta, lastPeers, lastLatency)
				} else {
					fmt.Fprintf(opts.Out, "%s height=%d/%d rate=%.2f blk/s%s peers=%d rtt=%dms\n", time.Now().Format(time.Kitchen), cur, lastRemote, rate, eta, lastPeers, lastLatency)
				}
			}
			if !barPrinted {
				fmt.Fprintln(opts.Out, "")
				firstBarTime = time.Now()
				holdStarted = true
			}
			barPrinted = true
		case <-tick.C:
			if opts.StuckTimeout > 0 && lastProgress.Since() > opts.StuckTimeout {
				if tty {
					fmt.Fprint(opts.Out, "\r\033[K")
				}
				return ErrSyncStuck
			}
			// Completion check via local status (cheap)
			ctx2, cancel2 := context.WithTimeout(context.Background(), 1200*time.Millisecond)
			st, err := cli.Status(ctx2)
			cancel2()
			if err == nil {
				// If we haven't printed any bar yet (e.g., already synced), render a final bar once
				if !barPrinted {
					cur := st.Height
					remoteH := lastRemote
					if remoteH == 0 { // quick remote probe
						remoteH = probeRemoteOnce(opts.RemoteRPC, cur)
					}
					if remoteH < cur {
						remoteH = cur
					}
					// If already synced but local height not yet reported, align to remote
					if !st.CatchingUp && cur < remoteH {
						cur = remoteH
					}
					// Avoid printing a misleading bar when cur is 0; wait for actual height
					if cur == 0 {
						break
					}
					var percent float64
					if remoteH > 0 {
						percent = float64(cur) / float64(remoteH) * 100
					}
					percent = floor2(percent)
					if cur < remoteH && percent >= 100 {
						percent = 99.99
					}
					line := renderProgressWithQuiet(percent, cur, remoteH, opts.Quiet)
					rate, eta := progressRateAndETA(buf, cur, remoteH)
					lineWithETA := line
					if eta != "" {
						lineWithETA += eta
					}
					if tty {
						extra := ""
						if lastPeers > 0 {
							extra += fmt.Sprintf(" | peers: %d", lastPeers)
						}
						if lastLatency > 0 {
							extra += fmt.Sprintf(" | rtt: %dms", lastLatency)
						}
						fmt.Fprintf(opts.Out, "\r\033[K  %s%s", lineWithETA, extra)
					} else {
						if opts.Quiet {
							fmt.Fprintf(opts.Out, "height=%d/%d rate=%.2f%s peers=%d rtt=%dms\n", cur, remoteH, rate, eta, lastPeers, lastLatency)
						} else {
							fmt.Fprintf(opts.Out, "%s height=%d/%d rate=%.2f blk/s%s peers=%d rtt=%dms\n", time.Now().Format(time.Kitchen), cur, remoteH, rate, eta, lastPeers, lastLatency)
						}
					}
					firstBarTime = time.Now()
					holdStarted = true
					barPrinted = true
				}
				// Active sync: update progress bar on every tick using current RPC status
				if st.CatchingUp && barPrinted && baseH > 0 {
					cur := st.Height
					if cur > 0 {
						// Add current height to buffer for rate calculation
						buf = append(buf, pt{cur, time.Now()})
						if len(buf) > opts.Window {
							buf = buf[1:]
						}
						// Calculate progress using baseline logic
						remoteH := lastRemote
						if remoteH == 0 {
							remoteH = probeRemoteOnce(opts.RemoteRPC, cur)
						}
						if remoteH < cur {
							remoteH = cur
						}
						var percent float64
						if remoteH > 0 {
							// Use baseline calculation for meaningful progress tracking
							if baseH > 0 && remoteH > baseH && (remoteH-baseH) > 100 {
								denom := float64(remoteH - baseH)
								if denom > 0 {
									percent = float64(cur-baseH) / denom * 100
								}
							} else {
								// Direct calculation for near-synced nodes
								percent = float64(cur) / float64(remoteH) * 100
							}
						}
						percent = floor2(percent)
						if cur < remoteH && percent >= 100 {
							percent = 99.99
						}
						// Render progress bar with current stats
						line := renderProgressWithQuiet(percent, cur, remoteH, opts.Quiet)
						rate, eta := progressRateAndETA(buf, cur, remoteH)
						lineWithETA := line
						if eta != "" {
							lineWithETA += eta
						}
						if tty {
							extra := ""
							if lastPeers > 0 {
								extra += fmt.Sprintf(" | peers: %d", lastPeers)
							}
							if lastLatency > 0 {
								extra += fmt.Sprintf(" | rtt: %dms", lastLatency)
							}
							fmt.Fprintf(opts.Out, "\r\033[K  %s%s", lineWithETA, extra)
						} else {
							if opts.Quiet {
								fmt.Fprintf(opts.Out, "height=%d/%d rate=%.2f%s peers=%d rtt=%dms\n", cur, remoteH, rate, eta, lastPeers, lastLatency)
							} else {
								fmt.Fprintf(opts.Out, "%s height=%d/%d rate=%.2f blk/s%s peers=%d rtt=%dms\n", time.Now().Format(time.Kitchen), cur, remoteH, rate, eta, lastPeers, lastLatency)
							}
						}
					}
				}
				// While within minShow and already not catching_up, keep the bar live-updating
				if !st.CatchingUp && barPrinted && time.Since(firstBarTime) < minShow {
					cur := st.Height
					if cur == 0 && len(buf) > 0 {
						cur = buf[len(buf)-1].h
					}
					remoteH := lastRemote
					if remoteH == 0 {
						remoteH = probeRemoteOnce(opts.RemoteRPC, cur)
					}
					if remoteH < cur {
						remoteH = cur
					}
					percent := 0.0
					if remoteH > 0 {
						percent = float64(cur) / float64(remoteH) * 100
					}
					percent = floor2(percent)
					if cur < remoteH && percent >= 100 {
						percent = 99.99
					}
					line := renderProgressWithQuiet(percent, cur, remoteH, opts.Quiet)
					rate, eta := progressRateAndETA(buf, cur, remoteH)
					lineWithETA := line
					if eta != "" {
						lineWithETA += eta
					}
					if tty {
						extra := ""
						if lastPeers > 0 {
							extra += fmt.Sprintf(" | peers: %d", lastPeers)
						}
						if lastLatency > 0 {
							extra += fmt.Sprintf(" | rtt: %dms", lastLatency)
						}
						fmt.Fprintf(opts.Out, "\r\033[K  %s%s", lineWithETA, extra)
					} else {
						if opts.Quiet {
							fmt.Fprintf(opts.Out, "height=%d/%d rate=%.2f%s peers=%d rtt=%dms\n", cur, remoteH, rate, eta, lastPeers, lastLatency)
						} else {
							fmt.Fprintf(opts.Out, "%s height=%d/%d rate=%.2f blk/s%s peers=%d rtt=%dms\n", time.Now().Format(time.Kitchen), cur, remoteH, rate, eta, lastPeers, lastLatency)
						}
					}
					continue
				}
				// End condition: catching_up is false AND minShow window has passed
				if !st.CatchingUp && holdStarted && time.Since(firstBarTime) >= minShow {
					cur := st.Height
					remoteH := lastRemote
					if remoteH == 0 {
						remoteH = probeRemoteOnce(opts.RemoteRPC, cur)
					}
					if remoteH < cur {
						remoteH = cur
					}
					percent := 0.0
					if remoteH > 0 {
						percent = float64(cur) / float64(remoteH) * 100
					}
					percent = floor2(percent)
					if cur < remoteH && percent >= 100 {
						percent = 99.99
					}
					line := renderProgressWithQuiet(percent, cur, remoteH, opts.Quiet)
					rate, eta := progressRateAndETA(buf, cur, remoteH)
					lineWithETA := line
					if eta != "" {
						lineWithETA += eta
					}
					if tty {
						extra := ""
						if lastPeers > 0 {
							extra += fmt.Sprintf(" | peers: %d", lastPeers)
						}
						if lastLatency > 0 {
							extra += fmt.Sprintf(" | rtt: %dms", lastLatency)
						}
						fmt.Fprintf(opts.Out, "\r\033[K%s%s\n", lineWithETA, extra)
					} else {
						if opts.Quiet {
							fmt.Fprintf(opts.Out, "height=%d/%d rate=%.2f%s peers=%d rtt=%dms\n", cur, remoteH, rate, eta, lastPeers, lastLatency)
						} else {
							fmt.Fprintf(opts.Out, "%s height=%d/%d rate=%.2f blk/s%s peers=%d rtt=%dms\n", time.Now().Format(time.Kitchen), cur, remoteH, rate, eta, lastPeers, lastLatency)
						}
					}
					return nil
				}
			}
		}
	}
}

// --- helpers ---

type atomicTime struct {
	value atomic.Int64
}

func newAtomicTime(t time.Time) *atomicTime {
	at := &atomicTime{}
	at.Store(t)
	return at
}

func (a *atomicTime) Store(t time.Time) {
	a.value.Store(t.UnixNano())
}

func (a *atomicTime) Update() {
	a.Store(time.Now())
}

func (a *atomicTime) Since() time.Duration {
	last := a.value.Load()
	if last == 0 {
		return 0
	}
	return time.Since(time.Unix(0, last))
}

func renderStepIndicator(step, total int, message string, quiet bool, completed bool) string {
	filled := "●"
	empty := "○"
	if quiet {
		filled = "#"
		empty = "-"
	}
	if total <= 0 {
		total = 1
	}
	if step < 1 {
		step = 1
	}
	if step > total {
		step = total
	}
	var sb strings.Builder
	sb.Grow(total)
	for i := 1; i <= total; i++ {
		if i <= step {
			sb.WriteString(filled)
		} else {
			sb.WriteString(empty)
		}
	}
	suffix := message
	if completed && !quiet {
		suffix = fmt.Sprintf("%s \033[92m%s\033[0m", message, "✓")
	}
	return fmt.Sprintf("    [%s] Step %d/%d: %s", sb.String(), step, total, suffix)
}

func tailStatesync(ctx context.Context, path string, out chan<- string, stop <-chan struct{}) {
	defer close(out)
	// Wait for log file to appear to avoid missing early snapshot lines
	for {
		if _, err := os.Stat(path); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case <-time.After(300 * time.Millisecond):
		}
	}
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	// Seek to end
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return
	}
	r := bufio.NewReader(f)
	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		default:
		}
		line, err := r.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				time.Sleep(100 * time.Millisecond)
				continue
			}
			return
		}
		// Only forward relevant lines to reduce chatter
		low := strings.ToLower(line)
		if strings.Contains(low, "statesync") || strings.Contains(low, "state sync") || strings.Contains(low, "snapshot") {
			out <- strings.TrimSpace(line)
		}
	}
}

func hostPortFromURL(s string) string {
	u, err := url.Parse(s)
	if err == nil && u.Host != "" {
		return u.Host
	}
	return "127.0.0.1:26657"
}

// isSyncedQuick checks local RPC catching_up with a tiny timeout.
func isSyncedQuick(local string) bool {
	local = strings.TrimRight(local, "/")
	if local == "" {
		local = "http://127.0.0.1:26657"
	}
	httpc := &http.Client{Timeout: 1200 * time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, local+"/status", nil)
	resp, err := httpc.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var payload struct {
		Result struct {
			SyncInfo struct {
				CatchingUp bool `json:"catching_up"`
			} `json:"sync_info"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return false
	}
	return !payload.Result.SyncInfo.CatchingUp
}

func waitTCP(hostport string, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		conn, err := (&net.Dialer{Timeout: 750 * time.Millisecond}).Dial("tcp", hostport)
		if err == nil {
			_ = conn.Close()
			return true
		}
		time.Sleep(750 * time.Millisecond)
	}
	return false
}

func pollRemote(ctx context.Context, base string, every time.Duration, out chan<- int64) {
	defer close(out)
	httpc := &http.Client{Timeout: 2 * time.Second}
	base = strings.TrimRight(base, "/")
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/status", nil)
			resp, err := httpc.Do(req)
			if err != nil {
				continue
			}
			var payload struct {
				Result struct {
					SyncInfo struct {
						Height string `json:"latest_block_height"`
					} `json:"sync_info"`
				} `json:"result"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&payload)
			_ = resp.Body.Close()
			if payload.Result.SyncInfo.Height != "" {
				hv, _ := strconvParseInt(payload.Result.SyncInfo.Height)
				if hv > 0 {
					select {
					case out <- hv:
					default:
					}
				}
			}
		}
	}
}

// probeRemoteOnce fetches a single remote height with a small timeout.
func probeRemoteOnce(base string, fallback int64) int64 {
	base = strings.TrimRight(base, "/")
	if base == "" {
		return fallback
	}
	httpc := &http.Client{Timeout: 1200 * time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/status", nil)
	resp, err := httpc.Do(req)
	if err != nil {
		return fallback
	}
	defer resp.Body.Close()
	var payload struct {
		Result struct {
			SyncInfo struct {
				Height string `json:"latest_block_height"`
			} `json:"sync_info"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return fallback
	}
	h, _ := strconvParseInt(payload.Result.SyncInfo.Height)
	if h <= 0 {
		return fallback
	}
	return h
}

func movingRate(buf []struct {
	h int64
	t time.Time
}) float64 {
	n := len(buf)
	if n < 2 {
		return 0
	}
	dh := float64(buf[n-1].h - buf[0].h)
	dt := buf[n-1].t.Sub(buf[0].t).Seconds()
	if dt <= 0 {
		return 0
	}
	return dh / dt
}

func progressRateAndETA(buf []pt, cur, remote int64) (float64, string) {
	rate := movingRatePt(buf)
	if rate <= 0 || math.IsNaN(rate) || math.IsInf(rate, 0) {
		rate = 1.0
	}
	eta := ""
	if remote > cur && rate > 0 {
		rem := float64(remote-cur) / rate
		if rem < 0 {
			rem = 0
		}
		eta = fmt.Sprintf(" | ETA: %s", (time.Duration(rem * float64(time.Second))).Round(time.Second))
	} else if remote > 0 {
		eta = " | ETA: 0s"
	}
	return rate, eta
}

func renderProgress(percent float64, cur, remote int64) string {
	width := 28
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	filled := int(percent / 100 * float64(width))
	if filled > width {
		filled = width
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
	return fmt.Sprintf("📊 Syncing [%s] %.2f%% | %d/%d blocks", bar, percent, cur, remote)
}

func renderProgressWithQuiet(percent float64, cur, remote int64, quiet bool) string {
	if quiet {
		width := 28
		if percent < 0 {
			percent = 0
		}
		if percent > 100 {
			percent = 100
		}
		filled := int(percent / 100 * float64(width))
		if filled < 0 {
			filled = 0
		}
		if filled > width {
			filled = width
		}
		bar := strings.Repeat("#", filled) + strings.Repeat("-", width-filled)
		return fmt.Sprintf("[%s] %.2f%% | %d/%d", bar, percent, cur, remote)
	}
	return renderProgress(percent, cur, remote)
}

func stdinIsTTY() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

func swallowEnter(out io.Writer) {
	reader := bufio.NewReader(os.Stdin)
	for {
		r, _, err := reader.ReadRune()
		if err != nil {
			return
		}
		if r == '\n' || r == '\r' {
			// Move cursor to beginning of current line and clear it
			// This handles the newline created by Enter without moving up
			fmt.Fprint(out, "\r\x1b[K")
		}
	}
}

func isTTY() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode()&os.ModeCharDevice) != 0 && os.Getenv("TERM") != ""
}

func hideCursor(w io.Writer, tty bool) {
	if tty {
		fmt.Fprint(w, "\x1b[?25l")
	}
}
func showCursor(w io.Writer, tty bool) {
	if tty {
		fmt.Fprint(w, "\x1b[?25h")
	}
}

// local copy to avoid extra imports
func strconvParseInt(s string) (int64, error) {
	var n int64
	var sign int64 = 1
	if s == "" {
		return 0, fmt.Errorf("empty")
	}
	if s[0] == '-' {
		sign = -1
		s = s[1:]
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("invalid")
		}
		n = n*10 + int64(c-'0')
	}
	return sign * n, nil
}

// floor1 returns v floored to one decimal place.
func floor1(v float64) float64 { return math.Floor(v*10.0) / 10.0 }

// floor2 returns v floored to two decimal places.
func floor2(v float64) float64 { return math.Floor(v*100.0) / 100.0 }

// convert helper to reuse movingRate signature
func movingRatePt(in []pt) float64 {
	tmp := make([]struct {
		h int64
		t time.Time
	}, len(in))
	for i := range in {
		tmp[i] = struct {
			h int64
			t time.Time
		}{h: in[i].h, t: in[i].t}
	}
	return movingRate(tmp)
}
