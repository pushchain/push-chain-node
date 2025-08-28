package authz

import (
	"fmt"
	"strconv"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types/v1beta1"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

// ParseMessageFromArgs parses command line arguments into a message
func ParseMessageFromArgs(msgType string, granterAddr sdk.AccAddress, msgArgs []string) (sdk.Msg, error) {
	switch msgType {
	case "/cosmos.bank.v1beta1.MsgSend":
		if len(msgArgs) < 2 {
			return nil, fmt.Errorf("MsgSend requires: <to-address> <amount>")
		}
		toAddr, err := sdk.AccAddressFromBech32(msgArgs[0])
		if err != nil {
			return nil, fmt.Errorf("invalid to address: %w", err)
		}
		amount, err := sdk.ParseCoinsNormalized(msgArgs[1])
		if err != nil {
			return nil, fmt.Errorf("invalid amount: %w", err)
		}
		return &banktypes.MsgSend{
			FromAddress: granterAddr.String(),
			ToAddress:   toAddr.String(),
			Amount:      amount,
		}, nil

	case "/cosmos.staking.v1beta1.MsgDelegate":
		if len(msgArgs) < 2 {
			return nil, fmt.Errorf("MsgDelegate requires: <validator> <amount>")
		}
		validatorAddr := msgArgs[0]
		amount, err := sdk.ParseCoinNormalized(msgArgs[1])
		if err != nil {
			return nil, fmt.Errorf("invalid amount: %w", err)
		}
		return &stakingtypes.MsgDelegate{
			DelegatorAddress: granterAddr.String(),
			ValidatorAddress: validatorAddr,
			Amount:           amount,
		}, nil

	case "/cosmos.staking.v1beta1.MsgUndelegate":
		if len(msgArgs) < 2 {
			return nil, fmt.Errorf("MsgUndelegate requires: <validator> <amount>")
		}
		validatorAddr := msgArgs[0]
		amount, err := sdk.ParseCoinNormalized(msgArgs[1])
		if err != nil {
			return nil, fmt.Errorf("invalid amount: %w", err)
		}
		return &stakingtypes.MsgUndelegate{
			DelegatorAddress: granterAddr.String(),
			ValidatorAddress: validatorAddr,
			Amount:           amount,
		}, nil

	case "/cosmos.gov.v1beta1.MsgVote":
		if len(msgArgs) < 2 {
			return nil, fmt.Errorf("MsgVote requires: <proposal-id> <option>")
		}
		proposalID, err := strconv.ParseUint(msgArgs[0], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid proposal ID: %w", err)
		}
		
		var option govtypes.VoteOption
		switch msgArgs[1] {
		case "yes":
			option = govtypes.OptionYes
		case "no":
			option = govtypes.OptionNo
		case "abstain":
			option = govtypes.OptionAbstain
		case "no_with_veto":
			option = govtypes.OptionNoWithVeto
		default:
			return nil, fmt.Errorf("invalid vote option: %s (use: yes, no, abstain, no_with_veto)", msgArgs[1])
		}
		return &govtypes.MsgVote{
			ProposalId: proposalID,
			Voter:      granterAddr.String(),
			Option:     option,
		}, nil

	default:
		return nil, fmt.Errorf("unsupported message type: %s", msgType)
	}
}

