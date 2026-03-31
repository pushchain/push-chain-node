package pushsigner

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/codec"
	cosmoskeyring "github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdktx "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	cosmosauthz "github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types/v1beta1"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/rs/zerolog"

	"github.com/pushchain/push-chain-node/universalClient/config"
	"github.com/pushchain/push-chain-node/universalClient/pushcore"
	"github.com/pushchain/push-chain-node/universalClient/pushsigner/keys"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// chainClient defines the subset of pushcore.Client methods used by Signer.
// Defined here (consumer-side) so tests can provide mock implementations.
type chainClient interface {
	BroadcastTx(ctx context.Context, txBytes []byte) (*sdktx.BroadcastTxResponse, error)
	GetTx(ctx context.Context, txHash string) (*sdktx.GetTxResponse, error)
	GetAccount(ctx context.Context, address string) (*authtypes.QueryAccountResponse, error)
	GetGranteeGrants(ctx context.Context, granteeAddr string) (*cosmosauthz.QueryGranteeGrantsResponse, error)
}

// Signer provides the main public API for signing and voting operations.
type Signer struct {
	keys          *keys.Keys
	clientCtx     client.Context
	pushCore      chainClient
	granter       string
	log           zerolog.Logger
	sequenceMutex sync.Mutex // Mutex to synchronize transaction signing
	lastSequence  uint64     // Track the last used sequence
}

// New creates a new Signer instance with validation.
func New(
	ctx context.Context,
	log zerolog.Logger,
	keyringBackend config.KeyringBackend,
	keyringPassword string,
	nodeHome string,
	pushCore *pushcore.Client,
	chainID string,
	granter string,
) (*Signer, error) {
	log.Info().Msg("Validating hotkey and AuthZ permissions...")

	validationResult, err := validateKeysAndGrants(ctx, keyringBackend, keyringPassword, nodeHome, pushCore, granter)
	if err != nil {
		log.Error().Err(err).Msg("PushSigner validation failed")
		return nil, fmt.Errorf("PushSigner validation failed: %w", err)
	}

	keyAddress, err := sdk.AccAddressFromBech32(validationResult.KeyAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse key address: %w", err)
	}

	universalKeys := keys.NewKeys(
		validationResult.Keyring,
		validationResult.KeyName,
	)

	derivedAddr, err := universalKeys.GetAddress()
	if err != nil {
		return nil, fmt.Errorf("failed to get address from keys: %w", err)
	}
	if !derivedAddr.Equals(keyAddress) {
		return nil, fmt.Errorf("key address mismatch: expected %s, got %s", keyAddress, derivedAddr)
	}

	// Validate keyring is accessible before creating client context
	if _, err := universalKeys.GetKeyring(); err != nil {
		return nil, fmt.Errorf("failed to validate keyring: %w", err)
	}

	clientCtx := createClientContext(validationResult.Keyring, chainID)

	log.Info().
		Str("key_name", validationResult.KeyName).
		Str("key_address", validationResult.KeyAddr).
		Str("granter", validationResult.Granter).
		Msg("Signer initialized successfully")

	return &Signer{
		keys:      universalKeys,
		clientCtx: clientCtx,
		pushCore:  pushCore,
		granter:   validationResult.Granter,
		log:       log.With().Str("component", "signer").Logger(),
	}, nil
}

// VoteInbound votes on an inbound transaction.
func (s *Signer) VoteInbound(ctx context.Context, inbound *uexecutortypes.Inbound) (string, error) {
	return voteInbound(ctx, s, s.log, s.granter, inbound)
}

// VoteChainMeta votes on chain metadata (gas price, block height).
func (s *Signer) VoteChainMeta(ctx context.Context, chainID string, price uint64, chainHeight uint64) (string, error) {
	return voteChainMeta(ctx, s, s.log, s.granter, chainID, price, chainHeight)
}

