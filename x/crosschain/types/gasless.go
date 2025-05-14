package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func IsGaslessTx(tx sdk.Tx) bool {
	msgs := tx.GetMsgs()
	for _, msg := range msgs {
		url := sdk.MsgTypeURL(msg)
		if url != sdk.MsgTypeURL(&MsgExecutePayload{}) &&
			url != sdk.MsgTypeURL(&MsgDeployNMSC{}) &&
			url != sdk.MsgTypeURL(&MsgMintPush{}) {
			return false
		}
	}
	return true
}
