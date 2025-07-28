package constant

import "os"

const (
	appName = "puniversal"
	NodeDir = ".puniversal"
)

var (
	DefaultNodeHome = os.ExpandEnv("$HOME/") + NodeDir
)
