package types

import (
	"slices"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
)

// IsGaslessTx checks if a transaction contains only allowed gasless message types
// Returns true if all messages in the transaction are in the allowed gasless message types
func IsGaslessTx(tx sdk.Tx) bool {
	var (
		// GaslessMsgTypes defines the message types that are allowed in gasless transactions
		GaslessMsgTypes = []string{
			sdk.MsgTypeURL(&MsgExecutePayload{}),
			sdk.MsgTypeURL(&MsgDeployUEA{}),
			sdk.MsgTypeURL(&MsgMintPC{}),
			sdk.MsgTypeURL(&MsgVoteInbound{}),
		}
	)

	msgs := tx.GetMsgs()
	if len(msgs) == 0 {
		return false
	}

	for _, msg := range msgs {
		switch m := msg.(type) {
		case *authz.MsgExec:
			// Only gasless if ALL inner messages are allowed
			for _, innerMsg := range m.Msgs {
				if !slices.Contains(GaslessMsgTypes, sdk.MsgTypeURL(innerMsg)) {
					return false
				}
			}
		default:
			if !slices.Contains(GaslessMsgTypes, sdk.MsgTypeURL(msg)) {
				return false
			}
		}
	}
	return true
}
