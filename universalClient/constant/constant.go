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
