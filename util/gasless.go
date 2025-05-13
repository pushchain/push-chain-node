package util

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	crosschaintypes "github.com/rollchains/pchain/x/crosschain/types"
)

func IsGaslessTx(tx sdk.Tx) bool {
	msgs := tx.GetMsgs()
	for _, msg := range msgs {
		url := sdk.MsgTypeURL(msg)
		if url != sdk.MsgTypeURL(&crosschaintypes.MsgExecutePayload{}) &&
			url != sdk.MsgTypeURL(&crosschaintypes.MsgDeployNMSC{}) &&
			url != sdk.MsgTypeURL(&crosschaintypes.MsgMintPush{}) {
			return false
		}
	}
	return true
}
