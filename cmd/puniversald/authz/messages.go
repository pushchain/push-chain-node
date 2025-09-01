package authz

import (
	"fmt"
	"strconv"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	uetypes "github.com/rollchains/pchain/x/ue/types"
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

	case "/ue.v1.MsgVoteInbound":
		if len(msgArgs) < 9 {
			return nil, fmt.Errorf("MsgVoteInbound requires: <signer> <source-chain> <tx-hash> <sender> <recipient> <amount> <asset-addr> <log-index> <tx-type>")
		}
		signerAddr, err := sdk.AccAddressFromBech32(msgArgs[0])
		if err != nil {
			return nil, fmt.Errorf("invalid signer address: %w", err)
		}
		
		// Parse tx type (0=UNSPECIFIED, 1=SYNTHETIC, 2=FEE_ABSTRACTION)
		txTypeInt, err := strconv.Atoi(msgArgs[8])
		if err != nil {
			return nil, fmt.Errorf("invalid tx type (must be number 0-2): %w", err)
		}
		
		var txType uetypes.InboundTxType
		switch txTypeInt {
		case 0:
			txType = uetypes.InboundTxType_UNSPECIFIED_TX
		case 1:
			txType = uetypes.InboundTxType_SYNTHETIC
		case 2:
			txType = uetypes.InboundTxType_FEE_ABSTRACTION
		default:
			return nil, fmt.Errorf("invalid tx type %d (must be 0=UNSPECIFIED, 1=SYNTHETIC, 2=FEE_ABSTRACTION)", txTypeInt)
		}
		
		return &uetypes.MsgVoteInbound{
			Signer: signerAddr.String(),
			Inbound: &uetypes.Inbound{
				SourceChain: msgArgs[1],
				TxHash:      msgArgs[2],
				Sender:      msgArgs[3],
				Recipient:   msgArgs[4],
				Amount:      msgArgs[5],
				AssetAddr:   msgArgs[6],
				LogIndex:    msgArgs[7],
				TxType:      txType,
			},
		}, nil

	default:
		return nil, fmt.Errorf("unsupported message type: %s", msgType)
	}
}

