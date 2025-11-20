package node

import (
	"context"
	"time"

	"github.com/rs/zerolog"

	"github.com/pushchain/push-chain-node/universalClient/db"
	"github.com/pushchain/push-chain-node/universalClient/tss"
)

const (
	statusPending    = "PENDING"
	statusInProgress = "IN_PROGRESS"
	statusSuccess    = "SUCCESS"
	statusFailed     = "FAILED"
	statusExpired    = "EXPIRED"
)

// PushChainDataProvider provides access to Push Chain data.
type PushChainDataProvider interface {
	GetLatestBlockNum(ctx context.Context) (uint64, error)
	GetUniversalValidators(ctx context.Context) ([]*tss.UniversalValidator, error)
	GetUniversalValidator(ctx context.Context, validatorAddress string) (*tss.UniversalValidator, error)
}

// Config holds configuration for initializing a TSS node.
type Config struct {
	ValidatorAddress string
	PrivateKeyHex    string
	LibP2PListen     string
	HomeDir          string
	Password         string
	Database         *db.DB
	DataProvider     PushChainDataProvider
	Logger           zerolog.Logger

	// Optional configuration
	PollInterval      time.Duration
	ProcessingTimeout time.Duration
	CoordinatorRange  uint64
	SetupTimeout      time.Duration
	MessageTimeout    time.Duration
	ProtocolID        string
	DialTimeout       time.Duration
	IOTimeout         time.Duration
}
