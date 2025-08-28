package authz

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func init() {
	// Initialize SDK config for tests if not already sealed
	sdkConfig := sdk.GetConfig()
	defer func() {
		// Config already sealed, that's fine - ignore panic
		_ = recover()
	}()
	sdkConfig.SetBech32PrefixForAccount("push", "pushpub")
	sdkConfig.SetBech32PrefixForValidator("pushvaloper", "pushvaloperpub")  
	sdkConfig.SetBech32PrefixForConsensusNode("pushvalcons", "pushvalconspub")
}