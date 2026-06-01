package keeper

import (
	"context"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

// StakingHooks implements stakingtypes.StakingHooks. AfterValidatorBeginUnbonding
// and AfterValidatorBonded keep UV lifecycle in sync with base-chain state;
// the rest are no-op stubs.
type StakingHooks struct {
	k Keeper
}

var _ stakingtypes.StakingHooks = StakingHooks{}

func (k Keeper) StakingHooks() StakingHooks { return StakingHooks{k} }

func (h StakingHooks) AfterValidatorBeginUnbonding(ctx context.Context, _ sdk.ConsAddress, valAddr sdk.ValAddress) error {
	h.k.HandleBaseValidatorUnbonding(sdk.UnwrapSDKContext(ctx), valAddr)
	return nil
}

func (h StakingHooks) AfterValidatorBonded(ctx context.Context, _ sdk.ConsAddress, valAddr sdk.ValAddress) error {
	h.k.HandleBaseValidatorBonded(sdk.UnwrapSDKContext(ctx), valAddr)
	return nil
}


func (h StakingHooks) AfterValidatorCreated(_ context.Context, _ sdk.ValAddress) error {
	return nil
}

func (h StakingHooks) BeforeValidatorModified(_ context.Context, _ sdk.ValAddress) error {
	return nil
}

func (h StakingHooks) AfterValidatorRemoved(_ context.Context, _ sdk.ConsAddress, _ sdk.ValAddress) error {
	return nil
}

func (h StakingHooks) BeforeDelegationCreated(_ context.Context, _ sdk.AccAddress, _ sdk.ValAddress) error {
	return nil
}

func (h StakingHooks) BeforeDelegationSharesModified(_ context.Context, _ sdk.AccAddress, _ sdk.ValAddress) error {
	return nil
}

func (h StakingHooks) BeforeDelegationRemoved(_ context.Context, _ sdk.AccAddress, _ sdk.ValAddress) error {
	return nil
}

func (h StakingHooks) AfterDelegationModified(_ context.Context, _ sdk.AccAddress, _ sdk.ValAddress) error {
	return nil
}

func (h StakingHooks) BeforeValidatorSlashed(_ context.Context, _ sdk.ValAddress, _ math.LegacyDec) error {
	return nil
}

func (h StakingHooks) AfterUnbondingInitiated(_ context.Context, _ uint64) error {
	return nil
}
