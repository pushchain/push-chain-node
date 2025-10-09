package ui

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode"

	"golang.org/x/term"
)

// RunLogUIV2 shows logs with sticky footer at bottom
func RunLogUIV2(ctx context.Context, opts LogUIOptions) error {
	// 1. Check TTY
	stdin := int(os.Stdin.Fd())
	stdout := int(os.Stdout.Fd())
	if !term.IsTerminal(stdin) || !term.IsTerminal(stdout) || !opts.ShowFooter {
		return tailFollowSimple(ctx, opts.LogPath)
	}

	// 2. Get terminal size (need width for divider, height for footer placement)
	rows, cols, err := term.GetSize(stdout)
	if err != nil {
		return tailFollowSimple(ctx, opts.LogPath)
	}

	// 3. Enter raw mode for key handling
	oldState, err := term.MakeRaw(stdin)
	if err != nil {
		return tailFollowSimple(ctx, opts.LogPath)
	}
	defer term.Restore(stdin, oldState)

	// 4. Print minimal controls banner (keeps existing scrollback intact)
	if opts.BgKey == 0 {
		opts.BgKey = 'b'
	}
	bgLabel := fmt.Sprintf("%c", opts.BgKey)
	footerRaw := fmt.Sprintf("Controls: Ctrl+C to stop node | '%s' to run in background", bgLabel)
	if cols > 0 && len(footerRaw) > cols {
		footerRaw = footerRaw[:cols]
	}
	footerStyled := footerRaw
	if !opts.NoColor {
		footerStyled = "\x1b[1m" + footerRaw + "\x1b[0m"
	}

	var renderFooter func()
	if rows > 2 {
		renderFooter = func() {
			fmt.Fprint(os.Stdout, "\x1b7")
			if rows > 1 {
				fmt.Fprintf(os.Stdout, "\x1b[%d;1H\x1b[2K", rows-1)
			}
			fmt.Fprintf(os.Stdout, "\x1b[%d;1H\x1b[2K%s", rows, footerStyled)
			fmt.Fprint(os.Stdout, "\x1b8")
		}
		renderFooter()
	} else {
		renderFooter = func() {}
	}
	defer renderFooter()

	// 8. Start log tailing in goroutine
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	logDone := make(chan error, 1)
	go func() {
		logDone <- streamLogsSimple(ctx, opts.LogPath, renderFooter)
	}()

	// 9. Listen for keypresses
	keyDone := make(chan byte, 1)
	go func() {
		buf := make([]byte, 1)
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil || n == 0 {
				return
			}
			keyDone <- buf[0]
		}
	}()

	// 10. Wait for key or log error
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-logDone:
			return err
		case key := <-keyDone:
			upperBg := byte(unicode.ToUpper(rune(opts.BgKey)))
			switch key {
			case 3: // Ctrl+C
				fmt.Fprint(os.Stdout, "\r\nStopping node...\r\n")
				stopNodeSimple()
				return nil
			case opts.BgKey, upperBg:
				fmt.Fprint(os.Stdout, "\r\nDetaching...\r\n")
				return nil
			}
		}
	}
}

func streamLogsSimple(ctx context.Context, logPath string, onPrint func()) error {
	// Wait for file
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

	// Open and tail file
	f, err := os.Open(logPath)
	if err != nil {
		return err
	}
	defer f.Close()

	const backlogLines = 20
	// Emit recent history so the viewer isn't blank on start
	if err := printRecentLines(f, os.Stdout, backlogLines, onPrint); err != nil {
		return err
	}

	// Seek to end and continue streaming
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return err
	}

	reader := bufio.NewReader(f)
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

		// Print with \r\n for raw mode
		fmt.Fprint(os.Stdout, strings.TrimSuffix(line, "\n")+"\r\n")
		if onPrint != nil {
			onPrint()
		}
	}
}

func printRecentLines(f *os.File, out io.Writer, maxLines int, onPrint func()) error {
	if maxLines <= 0 {
		return nil
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	scanner := bufio.NewScanner(f)
	buf := make([]string, 0, maxLines)
	// allow long log lines up to 512 KiB
	bufSize := 512 * 1024
	scanner.Buffer(make([]byte, bufSize), bufSize)
	for scanner.Scan() {
		if len(buf) == maxLines {
			copy(buf, buf[1:])
			buf[len(buf)-1] = scanner.Text()
		} else {
			buf = append(buf, scanner.Text())
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	for _, line := range buf {
		fmt.Fprintf(out, "%s\r\n", line)
		if onPrint != nil {
			onPrint()
		}
	}
	return nil
}

func stopNodeSimple() {
	exe, _ := os.Executable()
	if exe == "" {
		exe = "push-validator-manager"
	}
	cmd := exec.Command(exe, "stop")
	cmd.Run()
}

func tailFollowSimple(ctx context.Context, logPath string) error {
	cmd := exec.CommandContext(ctx, "tail", "-F", logPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		cmd = exec.CommandContext(ctx, "tail", "-f", logPath)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}
	return nil
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
