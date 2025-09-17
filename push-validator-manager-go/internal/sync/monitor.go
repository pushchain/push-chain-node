package syncmon

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/pushchain/push-chain-node/push-validator-manager-go/internal/node"
)

type Options struct {
	LocalRPC  string
	RemoteRPC string
	LogPath   string
	Window    int
	Compact   bool
	Out       io.Writer // default os.Stdout
}

type pt struct {
	h int64
	t time.Time
}

// Run performs two-phase monitoring: snapshot spinner from logs, then WS header progress.
func Run(ctx context.Context, opts Options) error {
	if opts.Out == nil {
		opts.Out = os.Stdout
	}
	if opts.Window <= 0 {
		opts.Window = 30
	}

	tty := isTTY()
	hideCursor(opts.Out, tty)
	defer showCursor(opts.Out, tty)

	// Start log tailer if log path provided
	snapCh := make(chan string, 16)
	stopLog := make(chan struct{})
	if opts.LogPath != "" {
		go tailStatesync(ctx, opts.LogPath, snapCh, stopLog)
	} else {
		close(snapCh)
	}

	// Phase 1: snapshot spinner until acceptance/quiet
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
		frames := []rune{'⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'}
		fi := 0
		for {
			select {
			case <-ctx.Done():
				phase1Err = ctx.Err()
				return
			case line, ok := <-snapCh:
				if !ok {
					// log tail closed; move to phase2 anyway
					return
				}
				low := strings.ToLower(line)
				if strings.Contains(low, "state sync failed") || strings.Contains(low, "state sync aborted") {
					phase1Err = fmt.Errorf("state sync failed: %s", strings.TrimSpace(line))
					return
				}
				if strings.Contains(low, "snapshot accepted") || strings.Contains(low, "restoring") {
					sawAccepted = true
				}
				if strings.Contains(low, "statesync") || strings.Contains(low, "state sync") || strings.Contains(low, "snapshot") {
					lastEvent = time.Now()
					sawSnapshot = true
				}
			case <-ticker.C:
				if tty {
					msg := "📥 Downloading and applying state sync snapshot..."
					fmt.Fprintf(opts.Out, "\r%s %c", msg, frames[fi%len(frames)])
					fi++
				}
				// If we saw acceptance and logs have been quiet for a few seconds, proceed
				if sawAccepted && time.Since(lastEvent) > 5*time.Second {
					return
				}
				// If we never saw snapshot logs but node is already synced, proceed
				if !sawSnapshot {
					if isSyncedQuick(opts.LocalRPC) {
						return
					}
				}
			}
		}
	}()

	// Wait for phase 1 to complete or error
	<-phase1Done
	close(stopLog)
	if phase1Err != nil {
		if tty {
			fmt.Fprint(opts.Out, "\r\033[K")
		}
		return phase1Err
	}
	if tty {
		fmt.Fprint(opts.Out, "\r\033[K")
	}
	if sawAccepted && tty {
		fmt.Fprintln(opts.Out, "✅ Snapshot restored. Switching to block sync...")
	}

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
	// minimum time to show the bar even if already synced
	const minShow = 15 * time.Second
	// Print initial line to claim space
	if tty {
		fmt.Fprint(opts.Out, "\r")
	}
	tick := time.NewTicker(1 * time.Second)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case rhd, ok := <-remoteHeaders:
			if remoteWSErr == nil && ok {
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
					line := renderProgress(percent, cur, lastRemote)
					if tty {
						fmt.Fprintf(opts.Out, "\r\033[K%s", line)
					} else {
						fmt.Println(line)
					}
					if !barPrinted {
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
			// Compute moving rate from recent headers
			rate := movingRatePt(buf)
			eta := ""
			if lastRemote > cur && rate > 0 {
				rem := float64(lastRemote-cur) / rate
				eta = fmt.Sprintf(" | ETA: %s", (time.Duration(rem * float64(time.Second))).Round(time.Second))
			}
			// Only render the bar once baseline exists and we have at least 2 samples (reduce jitter)
			if baseH == 0 || len(buf) < 2 {
				break
			}
			line := renderProgress(percent, cur, lastRemote)
			if tty {
				fmt.Fprintf(opts.Out, "\r\033[K%s%s", line, eta)
			} else {
				fmt.Fprintf(opts.Out, "%s height=%d/%d rate=%.2f blk/s%s\n", time.Now().Format(time.Kitchen), cur, lastRemote, rate, eta)
			}
			if !barPrinted {
				firstBarTime = time.Now()
				holdStarted = true
			}
			barPrinted = true
		case <-tick.C:
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
					line := renderProgress(percent, cur, remoteH)
					if tty {
						fmt.Fprintf(opts.Out, "\r\033[K%s", line)
					} else {
						fmt.Println(line)
					}
					firstBarTime = time.Now()
					holdStarted = true
					barPrinted = true
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
					line := renderProgress(percent, cur, remoteH)
					if tty {
						fmt.Fprintf(opts.Out, "\r\033[K%s", line)
					} else {
						fmt.Println(line)
					}
					continue
				}
				// End condition: catching_up is false AND minShow window has passed
				if !st.CatchingUp && holdStarted && time.Since(firstBarTime) >= minShow {
					if tty {
						fmt.Fprint(opts.Out, "\n✅ Node is fully synced!\n")
					} else {
						fmt.Fprintln(opts.Out, "Node is fully synced.")
					}
					return nil
				}
			}
		}
	}
}

// --- helpers ---

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
				time.Sleep(400 * time.Millisecond)
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