// VoteOutbound votes on an outbound transaction observation.
func (s *Signer) VoteOutbound(ctx context.Context, txID string, utxID string, observation *uexecutortypes.OutboundObservation) (string, error) {
	return voteOutbound(ctx, s, s.log, s.granter, txID, utxID, observation)
}

// VoteTssKeyProcess votes on a TSS key process.
func (s *Signer) VoteTssKeyProcess(ctx context.Context, tssPubKey string, keyID string, processID uint64) (string, error) {
	return voteTssKeyProcess(ctx, s, s.log, s.granter, tssPubKey, keyID, processID)
}

// VoteFundMigration votes on a fund migration result.
func (s *Signer) VoteFundMigration(ctx context.Context, migrationID uint64, txHash string, success bool) (string, error) {
	return voteFundMigration(ctx, s, s.log, s.granter, migrationID, txHash, success)
}

// signAndBroadcastAuthZTx signs and broadcasts an AuthZ transaction
func (s *Signer) signAndBroadcastAuthZTx(
	ctx context.Context,
	msgs []sdk.Msg,
	memo string,
	gasLimit uint64,
	feeAmount sdk.Coins,
) (*sdk.TxResponse, error) {
	// Lock to prevent concurrent sequence issues
	s.sequenceMutex.Lock()
	defer s.sequenceMutex.Unlock()

	s.log.Debug().
		Int("msg_count", len(msgs)).
		Str("memo", memo).
		Msg("Creating AuthZ transaction")

	// Wrap messages with AuthZ
	authzMsgs, err := s.wrapWithAuthZ(msgs)
	if err != nil {
		return nil, fmt.Errorf("failed to wrap messages with AuthZ: %w", err)
	}

	// Try up to 3 times in case of sequence mismatch
	maxAttempts := 3
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		// Create and sign transaction
		txBuilder, err := s.createTxBuilder(authzMsgs, memo, gasLimit, feeAmount)
		if err != nil {
			return nil, fmt.Errorf("failed to create tx builder: %w", err)
		}

		// Sign the transaction with sequence management using keyring (no private key exposure)
		if err := s.signTxWithSequence(ctx, txBuilder); err != nil {
			return nil, fmt.Errorf("failed to sign transaction: %w", err)
		}

		// Encode transaction
		txBytes, err := s.clientCtx.TxConfig.TxEncoder()(txBuilder.GetTx())
		if err != nil {
			return nil, fmt.Errorf("failed to encode transaction: %w", err)
		}

		// Broadcast transaction using pushcore
		broadcastResp, err := s.pushCore.BroadcastTx(ctx, txBytes)
		if err != nil {
			// Check if error is due to sequence mismatch
			if strings.Contains(err.Error(), "account sequence mismatch") && attempt < maxAttempts {
				s.log.Warn().
					Err(err).
					Uint64("current_sequence", s.lastSequence).
					Int("attempt", attempt).
					Msg("Sequence mismatch detected, forcing refresh and retrying")
				s.lastSequence = 0
				continue
			}
			// Network/transport errors: sequence was NOT consumed, don't increment.
			// Force refresh from chain on next use to reconcile.
			s.lastSequence = 0
			return nil, fmt.Errorf("failed to broadcast transaction: %w", err)
		}

		// Convert tx.BroadcastTxResponse to sdk.TxResponse
		var txResp *sdk.TxResponse
		if broadcastResp != nil && broadcastResp.TxResponse != nil {
			txResp = broadcastResp.TxResponse
		}

		// If chain responded with error code, handle sequence-mismatch specially
		if txResp != nil && txResp.Code != 0 {
			if strings.Contains(strings.ToLower(txResp.RawLog), "account sequence mismatch") && attempt < maxAttempts {
				s.log.Warn().
					Uint64("current_sequence", s.lastSequence).
					Int("attempt", attempt).
					Str("raw_log", txResp.RawLog).
					Msg("Sequence mismatch in response, refreshing and retrying")
				s.lastSequence = 0
				continue
			}

			// Chain accepted the tx into mempool but it failed — sequence was consumed
			s.lastSequence++

			s.log.Error().
				Str("tx_hash", txResp.TxHash).
				Uint32("code", txResp.Code).
				Str("raw_log", txResp.RawLog).
				Uint64("sequence_used", s.lastSequence-1).
				Msg("Transaction failed on chain")
			return txResp, fmt.Errorf("transaction failed with code %d: %s", txResp.Code, txResp.RawLog)
		}

		// Success: sequence was consumed
		s.lastSequence++
		s.log.Debug().
			Str("tx_hash", txResp.TxHash).
			Int64("gas_used", txResp.GasUsed).
			Uint64("sequence_used", s.lastSequence-1).
			Msg("Transaction broadcasted successfully")

		return txResp, nil
	}

	return nil, fmt.Errorf("failed to broadcast transaction after %d attempts", maxAttempts)
}

