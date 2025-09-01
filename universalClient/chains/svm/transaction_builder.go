package svm

import (
	"context"
	"encoding/binary"
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/programs/system"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/rs/zerolog"
)

// TransactionBuilder handles building Solana transactions for the gateway
type TransactionBuilder struct {
	parentClient *Client
	gatewayAddr  solana.PublicKey
	logger       zerolog.Logger
}

// NewTransactionBuilder creates a new transaction builder
func NewTransactionBuilder(
	parentClient *Client,
	gatewayAddr solana.PublicKey,
	logger zerolog.Logger,
) *TransactionBuilder {
	return &TransactionBuilder{
		parentClient: parentClient,
		gatewayAddr:  gatewayAddr,
		logger:       logger.With().Str("component", "svm_tx_builder").Logger(),
	}
}

// BuildGatewayTransaction builds a transaction for gateway operations
func (tb *TransactionBuilder) BuildGatewayTransaction(
	ctx context.Context,
	from solana.PrivateKey,
	instruction solana.Instruction,
) (*solana.Transaction, error) {
	// Get recent blockhash
	var recentBlockhash *rpc.GetRecentBlockhashResult
	err := tb.parentClient.executeWithFailover(ctx, "get_recent_blockhash", func(client *rpc.Client) error {
		var innerErr error
		recentBlockhash, innerErr = client.GetRecentBlockhash(ctx, rpc.CommitmentFinalized)
		return innerErr
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get recent blockhash: %w", err)
	}

	// Build transaction
	tx, err := solana.NewTransaction(
		[]solana.Instruction{instruction},
		recentBlockhash.Value.Blockhash,
		solana.TransactionPayer(from.PublicKey()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create transaction: %w", err)
	}

	// Sign transaction
	_, err = tx.Sign(func(key solana.PublicKey) *solana.PrivateKey {
		if key.Equals(from.PublicKey()) {
			return &from
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to sign transaction: %w", err)
	}

	return tx, nil
}

// CreateAddFundsInstruction creates an instruction for adding funds
func (tb *TransactionBuilder) CreateAddFundsInstruction(
	from solana.PublicKey,
	amount uint64,
	payload []byte,
) (solana.Instruction, error) {
	// Create instruction data
	// Format: [8 bytes discriminator] + [8 bytes amount] + [variable payload]
	discriminator := []byte{0x84, 0xed, 0x4c, 0x39, 0x50, 0x0a, 0xb3, 0x8a} // add_funds
	
	data := make([]byte, 16+len(payload))
	copy(data[0:8], discriminator)
	binary.LittleEndian.PutUint64(data[8:16], amount)
	if len(payload) > 0 {
		copy(data[16:], payload)
	}

	// Build instruction
	instruction := &solana.GenericInstruction{
		ProgID: tb.gatewayAddr,
		AccountValues: solana.AccountMetaSlice{
			&solana.AccountMeta{
				PublicKey:  from,
				IsWritable: true,
				IsSigner:   true,
			},
			&solana.AccountMeta{
				PublicKey:  tb.gatewayAddr,
				IsWritable: true,
				IsSigner:   false,
			},
			&solana.AccountMeta{
				PublicKey:  solana.SystemProgramID,
				IsWritable: false,
				IsSigner:   false,
			},
		},
		DataBytes: data,
	}

	return instruction, nil
}

// CreateTransferInstruction creates a native SOL transfer instruction
func (tb *TransactionBuilder) CreateTransferInstruction(
	from solana.PublicKey,
	to solana.PublicKey,
	amount uint64,
) (solana.Instruction, error) {
	return system.NewTransferInstruction(
		amount,
		from,
		to,
	).Build(), nil
}

// SendTransaction sends a transaction to the network
func (tb *TransactionBuilder) SendTransaction(
	ctx context.Context,
	tx *solana.Transaction,
) (solana.Signature, error) {
	var sig solana.Signature
	err := tb.parentClient.executeWithFailover(ctx, "send_transaction", func(client *rpc.Client) error {
		var innerErr error
		sig, innerErr = client.SendTransactionWithOpts(
			ctx,
			tx,
			rpc.TransactionOpts{
				SkipPreflight:       false,
				PreflightCommitment: rpc.CommitmentConfirmed,
			},
		)
		return innerErr
	})
	if err != nil {
		return solana.Signature{}, fmt.Errorf("failed to send transaction: %w", err)
	}

	tb.logger.Info().
		Str("signature", sig.String()).
		Msg("transaction sent successfully")

	return sig, nil
}

// WaitForConfirmation waits for a transaction to be confirmed
func (tb *TransactionBuilder) WaitForConfirmation(
	ctx context.Context,
	sig solana.Signature,
	commitment rpc.CommitmentType,
) error {
	// Poll for transaction status
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			var statuses *rpc.GetSignatureStatusesResult
			err := tb.parentClient.executeWithFailover(ctx, "get_signature_statuses", func(client *rpc.Client) error {
				var innerErr error
				statuses, innerErr = client.GetSignatureStatuses(ctx, false, sig)
				return innerErr
			})
			if err != nil {
				tb.logger.Debug().Err(err).Msg("error checking transaction status")
				continue
			}

			if len(statuses.Value) > 0 && statuses.Value[0] != nil {
				status := statuses.Value[0]
				
				// Check if we've reached desired commitment level
				switch commitment {
				case rpc.CommitmentProcessed:
					if status.ConfirmationStatus >= rpc.ConfirmationStatusProcessed {
						return nil
					}
				case rpc.CommitmentConfirmed:
					if status.ConfirmationStatus >= rpc.ConfirmationStatusConfirmed {
						return nil
					}
				case rpc.CommitmentFinalized:
					if status.ConfirmationStatus >= rpc.ConfirmationStatusFinalized {
						return nil
					}
				}
			}
		}
	}
}