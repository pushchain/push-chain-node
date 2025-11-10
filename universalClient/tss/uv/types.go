package uv

import "errors"

// UniversalValidator represents a Universal Validator with its metadata
type UniversalValidator struct {
	ValidatorAddress string   // Core validator address
	Pubkey           string   // Validator consensus public key
	Status           UVStatus // Current lifecycle status
	NetworkIP        string   // IP address or domain name (from NetworkInfo)
	JoinedAtBlock    int64    // Block height when added to UV set
}

// UVStatus represents the status of a Universal Validator
type UVStatus int32

const (
	UVStatusUnspecified  UVStatus = 0
	UVStatusActive       UVStatus = 1 // Fully active (votes + signs)
	UVStatusPendingJoin  UVStatus = 2 // Waiting for onboarding keygen / vote
	UVStatusPendingLeave UVStatus = 3 // Marked for removal (still active until TSS reshare)
	UVStatusInactive     UVStatus = 4 // No longer part of the validator set
)

var (
	ErrNoEligibleValidators = errors.New("no eligible validators found")
	ErrInvalidBlockNumber   = errors.New("invalid block number")
)

