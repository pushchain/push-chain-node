package controlpanel

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/evmos/os/x/evm/core/vm"

	util "github.com/rollchains/pchain/util"
)

const (
	ParamsMethod = "getParams"
)

type ValidatorWhitelist struct {
	Enabled   bool     `json:"enabled"`
	Addresses []string `json:"addresses"`
}

type Params struct {
	AdminAddress string
	ValidatorWhitelist
}

func (p Precompile) GetParams(
	ctx sdk.Context,
	_ *vm.Contract,
	method *abi.Method,
	_ []interface{},
) ([]byte, error) {
	res := Params{
		AdminAddress: "push1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
		ValidatorWhitelist: ValidatorWhitelist{
			Enabled: false,
			Addresses: []string{
				"push1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
			},
		},
	}

	bz, err := util.ConvertAnyAddressToBytes(res.AdminAddress)
	if err != nil {
		return nil, fmt.Errorf("error converting address to bytes: %s", err)
	}

	// must match the output of the .sol ABI interface
	return method.Outputs.Pack(
		common.BytesToAddress(bz),
		ValidatorWhitelist{
			Enabled:   res.ValidatorWhitelist.Enabled,
			Addresses: res.ValidatorWhitelist.Addresses,
		},
	)
}
