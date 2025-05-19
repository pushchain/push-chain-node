package types

import (
	"slices"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

var (
	// GaslessMsgTypes defines the message types that are allowed in gasless transactions
	GaslessMsgTypes = []string{
		sdk.MsgTypeURL(&MsgExecutePayload{}),
		sdk.MsgTypeURL(&MsgDeployNMSC{}),
		sdk.MsgTypeURL(&MsgMintPush{}),
	}
)

// IsGaslessTx checks if a transaction contains only allowed gasless message types
// Returns true if all messages in the transaction are in the allowed gasless message types
func IsGaslessTx(tx sdk.Tx) bool {
	msgs := tx.GetMsgs()
	if len(msgs) == 0 {
		return false
	}

	for _, msg := range msgs {
		url := sdk.MsgTypeURL(msg)
		isAllowed := slices.Contains(GaslessMsgTypes, url)
		if !isAllowed {
			return false
		}
	}
	return true
}
