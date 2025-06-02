package types_test

import (
	"fmt"
	"os"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

func TestMain(m *testing.M) {
	// Configure correct Bech32 prefix for your app
	config := sdk.GetConfig()
	fmt.Println(config)
	config.SetBech32PrefixForAccount("push", "pushpub")
	config.Seal()

	// Run tests
	os.Exit(m.Run())
}
