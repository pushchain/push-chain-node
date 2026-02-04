package constant

import "os"

// <NodeDir>/                    (e.g., /home/universal/.puniversal)
// └── config/
//	└── pushuv_config.json
// └── databases/
//	└── eip155_1.db
//	└── eip155_97.db

const (
	NodeDir = ".puniversal"

	ConfigSubdir   = "config"
	ConfigFileName = "pushuv_config.json"

	DatabasesSubdir = "databases"

	RelayerSubdir = "relayer"
)

var DefaultNodeHome = os.ExpandEnv("$HOME/") + NodeDir

// RequiredMsgGrants contains all the required message type URLs
// that must be granted via AuthZ for the Universal Validator to function.
// These messages are executed on behalf of the core validator by the grantee (hotkey of the Universal Validator).
var RequiredMsgGrants = []string{
	"/uexecutor.v1.MsgVoteInbound",
	"/uexecutor.v1.MsgVoteGasPrice",
	"/uexecutor.v1.MsgVoteOutbound",
	"/utss.v1.MsgVoteTssKeyProcess",
}
