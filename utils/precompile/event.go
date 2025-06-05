package precompile_util

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	cmn "github.com/evmos/os/precompiles/common"
	"github.com/evmos/os/x/evm/core/vm"
)

// EmitEventWithArguments creates a new event with the specified event type, sender address,
// and additional arguments. It properly handles indexed topics and non-indexed data.
func EmitEventWithArguments(
	ctx sdk.Context,
	p cmn.Precompile,
	stateDB vm.StateDB,
	eventType string,
	senderAddress common.Address,
	packedArguments ...interface{},
) error {
	// Get the event definition
	event := p.Events[eventType]

	// Create topics array (event signature + indexed parameters)
	topics := make([]common.Hash, 2)
	topics[0] = event.ID // First topic is always event signature

	// Add sender address as indexed topic
	var err error
	topics[1], err = cmn.MakeTopic(senderAddress)
	if err != nil {
		return err
	}

	// If we have no additional arguments, we're done with topics
	if len(packedArguments) == 0 {
		// Empty data field
		stateDB.AddLog(&ethtypes.Log{
			Address:     p.Address(),
			Topics:      topics,
			Data:        []byte{},
			BlockNumber: uint64(ctx.BlockHeight()),
		})
		return nil
	}

	// Pack the non-indexed arguments for the data field
	// Using only the non-indexed parameters (typically starting from index 1)
	if len(event.Inputs) <= 1 {
		// No non-indexed parameters
		stateDB.AddLog(&ethtypes.Log{
			Address:     p.Address(),
			Topics:      topics,
			Data:        []byte{},
			BlockNumber: uint64(ctx.BlockHeight()),
		})
		return nil
	}

	// Make sure we have the right amount of arguments
	if len(packedArguments) != len(event.Inputs)-1 {
		return fmt.Errorf("argument count mismatch: got %d, want %d",
			len(packedArguments), len(event.Inputs)-1)
	}

	// Pack the arguments
	arguments := abi.Arguments{event.Inputs[1]} // Using the non-indexed parameter
	packed, err := arguments.Pack(packedArguments[0])
	if err != nil {
		return err
	}

	stateDB.AddLog(&ethtypes.Log{
		Address:     p.Address(),
		Topics:      topics,
		Data:        packed,
		BlockNumber: uint64(ctx.BlockHeight()),
	})

	return nil
}
