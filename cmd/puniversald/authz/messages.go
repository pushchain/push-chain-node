package authz

import (
	"fmt"
	"strconv"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types/v1beta1"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

// MessageFactory handles creation of various message types
type MessageFactory struct{}

// NewMessageFactory creates a new message factory instance
func NewMessageFactory() *MessageFactory {
	return &MessageFactory{}
}

// CreateMsgSend creates a MsgSend message
func (mf *MessageFactory) CreateMsgSend(fromAddr, toAddr sdk.AccAddress, amount sdk.Coins) sdk.Msg {
	return &banktypes.MsgSend{
		FromAddress: fromAddr.String(),
		ToAddress:   toAddr.String(),
		Amount:      amount,
	}
}

// CreateMsgDelegate creates a MsgDelegate message
func (mf *MessageFactory) CreateMsgDelegate(delegatorAddr sdk.AccAddress, validatorAddr string, amount sdk.Coin) sdk.Msg {
	return &stakingtypes.MsgDelegate{
		DelegatorAddress: delegatorAddr.String(),
		ValidatorAddress: validatorAddr,
		Amount:           amount,
	}
}

// CreateMsgUndelegate creates a MsgUndelegate message
func (mf *MessageFactory) CreateMsgUndelegate(delegatorAddr sdk.AccAddress, validatorAddr string, amount sdk.Coin) sdk.Msg {
	return &stakingtypes.MsgUndelegate{
		DelegatorAddress: delegatorAddr.String(),
		ValidatorAddress: validatorAddr,
		Amount:           amount,
	}
}

// CreateMsgVote creates a MsgVote message
func (mf *MessageFactory) CreateMsgVote(voterAddr sdk.AccAddress, proposalID uint64, option govtypes.VoteOption) sdk.Msg {
	return &govtypes.MsgVote{
		ProposalId: proposalID,
		Voter:      voterAddr.String(),
		Option:     option,
	}
}


// ParseMessageFromArgs parses command line arguments into a message
func (mf *MessageFactory) ParseMessageFromArgs(msgType string, granterAddr sdk.AccAddress, msgArgs []string) (sdk.Msg, error) {
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
		return mf.CreateMsgSend(granterAddr, toAddr, amount), nil

	case "/cosmos.staking.v1beta1.MsgDelegate":
		if len(msgArgs) < 2 {
			return nil, fmt.Errorf("MsgDelegate requires: <validator> <amount>")
		}
		validatorAddr := msgArgs[0]
		amount, err := sdk.ParseCoinNormalized(msgArgs[1])
		if err != nil {
			return nil, fmt.Errorf("invalid amount: %w", err)
		}
		return mf.CreateMsgDelegate(granterAddr, validatorAddr, amount), nil

	case "/cosmos.staking.v1beta1.MsgUndelegate":
		if len(msgArgs) < 2 {
			return nil, fmt.Errorf("MsgUndelegate requires: <validator> <amount>")
		}
		validatorAddr := msgArgs[0]
		amount, err := sdk.ParseCoinNormalized(msgArgs[1])
		if err != nil {
			return nil, fmt.Errorf("invalid amount: %w", err)
		}
		return mf.CreateMsgUndelegate(granterAddr, validatorAddr, amount), nil

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
		return mf.CreateMsgVote(granterAddr, proposalID, option), nil

	default:
		return nil, fmt.Errorf("unsupported message type: %s", msgType)
	}
}

// GetSupportedMessageTypes returns a map of supported message types with their descriptions
func (mf *MessageFactory) GetSupportedMessageTypes() map[string]string {
	return map[string]string{
		"/cosmos.bank.v1beta1.MsgSend":           "Send tokens - requires: <to-addr> <amount>",
		"/cosmos.staking.v1beta1.MsgDelegate":    "Delegate tokens - requires: <validator> <amount>",
		"/cosmos.staking.v1beta1.MsgUndelegate":  "Undelegate tokens - requires: <validator> <amount>",
		"/cosmos.gov.v1beta1.MsgVote":           "Vote on proposal - requires: <proposal-id> <option>",
	}
}