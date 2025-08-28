package authz

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

// ParseMessageFromArgs parses command line arguments into a message
func ParseMessageFromArgs(msgType string, msgArgs []string) (sdk.Msg, error) {
	switch msgType {
	case "/cosmos.bank.v1beta1.MsgSend":
		if len(msgArgs) < 3 {
			return nil, fmt.Errorf("MsgSend requires: <from-address> <to-address> <amount>")
		}
		fromAddr, err := sdk.AccAddressFromBech32(msgArgs[0])
		if err != nil {
			return nil, fmt.Errorf("invalid from address: %w", err)
		}
		toAddr, err := sdk.AccAddressFromBech32(msgArgs[1])
		if err != nil {
			return nil, fmt.Errorf("invalid to address: %w", err)
		}
		amount, err := sdk.ParseCoinsNormalized(msgArgs[2])
		if err != nil {
			return nil, fmt.Errorf("invalid amount: %w", err)
		}
		return &banktypes.MsgSend{
			FromAddress: fromAddr.String(),
			ToAddress:   toAddr.String(),
			Amount:      amount,
		}, nil

	case "/cosmos.staking.v1beta1.MsgDelegate":
		if len(msgArgs) < 3 {
			return nil, fmt.Errorf("MsgDelegate requires: <delegator-address> <validator> <amount>")
		}
		delegatorAddr, err := sdk.AccAddressFromBech32(msgArgs[0])
		if err != nil {
			return nil, fmt.Errorf("invalid delegator address: %w", err)
		}
		validatorAddr := msgArgs[1]
		amount, err := sdk.ParseCoinNormalized(msgArgs[2])
		if err != nil {
			return nil, fmt.Errorf("invalid amount: %w", err)
		}
		return &stakingtypes.MsgDelegate{
			DelegatorAddress: delegatorAddr.String(),
			ValidatorAddress: validatorAddr,
			Amount:           amount,
		}, nil

	case "/cosmos.staking.v1beta1.MsgUndelegate":
		if len(msgArgs) < 3 {
			return nil, fmt.Errorf("MsgUndelegate requires: <delegator-address> <validator> <amount>")
		}
		delegatorAddr, err := sdk.AccAddressFromBech32(msgArgs[0])
		if err != nil {
			return nil, fmt.Errorf("invalid delegator address: %w", err)
		}
		validatorAddr := msgArgs[1]
		amount, err := sdk.ParseCoinNormalized(msgArgs[2])
		if err != nil {
			return nil, fmt.Errorf("invalid amount: %w", err)
		}
		return &stakingtypes.MsgUndelegate{
			DelegatorAddress: delegatorAddr.String(),
			ValidatorAddress: validatorAddr,
			Amount:           amount,
		}, nil

	default:
		return nil, fmt.Errorf("unsupported message type: %s", msgType)
	}
}

