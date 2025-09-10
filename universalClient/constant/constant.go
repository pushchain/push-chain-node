package constant

import "os"

// Node configuration constants
const (
	NodeDir = ".puniversal"
)

var (
	DefaultNodeHome = os.ExpandEnv("$HOME/") + NodeDir
)

// TXType represents the type of transaction
type TXType int

const (
	// GAS is only for funding the UEA on Push Chain with GAS.
	// Doesn't support movement of high-value funds or payload execution.
	GAS TXType = iota

	// GAS_AND_PAYLOAD funds UEA and executes a payload instantly via UEA on Push Chain.
	// Allows movement of funds between low cap ranges and requires lower block confirmations.
	GAS_AND_PAYLOAD

	// FUNDS is for bridging large funds only from external chain to Push Chain.
	// Doesn't support arbitrary payload movement and requires longer block confirmations.
	FUNDS

	// FUNDS_AND_PAYLOAD bridges both funds and payload to Push Chain for execution.
	// No strict cap ranges for fund amount and requires longer block confirmations.
	FUNDS_AND_PAYLOAD
)

// SupportedMessages contains all the supported message type URLs
// that the Universal Validator should process.
var SupportedMessages = []string{
	"/uexecutor.v1.MsgVoteInbound",
	// TODO: Add More Messages here as supported by Chain
}
