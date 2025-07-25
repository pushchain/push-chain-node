package types

import (
	"encoding/json"
	"strings"

	"cosmossdk.io/errors"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// Stringer method for TokenConfig
func (p TokenConfig) String() string {
	bz, err := json.Marshal(p)
	if err != nil {
		panic(err)
	}
	return string(bz)
}

// ValidateBasic performs sanity checks on the TokenConfig
func (p TokenConfig) ValidateBasic() error {
	if strings.TrimSpace(p.Chain) == "" {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "chain cannot be empty")
	}

	if strings.TrimSpace(p.Address) == "" {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "token contract address cannot be empty")
	}

	if strings.TrimSpace(p.Name) == "" {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "token name cannot be empty")
	}

	if strings.TrimSpace(p.Symbol) == "" {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "token symbol cannot be empty")
	}

	if p.Decimals == 0 {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "decimals must be greater than zero")
	}

	if strings.TrimSpace(p.LiquidityCap) == "" {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "liquidity_cap cannot be empty")
	}

	if err := p.NativeRepresentation.ValidateBasic(); err != nil {
		return errors.Wrap(err, "invalid native representation")
	}

	return nil
}
