package keeper

import (
	"context"
	"fmt"

<<<<<<< HEAD:x/uexecutor/keeper/execute_inbound.go
	"github.com/pushchain/push-chain-node/x/uexecutor/types"
=======
	"github.com/rollchains/pchain/x/ue/types"
>>>>>>> feat/universal-validator:x/ue/keeper/execute_inbound.go
)

func (k Keeper) ExecuteInbound(ctx context.Context, utx types.UniversalTx) error {
	switch utx.InboundTx.TxType {
	case types.InboundTxType_SYNTHETIC:
		return k.ExecuteInboundSynthetic(ctx, utx)
	case types.InboundTxType_FEE_ABSTRACTION:
		// return k.handleInboundFeeAbs(ctx, utx)
		return nil
	default:
		return fmt.Errorf("unsupported inbound tx type: %d", utx.InboundTx.TxType)
	}
}
