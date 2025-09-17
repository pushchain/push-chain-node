package monitor

import "context"

// Service streams sync progress (WS-first, polling fallback) and computes ETA.
type Service interface {
    Run(ctx context.Context) (Result, error)
}

type Result struct {
    StartHeight int64
    TargetHeight int64
    FinalHeight int64
    DurationSec int64
}

// New returns a stub monitor service.
func New() Service { return &noop{} }

type noop struct{}

func (n *noop) Run(ctx context.Context) (Result, error) { return Result{}, nil }

