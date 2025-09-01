package authz

import "slices"

// DefaultAllowedMsgTypes defines the default message types that can be executed via AuthZ by the hot key
var DefaultAllowedMsgTypes = []string{
	// Bank module messages
	"/cosmos.bank.v1beta1.MsgSend",

	// Staking module messages
	"/cosmos.staking.v1beta1.MsgDelegate",
	"/cosmos.staking.v1beta1.MsgUndelegate",

	// UE module messages
	"/ue.v1.MsgVoteInbound",
}

// IsAllowedMsgType checks if a message type is allowed for AuthZ execution
// This uses the default allowed message types
func IsAllowedMsgType(msgType string) bool {
	return slices.Contains(DefaultAllowedMsgTypes, msgType)
}
