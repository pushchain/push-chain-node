package ante

import (
	"fmt" // Importing fmt for logging

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
	authante "github.com/cosmos/cosmos-sdk/x/auth/ante"
)

// NewAnteHandler returns an ante handler responsible for attempting to route an
// Ethereum or SDK transaction to an internal ante handler for performing
// transaction-level processing (e.g. fee payment, signature verification) before
// being passed onto its respective handler.
func NewAnteHandler(options HandlerOptions) sdk.AnteHandler {
	return func(
		ctx sdk.Context, tx sdk.Tx, sim bool,
	) (newCtx sdk.Context, err error) {
		var anteHandler sdk.AnteHandler

		// Logging the start of the handler function
		fmt.Println("Starting NewAnteHandler")

		// Checking if the transaction has extension options
		txWithExtensions, ok := tx.(authante.HasExtensionOptionsTx)
		if ok {
			fmt.Println("Transaction has extension options")

			opts := txWithExtensions.GetExtensionOptions()
			if len(opts) > 0 {
				// Logging extension options
				fmt.Printf("Extension options found: %v\n", opts)

				switch typeURL := opts[0].GetTypeUrl(); typeURL {
				case "/os.evm.v1.ExtensionOptionsEthereumTx":
					// handle as *evmtypes.MsgEthereumTx
					fmt.Println("Handling as Ethereum transaction")
					anteHandler = newMonoEVMAnteHandler(options)
				case "/os.types.v1.ExtensionOptionDynamicFeeTx":
					// cosmos-sdk tx with dynamic fee extension
					fmt.Println("Handling as Cosmos transaction with dynamic fees")
					anteHandler = NewCosmosAnteHandler(options)
				default:
					// Logging the unsupported extension type
					fmt.Printf("Unsupported extension option: %s\n", typeURL)
					return ctx, errorsmod.Wrapf(
						errortypes.ErrUnknownExtensionOptions,
						"rejecting tx with unsupported extension option: %s", typeURL,
					)
				}

				// Returning after handling the extension options
				return anteHandler(ctx, tx, sim)
			}
		}

		// Logging if no extension options are found
		fmt.Println("No extension options found, handling as normal Cosmos SDK transaction")

		// handle as totally normal Cosmos SDK tx
		switch tx.(type) {
		case sdk.Tx:
			// Logging Cosmos tx handling
			fmt.Println("Handling as standard Cosmos SDK tx")
			anteHandler = NewCosmosAnteHandler(options)
		default:
			// Logging invalid transaction type
			fmt.Printf("Invalid transaction type: %T\n", tx)
			return ctx, errorsmod.Wrapf(errortypes.ErrUnknownRequest, "invalid transaction type: %T", tx)
		}

		// Returning after handling the Cosmos tx
		return anteHandler(ctx, tx, sim)
	}
}

// LoggingDecorator wraps an AnteDecorator with logging
type LoggingDecorator struct {
	name string
	dec  sdk.AnteDecorator
}

// NewLoggingDecorator creates a new LoggingDecorator
func NewLoggingDecorator(name string, dec sdk.AnteDecorator) sdk.AnteDecorator {
	return &LoggingDecorator{
		name: name,
		dec:  dec,
	}
}

func (ld *LoggingDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, sim bool, next sdk.AnteHandler) (sdk.Context, error) {
	ctx.Logger().Info(fmt.Sprintf("Executing %s decorator", ld.name))
	newCtx, err := ld.dec.AnteHandle(ctx, tx, sim, next)
	if err != nil {
		ctx.Logger().Error(fmt.Sprintf("%s decorator failed", ld.name), "error", err)
	} else {
		ctx.Logger().Info(fmt.Sprintf("%s decorator succeeded", ld.name))
	}
	return newCtx, err
}
