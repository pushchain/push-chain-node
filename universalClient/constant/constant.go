package constant

import "os"

const (
	NodeDir = ".puniversal"
)

var (
	DefaultNodeHome = os.ExpandEnv("$HOME/") + NodeDir
)
