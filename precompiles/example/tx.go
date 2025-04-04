package controlpanel

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	cmn "github.com/evmos/os/precompiles/common"
	"github.com/evmos/os/x/evm/core/vm"
	util "github.com/rollchains/pchain/util"
	"github.com/rollchains/pchain/x/precompileexa/keeper"
	"github.com/rollchains/pchain/x/precompileexa/types"
)

const (
	UpdateParamsMethod = "updateParams"

	ErrDifferentOrigin = "tx origin address %s does not match the sender address %s"
)

// UpdateParams defines a method to update params.
func (p Precompile) UpdateParams(
	ctx sdk.Context,
	origin common.Address,
	contract *vm.Contract,
	stateDB vm.StateDB,
	method *abi.Method,
	args []interface{},
) ([]byte, error) {
	msg, authority, err := NewMsgUpdateParams(args)
	if err != nil {
		return nil, err
	}

	// If the contract is the executor, we don't need an origin check
	// Otherwise check if the origin matches the sender address
	isContractExec := contract.CallerAddress == authority && contract.CallerAddress != origin
	if !isContractExec && origin != authority {
		return nil, fmt.Errorf(ErrDifferentOrigin, origin.String(), authority.String())
	}

	msgSrv := keeper.NewMsgServerImpl(p.keeper)
	if _, err = msgSrv.UpdateParams(ctx, msg); err != nil {
		return nil, fmt.Errorf("error updating params in precompile: %s", err)
	}

	params, err := p.keeper.Params.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("error getting params in precompile: %s", err)
	}

	if err = p.EmitUpdateParamsEvent(ctx, stateDB, authority, params); err != nil {
		return nil, err
	}

	return method.Outputs.Pack(true)
}

// NewMsgUpdateParams creates a new instance.
func NewMsgUpdateParams(args []interface{}) (*types.MsgUpdateParams, common.Address, error) {
	if len(args) != 4 {
		return nil, common.Address{}, fmt.Errorf(cmn.ErrInvalidNumberOfArgs, 4, len(args))
	}

	authority := args[0].(string)
	authorityBz, err := util.ConvertAnyAddressToBytes(authority)
	if err != nil {
		return nil, common.Address{}, fmt.Errorf("invalid address: %s", authority)
	}

	// if errors, caught by msg.Validate()
	adminAddress := args[1].(string)
	enabled := args[2].(bool)
	// addresses := args[3].([]string) // ignored for this example

	msg := &types.MsgUpdateParams{
		Authority: sdk.AccAddress(authorityBz).String(),
		Params: types.Params{
			AdminAddress: adminAddress,
			SomeValue:    enabled,
		},
	}

	if err := msg.Validate(); err != nil {
		return nil, common.Address{}, fmt.Errorf("invalid message: %s", err)
	}

	return msg, common.Address(authorityBz), nil
}

func (p Precompile) EmitUpdateParamsEvent(ctx sdk.Context, stateDB vm.StateDB, authorityAddress common.Address, params types.Params) error {
	event := p.ABI.Events["UpdateParams"]

	authorityTopic, err := cmn.MakeTopic(authorityAddress)
	if err != nil {
		return err
	}

	// The first topic is always the signature of the event.
	topics := []common.Hash{event.ID, authorityTopic}

	admin := params.AdminAddress

	// Prepare the event data
	packed, err := event.Inputs.Pack(authorityAddress.String(), admin, params.SomeValue, []string{}) //nolint:gosec // G115
	if err != nil {
		return err
	}

	stateDB.AddLog(&ethtypes.Log{
		Address:     p.Address(),
		Topics:      topics,
		Data:        packed,
		BlockNumber: uint64(ctx.BlockHeight()), //nolint:gosec // G115
	})

	return nil
}
