package constant

import "os"

// Node configuration constants
const (
	NodeDir = ".puniversal"
)

var (
	DefaultNodeHome = os.ExpandEnv("$HOME/") + NodeDir
)

// SupportedMessages contains all the supported message type URLs
// that the Universal Validator should process.
var SupportedMessages = []string{
	"/uexecutor.v1.MsgVoteInbound",
	"/uexecutor.v1.MsgVoteGasPrice",
	"/utss.v1.MsgVoteTssKeyProcess",
}
