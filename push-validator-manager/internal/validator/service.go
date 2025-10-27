package validator

import "context"

// KeyInfo contains structured information about a created/existing key
type KeyInfo struct {
    Address  string // Cosmos address (push1...)
    Name     string // Key name
    Pubkey   string // Public key JSON
    Type     string // Key type (local, ledger, etc)
    Mnemonic string // Recovery mnemonic phrase (only set on creation)
}

// Service handles key ops, balances, validator detection, and registration flow.
type Service interface {
    EnsureKey(ctx context.Context, name string) (KeyInfo, error) // returns key info
    GetEVMAddress(ctx context.Context, addr string) (string, error) // returns hex/EVM address
    IsValidator(ctx context.Context, addr string) (bool, error)
    Balance(ctx context.Context, addr string) (string, error) // denom string for now
    Register(ctx context.Context, args RegisterArgs) (string, error) // returns tx hash
    Unjail(ctx context.Context, keyName string) (string, error) // returns tx hash
    WithdrawRewards(ctx context.Context, validatorAddr string, keyName string, includeCommission bool) (string, error) // returns tx hash
    Delegate(ctx context.Context, args DelegateArgs) (string, error) // returns tx hash
}

type RegisterArgs struct {
    Moniker string
    CommissionRate string
    MinSelfDelegation string
    Amount string
    KeyName string
}

type DelegateArgs struct {
    ValidatorAddress string
    Amount string
    KeyName string
}

// New returns a stub validator service.
func New() Service { return &noop{} }

type noop struct{}

func (n *noop) EnsureKey(ctx context.Context, name string) (KeyInfo, error) { return KeyInfo{}, nil }
func (n *noop) GetEVMAddress(ctx context.Context, addr string) (string, error) { return "", nil }
func (n *noop) IsValidator(ctx context.Context, addr string) (bool, error) { return false, nil }
func (n *noop) Balance(ctx context.Context, addr string) (string, error) { return "0", nil }
func (n *noop) Register(ctx context.Context, args RegisterArgs) (string, error) { return "", nil }
func (n *noop) Unjail(ctx context.Context, keyName string) (string, error) { return "", nil }
func (n *noop) WithdrawRewards(ctx context.Context, validatorAddr string, keyName string, includeCommission bool) (string, error) { return "", nil }
func (n *noop) Delegate(ctx context.Context, args DelegateArgs) (string, error) { return "", nil }