// wrapWithAuthZ wraps messages with AuthZ MsgExec
func (s *Signer) wrapWithAuthZ(msgs []sdk.Msg) ([]sdk.Msg, error) {
	if len(msgs) == 0 {
		return nil, fmt.Errorf("no messages to wrap")
	}

	// Get hot key address for grantee
	hotKeyAddr, err := s.keys.GetAddress()
	if err != nil {
		return nil, fmt.Errorf("failed to get hot key address: %w", err)
	}

	s.log.Debug().
		Str("grantee", hotKeyAddr.String()).
		Int("msg_count", len(msgs)).
		Msg("Wrapping messages with AuthZ")

	// Create MsgExec
	msgExec := cosmosauthz.NewMsgExec(hotKeyAddr, msgs)

	return []sdk.Msg{&msgExec}, nil
}

// createTxBuilder creates a transaction builder with the given parameters
func (s *Signer) createTxBuilder(
	msgs []sdk.Msg,
	memo string,
	gasLimit uint64,
	feeAmount sdk.Coins,
) (client.TxBuilder, error) {
	txBuilder := s.clientCtx.TxConfig.NewTxBuilder()

	// Set messages
	if err := txBuilder.SetMsgs(msgs...); err != nil {
		return nil, fmt.Errorf("failed to set messages: %w", err)
	}

	// Set memo
	txBuilder.SetMemo(memo)

	// Set gas limit
	txBuilder.SetGasLimit(gasLimit)

	// Set fee amount
	txBuilder.SetFeeAmount(feeAmount)

	return txBuilder, nil
}

