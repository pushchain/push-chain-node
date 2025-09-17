package validator

import "context"

// Service handles key ops, balances, validator detection, and registration flow.
type Service interface {
    EnsureKey(ctx context.Context, name string) (string, error) // returns address
    IsValidator(ctx context.Context, addr string) (bool, error)
    Balance(ctx context.Context, addr string) (string, error) // denom string for now
    Register(ctx context.Context, args RegisterArgs) (string, error) // returns tx hash
}

type RegisterArgs struct {
    Moniker string
    CommissionRate string
    MinSelfDelegation string
    Amount string
    KeyName string
}

// New returns a stub validator service.
func New() Service { return &noop{} }

type noop struct{}

func (n *noop) EnsureKey(ctx context.Context, name string) (string, error) { return "", nil }
func (n *noop) IsValidator(ctx context.Context, addr string) (bool, error) { return false, nil }
func (n *noop) Balance(ctx context.Context, addr string) (string, error) { return "0", nil }
func (n *noop) Register(ctx context.Context, args RegisterArgs) (string, error) { return "", nil }

