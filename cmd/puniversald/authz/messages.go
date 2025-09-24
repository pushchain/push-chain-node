package authz

import (
	"fmt"
	"strconv"

	sdk "github.com/cosmos/cosmos-sdk/types"
	uetypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// ParseMessageFromArgs parses command line arguments into a message
func ParseMessageFromArgs(msgType string, msgArgs []string) (sdk.Msg, error) {
	switch msgType {
	case "/uexecutor.v1.MsgVoteInbound":
		if len(msgArgs) < 9 {
			return nil, fmt.Errorf("MsgVoteInbound requires: <signer> <source-chain> <tx-hash> <sender> <recipient> <amount> <asset-addr> <log-index> <tx-type>")
		}
		signerAddr, err := sdk.AccAddressFromBech32(msgArgs[0])
		if err != nil {
			return nil, fmt.Errorf("invalid signer address: %w", err)
		}

		// Parse tx type (0=UNSPECIFIED_TX, 1=GAS, 2=FUNDS, 3=FUNDS_AND_PAYLOAD, 4=GAS_AND_PAYLOAD)
		txTypeInt, err := strconv.Atoi(msgArgs[8])
		if err != nil {
			return nil, fmt.Errorf("invalid tx type (must be number 0-4): %w", err)
		}

		var txType uetypes.InboundTxType
		switch txTypeInt {
		case 0:
			txType = uetypes.InboundTxType_UNSPECIFIED_TX
		case 1:
			txType = uetypes.InboundTxType_GAS
		case 2:
			txType = uetypes.InboundTxType_FUNDS
		case 3:
			txType = uetypes.InboundTxType_FUNDS_AND_PAYLOAD
		case 4:
			txType = uetypes.InboundTxType_GAS_AND_PAYLOAD
		default:
			return nil, fmt.Errorf("invalid tx type: %d (must be 0-4)", txTypeInt)
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