// signTxWithSequence signs a transaction with proper sequence management using keyring
// This method does NOT expose the private key - it uses the keyring directly
func (s *Signer) signTxWithSequence(ctx context.Context, txBuilder client.TxBuilder) error {
	s.log.Debug().Msg("Starting transaction signing with sequence management")

	// Get account info to refresh sequence if needed
	account, err := s.getAccountInfo(ctx)
	if err != nil {
		return fmt.Errorf("failed to get account info: %w", err)
	}

	// Reconcile local vs chain sequence conservatively:
	// - If we have no local sequence (0), adopt chain's sequence.
	// - If local < chain, adopt chain (we are behind).
	// - If local > chain, keep local (likely recent tx not yet reflected in query).
	chainSequence := account.GetSequence()
	if s.lastSequence == 0 {
		s.lastSequence = chainSequence
		s.log.Debug().
			Uint64("adopted_chain_sequence", chainSequence).
			Msg("Initialized local sequence from chain")
	} else if s.lastSequence < chainSequence {
		s.log.Debug().
			Uint64("chain_sequence", chainSequence).
			Uint64("cached_sequence", s.lastSequence).
			Msg("Local sequence behind chain, adopting chain's sequence")
		s.lastSequence = chainSequence
	} else if s.lastSequence > chainSequence {
		s.log.Warn().
			Uint64("chain_sequence", chainSequence).
			Uint64("cached_sequence", s.lastSequence).
			Msg("Local sequence ahead of chain query, keeping local to avoid reuse")
	}

	// Get hot key address
	hotKeyAddr, err := s.keys.GetAddress()
	if err != nil {
		return fmt.Errorf("failed to get hot key address: %w", err)
	}

	keyName := s.keys.GetKeyName()

	s.log.Debug().
		Str("signer", hotKeyAddr.String()).
		Str("key_name", keyName).
		Uint64("account_number", account.GetAccountNumber()).
		Uint64("sequence", s.lastSequence).
		Msg("Signing transaction with managed sequence using keyring")

	// Get keyring and validate key exists
	kr, err := s.keys.GetKeyring()
	if err != nil {
		return fmt.Errorf("failed to get keyring: %w", err)
	}

	// Use SDK's tx.Sign method which uses the keyring directly (no private key exposure)
	// The keyring handles decryption automatically for file backend when signing
	// Create a tx factory from the client context
	txFactory := tx.Factory{}.
		WithChainID(s.clientCtx.ChainID).
		WithKeybase(kr).
		WithTxConfig(s.clientCtx.TxConfig).
		WithAccountNumber(account.GetAccountNumber()).
		WithSequence(s.lastSequence)

	err = tx.Sign(
		ctx,
		txFactory,
		keyName,
		txBuilder,
		false, // overwriteSig
	)
	if err != nil {
		return fmt.Errorf("failed to sign transaction with keyring: %w", err)
	}

	s.log.Debug().
		Str("signer", hotKeyAddr.String()).
		Uint64("sequence", s.lastSequence).
		Msg("Transaction signed successfully with managed sequence")

	return nil
}

// getAccountInfo retrieves account information for the hot key using pushcore
func (s *Signer) getAccountInfo(ctx context.Context) (client.Account, error) {
	hotKeyAddr, err := s.keys.GetAddress()
	if err != nil {
		return nil, fmt.Errorf("failed to get hot key address: %w", err)
	}

	s.log.Debug().
		Str("address", hotKeyAddr.String()).
		Msg("Querying account info from chain")

	// Query account information using pushcore
	accountResp, err := s.pushCore.GetAccount(ctx, hotKeyAddr.String())
	if err != nil {
		return nil, fmt.Errorf("failed to query account info: %w", err)
	}

	// Unpack account using interface registry from client context
	var account sdk.AccountI
	if err := s.clientCtx.InterfaceRegistry.UnpackAny(accountResp.Account, &account); err != nil {
		return nil, fmt.Errorf("failed to unpack account: %w", err)
	}

	s.log.Debug().
		Str("address", account.GetAddress().String()).
		Uint64("account_number", account.GetAccountNumber()).
		Uint64("sequence", account.GetSequence()).
		Msg("Retrieved account info")

	return account, nil
}

func createClientContext(kr cosmoskeyring.Keyring, chainID string) client.Context {
	interfaceRegistry := keys.NewInterfaceRegistryWithEVMSupport()
	cosmosauthz.RegisterInterfaces(interfaceRegistry)
	authtypes.RegisterInterfaces(interfaceRegistry)
	banktypes.RegisterInterfaces(interfaceRegistry)
	stakingtypes.RegisterInterfaces(interfaceRegistry)
	govtypes.RegisterInterfaces(interfaceRegistry)
	uexecutortypes.RegisterInterfaces(interfaceRegistry)

	cdc := codec.NewProtoCodec(interfaceRegistry)
	txConfig := authtx.NewTxConfig(cdc, []signing.SignMode{signing.SignMode_SIGN_MODE_DIRECT})

	return client.Context{}.
		WithCodec(cdc).
		WithInterfaceRegistry(interfaceRegistry).
		WithChainID(chainID).
		WithKeyring(kr).
		WithTxConfig(txConfig)
}
