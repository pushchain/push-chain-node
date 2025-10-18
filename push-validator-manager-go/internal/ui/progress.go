package ui

import (
    "fmt"
    "io"
    "time"
)

// Spinner is a tiny terminal spinner helper.
type Spinner struct {
    frames []rune
    idx    int
    out    io.Writer
    colors *ColorConfig
    prefix string
    delay  time.Duration
}

func NewSpinner(out io.Writer, prefix string) *Spinner {
    if out == nil { out = io.Discard }
    return &Spinner{
        frames: []rune{'⠋','⠙','⠹','⠸','⠼','⠴','⠦','⠧','⠇','⠏'},
        idx: 0,
        out: out,
        colors: NewColorConfig(),
        prefix: prefix,
        delay: 120 * time.Millisecond,
    }
}

func (s *Spinner) SetDelay(d time.Duration) { if d > 0 { s.delay = d } }

// Tick renders the next frame with prefix. Caller controls timing via time.Ticker.
func (s *Spinner) Tick() {
    if s.out == nil { return }
    frame := s.frames[s.idx%len(s.frames)]
    s.idx++
    msg := s.prefix
    if s.colors.Enabled { fmt.Fprintf(s.out, "\r%s %c", msg, frame) } else { fmt.Fprintf(s.out, "\r%s", msg) }
}

