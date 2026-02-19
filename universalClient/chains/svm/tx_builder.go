// Package svm implements the Solana (SVM) transaction builder for Push Chain's
// cross-chain outbound transaction system.
//
// # How Cross-Chain Outbound Works (High-Level)
//
// When a user on Push Chain wants to send funds/execute something on Solana:
//
//  1. Push Chain emits an OutboundCreatedEvent with details (amount, recipient, etc.)
//  2. A coordinator node picks up the event
//  3. This TxBuilder constructs the message that needs to be signed (GetOutboundSigningRequest)
//  4. Push Chain validators collectively sign the message using TSS (Threshold Signature Scheme)
//     - TSS uses secp256k1 (same curve as Ethereum) — the TSS group has an ETH-style address
//  5. This TxBuilder assembles the full Solana transaction with the TSS signature and broadcasts it
//     (BroadcastOutboundSigningRequest)
//  6. The Solana gateway contract verifies the TSS signature on-chain using secp256k1_recover
//
// # Two-Signature Architecture
//
// Every Solana transaction requires TWO different signatures:
//
//   - TSS Signature (secp256k1/ECDSA): Signs the message hash. Verified by the gateway contract
//     on-chain via secp256k1_recover. This proves the Push Chain validators approved the operation.
//     The TSS group's ETH address is stored in the TSS PDA on Solana.
//
//   - Relayer Signature (Ed25519): Signs the Solana transaction itself. This is a standard
//     Solana transaction signature from the relayer's keypair. The relayer pays for gas (SOL).
//
// # Gateway Contract (Anchor/Rust on Solana)
//
// The gateway is an Anchor program deployed on Solana with these main entry points:
//
//   - withdraw_and_execute (instruction_id=1 for withdraw, 2 for execute):
//     Unified function that handles both simple fund transfers and arbitrary program execution.
//     For withdraw: transfers SOL/SPL from the vault to a recipient.
//     For execute: calls an arbitrary Solana program via CPI with provided accounts and data.
//
//   - revert_universal_tx (instruction_id=3): Reverts a failed cross-chain tx, returns native SOL.
//
//   - revert_universal_tx_token (instruction_id=4): Same but for SPL tokens.
//
// # Key Concepts
//
//   - PDA (Program Derived Address): Deterministic addresses derived from seeds + program ID.
//     Like CREATE2 in EVM. The gateway uses PDAs for config, vault, TSS state, etc.
//
//   - Anchor Discriminator: First 8 bytes of sha256("global:<method_name>"). Tells the
//     Anchor framework which function to call. Similar to EVM function selectors (4 bytes of keccak256).
//
//   - Borsh Serialization: Solana's standard binary format. Little-endian integers,
//     Vec<T> = 4-byte LE length prefix + elements. Used for instruction data.
//
//   - TSS PDA: Stores the TSS group's 20-byte ETH address, chain ID, and a nonce for replay protection.
//
//   - CEA (Cross-chain Execution Account): Per-sender identity PDA derived from the EVM sender address.
//
//   - ATA (Associated Token Account): Deterministic token account for a wallet + mint pair.
//     Like mapping(address => mapping(token => balance)) in EVM, but accounts are explicit on Solana.
package svm

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/gagliardetto/solana-go"
	"github.com/rs/zerolog"

	"github.com/pushchain/push-chain-node/universalClient/chains/common"
	"github.com/pushchain/push-chain-node/universalClient/constant"
	uetypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// DefaultComputeUnitLimit is the fallback compute budget when the event doesn't specify a gas limit.
// Solana charges based on "compute units" (similar to EVM gas). 200k is a safe default for most
// gateway operations. The actual cost depends on how many accounts are touched, CPI depth, etc.
const DefaultComputeUnitLimit = 200000

// GatewayAccountMeta represents a single account that a target program needs when executing
// an arbitrary cross-chain call (instruction_id=2). The payload from Push Chain includes a list
// of these — each with the account's public key and whether it needs write access.
// This mirrors the Rust struct in the gateway contract (state.rs).
type GatewayAccountMeta struct {
	Pubkey     [32]byte // Solana public key (32 bytes, not base58-encoded)
	IsWritable bool     // Whether the target program needs to write to this account
}

// TxBuilder constructs and broadcasts Solana transactions for cross-chain operations.
// It implements the common.OutboundTxBuilder interface shared with the EVM tx builder.
//
// The builder needs:
//   - rpcClient: to talk to a Solana RPC node (fetch account data, send transactions)
//   - chainID: identifies the Solana cluster (e.g., "solana:EtWTRABZ..." for devnet)
//   - gatewayAddress: the deployed gateway program's public key on Solana
//   - nodeHome: filesystem path where the relayer's Solana keypair is stored
type TxBuilder struct {
	rpcClient      *RPCClient
	chainID        string
	gatewayAddress solana.PublicKey
	nodeHome       string
	logger         zerolog.Logger
}

// NewTxBuilder creates a new Solana transaction builder.
// gatewayAddress must be a valid base58-encoded Solana public key pointing to the
// deployed gateway program.
func NewTxBuilder(
	rpcClient *RPCClient,
	chainID string,
	gatewayAddress string,
	nodeHome string,
	logger zerolog.Logger,
) (*TxBuilder, error) {
	if rpcClient == nil {
		return nil, fmt.Errorf("rpcClient is required")
	}
	if chainID == "" {
		return nil, fmt.Errorf("chainID is required")
	}
	if gatewayAddress == "" {
		return nil, fmt.Errorf("gatewayAddress is required")
	}

	addr, err := solana.PublicKeyFromBase58(gatewayAddress)
	if err != nil {
		return nil, fmt.Errorf("invalid gateway address: %w", err)
	}

	return &TxBuilder{
		rpcClient:      rpcClient,
		chainID:        chainID,
		gatewayAddress: addr,
		nodeHome:       nodeHome,
		logger:         logger.With().Str("component", "svm_tx_builder").Str("chain", chainID).Logger(),
	}, nil
}

// =============================================================================
//  STEP 1: GetOutboundSigningRequest
//
//  Called when Push Chain detects an outbound event targeting Solana.
//  This method:
//    1. Fetches the current TSS nonce from the on-chain TSS PDA (replay protection)
//    2. Determines the instruction type (withdraw/execute/revert)
//    3. Constructs the exact message that TSS validators will sign
//    4. Returns the 32-byte keccak256 hash for TSS signing
//
//  After this, the coordinator broadcasts the hash to TSS nodes, they collectively
//  sign it, and the 64-byte signature (r||s) is passed to BroadcastOutboundSigningRequest.
// =============================================================================

// GetOutboundSigningRequest creates a signing request from an outbound event.
// Returns a 32-byte keccak256 hash that the TSS nodes need to sign.
func (tb *TxBuilder) GetOutboundSigningRequest(
	ctx context.Context,
	data *uetypes.OutboundCreatedEvent,
	gasPrice *big.Int,
	nonce uint64,
) (*common.UnSignedOutboundTxReq, error) {
	if data == nil {
		return nil, fmt.Errorf("outbound event data is nil")
	}
	if data.TxID == "" {
		return nil, fmt.Errorf("txID is required")
	}
	if data.DestinationChain == "" {
		return nil, fmt.Errorf("destinationChain is required")
	}
	if gasPrice == nil {
		return nil, fmt.Errorf("gasPrice is required")
	}

	// --- Parse all fields from the outbound event ---

	amount := new(big.Int)
	amount, ok := amount.SetString(data.Amount, 10)
	if !ok {
		return nil, fmt.Errorf("invalid amount: %s", data.Amount)
	}

	// Validate amount fits in u64 (Solana uses u64 for amounts, events use uint256)
	if !amount.IsUint64() {
		return nil, fmt.Errorf("amount exceeds u64 max: %s", data.Amount)
	}

	// Determine if this is native SOL or an SPL token transfer.
	// Empty or zero address = native SOL. Otherwise it's the SPL token mint address.
	assetAddr := data.AssetAddr
	isNative := assetAddr == "" || assetAddr == "0x0" || assetAddr == "0x0000000000000000000000000000000000000000"

	txType, err := parseTxType(data.TxType)
	if err != nil {
		return nil, fmt.Errorf("invalid tx type: %w", err)
	}

	// --- Fetch on-chain state from the TSS PDA ---
	// The TSS PDA stores: ETH address of the TSS group, chain ID, and a nonce.
	// The nonce increments with each operation to prevent replay attacks.
	tssPDA, err := tb.deriveTSSPDA()
	if err != nil {
		return nil, fmt.Errorf("failed to derive TSS PDA: %w", err)
	}
	_, chainID, err := tb.fetchTSSNonce(ctx, tssPDA)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch chain ID from TSS PDA: %w", err)
	}

	// --- Parse identifiers from hex strings to fixed-size byte arrays ---

	// txID: unique identifier for this cross-chain transaction (32 bytes).
	// Must be deterministic and stable across retries (same tx_id for all retry attempts).
	var txID [32]byte
	txIDBytes, err := hex.DecodeString(removeHexPrefix(data.TxID))
	if err != nil {
		return nil, fmt.Errorf("invalid txID: %s", data.TxID)
	}
	if len(txIDBytes) == 32 {
		copy(txID[:], txIDBytes)
	} else if len(txIDBytes) > 0 {
		// Right-align shorter IDs (pad with leading zeros)
		copy(txID[32-len(txIDBytes):], txIDBytes)
	}

	// universalTxID: the original transaction ID from the source chain (32 bytes)
	var universalTxID [32]byte
	utxIDBytes, err := hex.DecodeString(removeHexPrefix(data.UniversalTxId))
	if err != nil {
		return nil, fmt.Errorf("invalid universalTxID: %s", data.UniversalTxId)
	}
	if len(utxIDBytes) == 32 {
		copy(universalTxID[:], utxIDBytes)
	} else if len(utxIDBytes) > 0 {
		copy(universalTxID[32-len(utxIDBytes):], utxIDBytes)
	}

	// sender: the 20-byte EVM address of the original sender on the source chain
	var sender [20]byte
	senderBytes, err := hex.DecodeString(removeHexPrefix(data.Sender))
	if err != nil {
		return nil, fmt.Errorf("invalid sender: %s", data.Sender)
	}
	if len(senderBytes) == 20 {
		copy(sender[:], senderBytes)
	} else {
		return nil, fmt.Errorf("invalid sender length: expected 20 bytes, got %d", len(senderBytes))
	}

	// token: 32-byte Solana pubkey of the SPL token mint. All zeros = native SOL (Pubkey::default())
	var token [32]byte
	if !isNative {
		mintPubkey, parseErr := solana.PublicKeyFromBase58(assetAddr)
		if parseErr != nil {
			hexBytes, hexErr := hex.DecodeString(removeHexPrefix(assetAddr))
			if hexErr != nil || len(hexBytes) != 32 {
				return nil, fmt.Errorf("invalid asset address format: %s", assetAddr)
			}
			mintPubkey = solana.PublicKeyFromBytes(hexBytes)
		}
		copy(token[:], mintPubkey.Bytes())
	}
	// For native SOL, token stays all-zeros (Pubkey::default() in Rust)

	// Gas fee from event. TODO: OutboundCreatedEvent doesn't include GasFee field yet.
	// When the event pipeline threads gas_fee through, parse it here.
	var gasFee uint64

	// recipient/target: Solana pubkey of the destination. Used differently depending on instruction:
	//   - Withdraw (id=1): the wallet that receives the funds (target = recipient)
	//   - Execute (id=2): the target program to CPI into (target = destination_program)
	//   - Revert (id=3,4): the wallet that gets the refund
	var recipientPubkey solana.PublicKey
	recipientPubkey, err = solana.PublicKeyFromBase58(data.Recipient)
	if err != nil {
		hexBytes, hexErr := hex.DecodeString(removeHexPrefix(data.Recipient))
		if hexErr != nil || len(hexBytes) != 32 {
			return nil, fmt.Errorf("invalid recipient address format (expected Solana Pubkey): %s", data.Recipient)
		}
		recipientPubkey = solana.PublicKeyFromBytes(hexBytes)
	}

	// --- Determine instruction ID and decode payload ---
	// For non-revert flows: decode payload to get instruction_id (1=withdraw, 2=execute),
	// accounts, ixData, and rentFee. The instruction_id in the payload is authoritative.
	// For revert flows: instruction_id is determined from TxType + asset type.
	var instructionID uint8
	var targetProgram [32]byte
	var accounts []GatewayAccountMeta
	var ixData []byte
	var rentFee uint64
	var revertRecipient [32]byte
	var revertMint [32]byte

	if txType == uetypes.TxType_INBOUND_REVERT {
		// Revert flows: instruction_id from TxType (no payload-based instruction_id)
		if isNative {
			instructionID = 3
		} else {
			instructionID = 4
		}

		switch instructionID {
		case 3: // Revert SOL: recipient gets their SOL back
			copy(revertRecipient[:], recipientPubkey.Bytes())
		case 4: // Revert SPL: recipient gets their SPL tokens back
			copy(revertRecipient[:], recipientPubkey.Bytes())
			copy(revertMint[:], token[:])
		}
	} else {
		// Non-revert flows: decode payload to get instruction_id.
		// Payload format: [accounts][ixData][rentFee][instruction_id]
		// For simple withdraw, payload may be empty — fall back to TxType.
		payloadHex := removeHexPrefix(data.Payload)
		if payloadHex != "" {
			payloadBytes, decErr := hex.DecodeString(payloadHex)
			if decErr != nil {
				return nil, fmt.Errorf("failed to decode payload hex: %w", decErr)
			}

			if len(payloadBytes) > 0 {
				var payloadInstructionID uint8
				accounts, ixData, rentFee, payloadInstructionID, err = decodePayload(payloadBytes)
				if err != nil {
					return nil, fmt.Errorf("failed to decode payload: %w", err)
				}
				instructionID = payloadInstructionID
			}
		}

		// If payload was empty/missing, fall back to TxType-derived instruction_id
		if instructionID == 0 {
			fallbackID, fbErr := tb.determineInstructionID(txType, isNative)
			if fbErr != nil {
				return nil, fmt.Errorf("failed to determine instruction ID: %w", fbErr)
			}
			instructionID = fallbackID
		}

		// Validate instruction_id
		if instructionID != 1 && instructionID != 2 {
			return nil, fmt.Errorf("invalid instruction_id: %d (expected 1=withdraw or 2=execute)", instructionID)
		}

		// Validate rent_fee <= gas_fee (contract requires this)
		if rentFee > gasFee {
			return nil, fmt.Errorf("rent_fee (%d) exceeds gas_fee (%d)", rentFee, gasFee)
		}

		// Validate mode-specific constraints per integration guide
		switch instructionID {
		case 1: // Withdraw mode
			if len(accounts) > 0 || len(ixData) > 0 || rentFee > 0 {
				return nil, fmt.Errorf("withdraw mode: accounts, ixData, and rentFee must be empty/zero")
			}
			if amount.Uint64() == 0 {
				return nil, fmt.Errorf("withdraw mode: amount must be > 0")
			}
			copy(targetProgram[:], recipientPubkey.Bytes())

		case 2: // Execute mode
			copy(targetProgram[:], recipientPubkey.Bytes())
		}
	}

	// --- Construct the TSS message and hash it ---
	// This message is what TSS validators sign. The gateway contract reconstructs
	// the same message on-chain and verifies the signature matches.
	messageHash, err := tb.constructTSSMessage(
		instructionID, chainID, nonce, amount.Uint64(),
		txID, universalTxID, sender, token, gasFee,
		targetProgram, accounts, ixData, rentFee,
		revertRecipient, revertMint,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to construct TSS message: %w", err)
	}

	prioritizationFee := gasPrice.Uint64()

	return &common.UnSignedOutboundTxReq{
		SigningHash: messageHash, // This is the keccak256 hash to be signed by TSS
		Nonce:       nonce,
		GasPrice:    big.NewInt(int64(prioritizationFee)),
	}, nil
}

// GetNextNonce returns the current TSS PDA nonce for this chain. useFinalized is ignored for SVM.
func (tb *TxBuilder) GetNextNonce(ctx context.Context, signerAddress string, useFinalized bool) (uint64, error) {
	_ = signerAddress // SVM uses PDA, not signer address
	tssPDA, err := tb.deriveTSSPDA()
	if err != nil {
		return 0, fmt.Errorf("failed to derive TSS PDA: %w", err)
	}
	nonce, _, err := tb.fetchTSSNonce(ctx, tssPDA)
	return nonce, err
}

// =============================================================================
//  STEP 2: BroadcastOutboundSigningRequest
//
//  Called after TSS nodes have collectively signed the message hash.
//  This method:
//    1. Re-parses all parameters from the event (same as GetOutboundSigningRequest)
//    2. Derives all PDAs needed for the gateway instruction
//    3. Determines the recovery ID for the ECDSA signature (v value, 0-3)
//    4. Builds the Borsh-serialized instruction data
//    5. Builds the ordered accounts list matching the Rust struct layout
//    6. Creates a Solana transaction with compute budget + gateway instruction
//    7. Signs with the relayer's Ed25519 key and broadcasts to the network
//
//  The signature parameter is the 64-byte TSS signature (r||s, no v byte).
//  The recovery ID is determined by trying all 4 possible values and checking
//  which one recovers to the TSS ETH address stored on-chain.
// =============================================================================

// BroadcastOutboundSigningRequest assembles a complete Solana transaction with the
// TSS signature and broadcasts it to the Solana network.
func (tb *TxBuilder) BroadcastOutboundSigningRequest(
	ctx context.Context,
	req *common.UnSignedOutboundTxReq,
	data *uetypes.OutboundCreatedEvent,
	signature []byte,
) (string, error) {
	tx, instructionID, err := tb.BuildOutboundTransaction(ctx, req, data, signature)
	if err != nil {
		return "", err
	}

	txHash, err := tb.rpcClient.BroadcastTransaction(ctx, tx)
	if err != nil {
		return "", fmt.Errorf("failed to broadcast transaction: %w", err)
	}

	tb.logger.Info().
		Str("tx_hash", txHash).
		Uint8("instruction_id", instructionID).
		Msg("transaction broadcast successfully")

	return txHash, nil
}

// BuildOutboundTransaction assembles a complete signed Solana transaction from the
// TSS signature and event data, without broadcasting. Returns the transaction and
// the instruction ID for logging. Use this for simulation or inspection.
func (tb *TxBuilder) BuildOutboundTransaction(
	ctx context.Context,
	req *common.UnSignedOutboundTxReq,
	data *uetypes.OutboundCreatedEvent,
	signature []byte,
) (*solana.Transaction, uint8, error) {
	if req == nil {
		return nil, 0, fmt.Errorf("signing request is nil")
	}
	if data == nil {
		return nil, 0, fmt.Errorf("outbound event data is nil")
	}
	if len(signature) != 64 {
		return nil, 0, fmt.Errorf("signature must be 64 bytes, got %d", len(signature))
	}

	// Load the relayer's Solana keypair from disk.
	// The relayer is the entity that pays for Solana transaction fees (gas).
	// Its Ed25519 signature authorizes the Solana transaction itself.
	// (This is separate from the TSS secp256k1 signature that authorizes the cross-chain operation.)
	relayerKeypair, err := tb.loadRelayerKeypair()
	if err != nil {
		return nil, 0, fmt.Errorf("failed to load relayer keypair: %w", err)
	}

	// --- Re-parse event data (same parsing as GetOutboundSigningRequest) ---

	amount := new(big.Int)
	amount, ok := amount.SetString(data.Amount, 10)
	if !ok {
		return nil, 0, fmt.Errorf("invalid amount: %s", data.Amount)
	}
	if !amount.IsUint64() {
		return nil, 0, fmt.Errorf("amount exceeds u64 max: %s", data.Amount)
	}

	assetAddr := data.AssetAddr
	isNative := assetAddr == "" || assetAddr == "0x0" || assetAddr == "0x0000000000000000000000000000000000000000"

	txType, err := parseTxType(data.TxType)
	if err != nil {
		return nil, 0, fmt.Errorf("invalid tx type: %w", err)
	}

	var txID [32]byte
	txIDBytes, err := hex.DecodeString(removeHexPrefix(data.TxID))
	if err != nil {
		return nil, 0, fmt.Errorf("invalid txID: %s", data.TxID)
	}
	if len(txIDBytes) == 32 {
		copy(txID[:], txIDBytes)
	} else if len(txIDBytes) > 0 {
		copy(txID[32-len(txIDBytes):], txIDBytes)
	}

	var universalTxID [32]byte
	utxIDBytes, err := hex.DecodeString(removeHexPrefix(data.UniversalTxId))
	if err != nil {
		return nil, 0, fmt.Errorf("invalid universalTxID: %s", data.UniversalTxId)
	}
	if len(utxIDBytes) == 32 {
		copy(universalTxID[:], utxIDBytes)
	} else if len(utxIDBytes) > 0 {
		copy(universalTxID[32-len(utxIDBytes):], utxIDBytes)
	}

	var sender [20]byte
	senderBytes, err := hex.DecodeString(removeHexPrefix(data.Sender))
	if err != nil {
		return nil, 0, fmt.Errorf("invalid sender: %s", data.Sender)
	}
	if len(senderBytes) == 20 {
		copy(sender[:], senderBytes)
	} else {
		return nil, 0, fmt.Errorf("invalid sender length: expected 20 bytes, got %d", len(senderBytes))
	}

	var token [32]byte
	var mintPubkey solana.PublicKey
	if !isNative {
		mintPubkey, err = solana.PublicKeyFromBase58(assetAddr)
		if err != nil {
			hexBytes, hexErr := hex.DecodeString(removeHexPrefix(assetAddr))
			if hexErr != nil || len(hexBytes) != 32 {
				return nil, 0, fmt.Errorf("invalid asset address format: %s", assetAddr)
			}
			mintPubkey = solana.PublicKeyFromBytes(hexBytes)
		}
		copy(token[:], mintPubkey.Bytes())
	}

	// Gas fee from event. TODO: OutboundCreatedEvent doesn't include GasFee field yet.
	var gasFee uint64

	recipientPubkey, err := solana.PublicKeyFromBase58(data.Recipient)
	if err != nil {
		hexBytes, hexErr := hex.DecodeString(removeHexPrefix(data.Recipient))
		if hexErr != nil || len(hexBytes) != 32 {
			return nil, 0, fmt.Errorf("invalid recipient address format: %s", data.Recipient)
		}
		recipientPubkey = solana.PublicKeyFromBytes(hexBytes)
	}

	revertMsgBytes, err := hex.DecodeString(removeHexPrefix(data.RevertMsg))
	if err != nil {
		revertMsgBytes = []byte{}
	}

	// --- Determine instruction ID and decode payload ---
	var instructionID uint8
	var execAccounts []GatewayAccountMeta
	var ixData []byte
	var rentFee uint64

	if txType == uetypes.TxType_INBOUND_REVERT {
		if isNative {
			instructionID = 3
		} else {
			instructionID = 4
		}
	} else {
		// Non-revert: decode payload to get instruction_id
		payloadHex := removeHexPrefix(data.Payload)
		if payloadHex != "" {
			payloadBytes, decErr := hex.DecodeString(payloadHex)
			if decErr != nil {
				return nil, 0, fmt.Errorf("failed to decode payload hex: %w", decErr)
			}
			if len(payloadBytes) > 0 {
				execAccounts, ixData, rentFee, instructionID, err = decodePayload(payloadBytes)
				if err != nil {
					return nil, 0, fmt.Errorf("failed to decode payload: %w", err)
				}
			}
		}

		// Fall back to TxType if payload was empty
		if instructionID == 0 {
			fallbackID, fbErr := tb.determineInstructionID(txType, isNative)
			if fbErr != nil {
				return nil, 0, fmt.Errorf("failed to determine instruction ID: %w", fbErr)
			}
			instructionID = fallbackID
		}

		if instructionID != 1 && instructionID != 2 {
			return nil, 0, fmt.Errorf("invalid instruction_id: %d", instructionID)
		}
		if rentFee > gasFee {
			return nil, 0, fmt.Errorf("rent_fee (%d) exceeds gas_fee (%d)", rentFee, gasFee)
		}
	}

	// --- Derive PDAs ---
	configPDA, _, err := solana.FindProgramAddress([][]byte{[]byte("config")}, tb.gatewayAddress)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to derive config PDA: %w", err)
	}

	vaultPDA, _, err := solana.FindProgramAddress([][]byte{[]byte("vault")}, tb.gatewayAddress)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to derive vault PDA: %w", err)
	}

	tssPDA, _, err := solana.FindProgramAddress([][]byte{[]byte("tsspda")}, tb.gatewayAddress)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to derive TSS PDA: %w", err)
	}

	executedTxPDA, _, err := solana.FindProgramAddress([][]byte{[]byte("executed_tx"), txID[:]}, tb.gatewayAddress)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to derive executed_tx PDA: %w", err)
	}

	// --- Determine recovery ID ---
	tssAccountData, err := tb.rpcClient.GetAccountData(ctx, tssPDA)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to fetch TSS PDA account for recovery ID: %w", err)
	}
	if len(tssAccountData) < 28 {
		return nil, 0, fmt.Errorf("invalid TSS PDA account data for recovery ID")
	}
	tssEthAddress := tssAccountData[8:28]

	recoveryID, err := tb.determineRecoveryID(req.SigningHash, signature, hex.EncodeToString(tssEthAddress))
	if err != nil {
		return nil, 0, fmt.Errorf("failed to determine recovery ID: %w", err)
	}

	// --- Build instruction data and accounts list ---
	var instructionData []byte
	var accounts []*solana.AccountMeta

	switch {
	case instructionID == 1 || instructionID == 2:
		// ---- withdraw_and_execute (unified function) ----
		var targetProgram solana.PublicKey
		var writableFlags []byte

		if instructionID == 2 {
			// Execute mode: writable flags from decoded accounts
			writableFlags = accountsToWritableFlags(execAccounts)
			targetProgram = recipientPubkey
		} else {
			// Withdraw mode: empty flags, system_program as sentinel
			writableFlags = []byte{}
			ixData = []byte{}
			targetProgram = solana.SystemProgramID
		}

		ceaAuthorityPDA, _, ceaErr := solana.FindProgramAddress([][]byte{[]byte("push_identity"), sender[:]}, tb.gatewayAddress)
		if ceaErr != nil {
			return nil, 0, fmt.Errorf("failed to derive cea_authority PDA: %w", ceaErr)
		}

		instructionData = tb.buildWithdrawAndExecuteData(
			instructionID, txID, universalTxID, amount.Uint64(), sender,
			writableFlags, ixData, gasFee, rentFee,
			signature, recoveryID, req.SigningHash, req.Nonce,
		)

		accounts = tb.buildWithdrawAndExecuteAccounts(
			relayerKeypair.PublicKey(),
			configPDA, vaultPDA, ceaAuthorityPDA, tssPDA, executedTxPDA,
			targetProgram,
			isNative, instructionID,
			recipientPubkey, mintPubkey,
			execAccounts,
		)

	case instructionID == 3:
		// ---- revert_universal_tx (native SOL refund) ----
		instructionData = tb.buildRevertData(
			instructionID, txID, universalTxID, amount.Uint64(),
			recipientPubkey, revertMsgBytes, gasFee,
			signature, recoveryID, req.SigningHash, req.Nonce,
		)
		accounts = tb.buildRevertSOLAccounts(
			configPDA, vaultPDA, tssPDA, recipientPubkey,
			executedTxPDA, relayerKeypair.PublicKey(),
		)

	case instructionID == 4:
		// ---- revert_universal_tx_token (SPL token refund) ----
		ataProgramID := solana.MustPublicKeyFromBase58("ATokenGPvbdGVxr1b2hvZbsiqW5xWH25efTNsLJA8knL")

		tokenVaultATA, _, ataErr := solana.FindProgramAddress(
			[][]byte{vaultPDA.Bytes(), solana.TokenProgramID.Bytes(), mintPubkey.Bytes()},
			ataProgramID,
		)
		if ataErr != nil {
			return nil, 0, fmt.Errorf("failed to derive token vault ATA: %w", ataErr)
		}

		recipientATA, _, ataErr := solana.FindProgramAddress(
			[][]byte{recipientPubkey.Bytes(), solana.TokenProgramID.Bytes(), mintPubkey.Bytes()},
			ataProgramID,
		)
		if ataErr != nil {
			return nil, 0, fmt.Errorf("failed to derive recipient ATA: %w", ataErr)
		}

		instructionData = tb.buildRevertData(
			instructionID, txID, universalTxID, amount.Uint64(),
			recipientPubkey, revertMsgBytes, gasFee,
			signature, recoveryID, req.SigningHash, req.Nonce,
		)
		accounts = tb.buildRevertSPLAccounts(
			configPDA, vaultPDA, tokenVaultATA, tssPDA,
			recipientATA, mintPubkey,
			executedTxPDA, relayerKeypair.PublicKey(),
		)
	}

	// --- Assemble the Solana transaction ---
	// Instructions in order:
	//   1. SetComputeUnitLimit — tells the runtime how many compute units to allocate
	//   2. (SPL only) CreateAssociatedTokenAccount — creates recipient ATA if it doesn't exist
	//   3. The actual gateway instruction (withdraw/execute/revert)

	gatewayInstruction := solana.NewInstruction(
		tb.gatewayAddress,
		accounts,
		instructionData,
	)

	var computeUnitLimit uint32
	if data.GasLimit == "" || data.GasLimit == "0" {
		computeUnitLimit = DefaultComputeUnitLimit
	} else {
		parsedLimit, parseErr := strconv.ParseUint(data.GasLimit, 10, 32)
		if parseErr != nil {
			computeUnitLimit = DefaultComputeUnitLimit
		} else {
			computeUnitLimit = uint32(parsedLimit)
		}
	}

	computeBudgetInstruction := tb.buildSetComputeUnitLimitInstruction(computeUnitLimit)

	// Build the instruction list. For SPL flows that need a recipient ATA
	// (withdraw SPL or revert SPL), prepend a CreateIdempotent ATA instruction.
	instructions := []solana.Instruction{computeBudgetInstruction}

	needsRecipientATA := (instructionID == 1 && !isNative) || instructionID == 4
	if needsRecipientATA {
		createATAInstruction := tb.buildCreateATAIdempotentInstruction(
			relayerKeypair.PublicKey(),
			recipientPubkey,
			mintPubkey,
		)
		instructions = append(instructions, createATAInstruction)
	}

	instructions = append(instructions, gatewayInstruction)

	// Get a recent blockhash — Solana uses this instead of nonces for transaction expiry.
	// Transactions expire after ~60-90 seconds if not confirmed.
	recentBlockhash, err := tb.rpcClient.GetRecentBlockhash(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get recent blockhash: %w", err)
	}

	tx, err := solana.NewTransaction(
		instructions,
		recentBlockhash,
		solana.TransactionPayer(relayerKeypair.PublicKey()),
	)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create transaction: %w", err)
	}

	// Sign the transaction with the relayer's Ed25519 key.
	// This is the standard Solana transaction signature (NOT the TSS signature).
	_, err = tx.Sign(func(key solana.PublicKey) *solana.PrivateKey {
		if key.Equals(relayerKeypair.PublicKey()) {
			privKey := relayerKeypair
			return &privKey
		}
		return nil
	})
	if err != nil {
		return nil, 0, fmt.Errorf("failed to sign transaction: %w", err)
	}

	return tx, instructionID, nil
}

// =============================================================================
//  Helper Functions
// =============================================================================

// removeHexPrefix strips the "0x" prefix from hex strings if present.
func removeHexPrefix(s string) string {
	if len(s) >= 2 && s[0:2] == "0x" {
		return s[2:]
	}
	return s
}

// =============================================================================
//  PDA Derivation & On-Chain Data
// =============================================================================

// deriveTSSPDA derives the TSS PDA address.
// The TSS PDA is a singleton account (one per gateway) that stores:
//   - tss_eth_address: the 20-byte Ethereum address of the TSS signing group
//   - chain_id: identifies this Solana cluster (for cross-chain replay protection)
//   - nonce: increments with each operation (per-chain replay protection)
//
// Seed: ["tsspda"] — must match the Rust constant TSS_SEED in state.rs
func (tb *TxBuilder) deriveTSSPDA() (solana.PublicKey, error) {
	seeds := [][]byte{[]byte("tsspda")}
	address, _, err := solana.FindProgramAddress(seeds, tb.gatewayAddress)
	return address, err
}

// fetchTSSNonce reads the TSS PDA account from on-chain and extracts the nonce and chain ID.
//
// On-chain layout (Borsh-serialized TssPda struct from state.rs):
//
//	Offset  Size     Field
//	0       8        Anchor discriminator (account type identifier)
//	8       20       tss_eth_address [u8; 20]
//	28      4        chain_id length (u32, little-endian) — Borsh String prefix
//	32      N        chain_id bytes (UTF-8, variable length)
//	32+N    8        nonce (u64, little-endian)
//	32+N+8  32       authority (Pubkey)
//	...     1        bump
func (tb *TxBuilder) fetchTSSNonce(ctx context.Context, tssPDA solana.PublicKey) (uint64, string, error) {
	accountData, err := tb.rpcClient.GetAccountData(ctx, tssPDA)
	if err != nil {
		return 0, "", fmt.Errorf("failed to fetch TSS PDA account: %w", err)
	}

	// Need at least: discriminator(8) + tss_eth_address(20) + chain_id_len(4) = 32 bytes
	if len(accountData) < 32 {
		return 0, "", fmt.Errorf("invalid TSS PDA account data: too short (%d bytes)", len(accountData))
	}

	// chain_id is a Borsh String: 4-byte LE length prefix followed by UTF-8 bytes.
	// This is NOT fixed-length — different clusters have different chain IDs.
	chainIDLen := binary.LittleEndian.Uint32(accountData[28:32])

	requiredLen := 32 + int(chainIDLen) + 8
	if len(accountData) < requiredLen {
		return 0, "", fmt.Errorf("invalid TSS PDA account data: too short for chain_id length %d (%d bytes)", chainIDLen, len(accountData))
	}

	chainID := string(accountData[32 : 32+chainIDLen])

	// Nonce is right after the chain_id bytes
	nonceOffset := 32 + int(chainIDLen)
	nonce := binary.LittleEndian.Uint64(accountData[nonceOffset : nonceOffset+8])

	return nonce, chainID, nil
}

// =============================================================================
//  Instruction ID Mapping
// =============================================================================

// determineInstructionID maps the Push Chain TxType + asset type to the gateway's instruction ID.
//
// The gateway contract uses these IDs in the TSS message and the instruction data:
//
//	ID  Function                     When
//	1   withdraw_and_execute         FUNDS (withdraw mode): send SOL or SPL tokens to a recipient
//	2   withdraw_and_execute         FUNDS_AND_PAYLOAD or GAS_AND_PAYLOAD (execute mode): call a program
//	3   revert_universal_tx          INBOUND_REVERT + native SOL: refund SOL for a failed cross-chain tx
//	4   revert_universal_tx_token    INBOUND_REVERT + SPL token: refund SPL tokens for a failed tx
func (tb *TxBuilder) determineInstructionID(txType uetypes.TxType, isNative bool) (uint8, error) {
	switch txType {
	case uetypes.TxType_FUNDS:
		return 1, nil

	case uetypes.TxType_FUNDS_AND_PAYLOAD, uetypes.TxType_GAS_AND_PAYLOAD:
		return 2, nil

	case uetypes.TxType_INBOUND_REVERT:
		if isNative {
			return 3, nil
		}
		return 4, nil

	default:
		return 0, fmt.Errorf("unsupported tx type for SVM: %s", txType.String())
	}
}

// =============================================================================
//  TSS Message Construction
// =============================================================================

// constructTSSMessage builds the byte message that TSS validators sign.
//
// The gateway contract (tss.rs validate_message) reconstructs this exact same message
// on-chain to verify the signature. If even one byte differs, verification fails.
//
// Message format:
//
//	"PUSH_CHAIN_SVM"              — 14-byte ASCII prefix (prevents cross-protocol replay)
//	instruction_id                — 1 byte (1=withdraw, 2=execute, 3=revert SOL, 4=revert SPL)
//	chain_id                      — N bytes UTF-8 (prevents cross-chain replay, e.g., "devnet")
//	                                NOTE: raw UTF-8 bytes, NOT Borsh-encoded (no length prefix)
//	nonce                         — 8 bytes big-endian (prevents same-chain replay)
//	amount                        — 8 bytes big-endian
//	additional_data               — varies by instruction_id (see below)
//
// Additional data per instruction_id:
//
//	1 (withdraw): [tx_id(32), utx_id(32), sender(20), token(32), gas_fee(8 BE), target(32)]
//	2 (execute):  [tx_id(32), utx_id(32), sender(20), token(32), gas_fee(8 BE), target(32),
//	               accounts_buf(4 BE count + [pubkey(32) + writable(1)] per account),
//	               ix_data_buf(4 BE len + data), rent_fee(8 BE)]
//	3 (revert SOL): [utx_id(32), tx_id(32), recipient(32), gas_fee(8 BE)]
//	4 (revert SPL): [utx_id(32), tx_id(32), mint(32), recipient(32), gas_fee(8 BE)]
//
// The final message is hashed with keccak256 (Solana's keccak::hash = Ethereum's keccak256).
func (tb *TxBuilder) constructTSSMessage(
	instructionID uint8,
	chainID string,
	nonce uint64,
	amount uint64,
	txID [32]byte,
	universalTxID [32]byte,
	sender [20]byte,
	token [32]byte,
	gasFee uint64,
	targetProgram [32]byte,
	execAccounts []GatewayAccountMeta,
	ixData []byte,
	rentFee uint64,
	revertRecipient [32]byte,
	revertMint [32]byte,
) ([]byte, error) {
	message := []byte("PUSH_CHAIN_SVM")
	message = append(message, instructionID)
	message = append(message, []byte(chainID)...)

	nonceBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(nonceBytes, nonce)
	message = append(message, nonceBytes...)

	amountBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(amountBytes, amount)
	message = append(message, amountBytes...)

	gasFeeBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(gasFeeBytes, gasFee)

	switch instructionID {
	case 1: // withdraw
		message = append(message, txID[:]...)
		message = append(message, universalTxID[:]...)
		message = append(message, sender[:]...)
		message = append(message, token[:]...)
		message = append(message, gasFeeBytes...)
		message = append(message, targetProgram[:]...)

	case 2: // execute
		message = append(message, txID[:]...)
		message = append(message, universalTxID[:]...)
		message = append(message, sender[:]...)
		message = append(message, token[:]...)
		message = append(message, gasFeeBytes...)
		message = append(message, targetProgram[:]...)

		// Encode the execute accounts into the message so the contract can verify
		// that the accounts passed to the CPI match what was signed
		accountsCount := make([]byte, 4)
		binary.BigEndian.PutUint32(accountsCount, uint32(len(execAccounts)))
		message = append(message, accountsCount...)
		for _, acc := range execAccounts {
			message = append(message, acc.Pubkey[:]...)
			if acc.IsWritable {
				message = append(message, 1)
			} else {
				message = append(message, 0)
			}
		}

		ixDataLen := make([]byte, 4)
		binary.BigEndian.PutUint32(ixDataLen, uint32(len(ixData)))
		message = append(message, ixDataLen...)
		message = append(message, ixData...)

		rentFeeBytes := make([]byte, 8)
		binary.BigEndian.PutUint64(rentFeeBytes, rentFee)
		message = append(message, rentFeeBytes...)

	case 3: // revert SOL — note: utx_id comes before tx_id (matches Rust)
		message = append(message, universalTxID[:]...)
		message = append(message, txID[:]...)
		message = append(message, revertRecipient[:]...)
		message = append(message, gasFeeBytes...)

	case 4: // revert SPL — note: includes the mint address for token identification
		message = append(message, universalTxID[:]...)
		message = append(message, txID[:]...)
		message = append(message, revertMint[:]...)
		message = append(message, revertRecipient[:]...)
		message = append(message, gasFeeBytes...)

	default:
		return nil, fmt.Errorf("unknown instruction ID: %d", instructionID)
	}

	// Hash with keccak256. Solana's keccak::hash is the same algorithm as Ethereum's keccak256.
	// NOT sha256 — Anchor uses sha256 for discriminators, but TSS messages use keccak256.
	messageHash := crypto.Keccak256(message)

	return messageHash, nil
}

// =============================================================================
//  Parsing Helpers
// =============================================================================

// parseTxType converts a TxType string (e.g., "FUNDS", "INBOUND_REVERT") or
// numeric string (e.g., "3") to the protobuf enum value.
func parseTxType(txTypeStr string) (uetypes.TxType, error) {
	txTypeStr = strings.TrimSpace(strings.ToUpper(txTypeStr))

	if val, ok := uetypes.TxType_value[txTypeStr]; ok {
		return uetypes.TxType(val), nil
	}

	if num, err := strconv.ParseInt(txTypeStr, 10, 32); err == nil {
		return uetypes.TxType(num), nil
	}

	return uetypes.TxType_UNSPECIFIED_TX, fmt.Errorf("unknown tx type: %s", txTypeStr)
}

// =============================================================================
//  Relayer Keypair
// =============================================================================

// loadRelayerKeypair loads the Solana relayer keypair from disk.
//
// The file is a JSON array of 64 bytes (Solana's standard keypair format):
//   - First 32 bytes: Ed25519 private key seed
//   - Last 32 bytes: Ed25519 public key
//
// Located at: <nodeHome>/relayer/solana.json
// The relayer pays for Solana transaction fees and signs the transaction envelope.
func (tb *TxBuilder) loadRelayerKeypair() (solana.PrivateKey, error) {
	chainParts := strings.Split(tb.chainID, ":")
	if len(chainParts) == 0 {
		return nil, fmt.Errorf("invalid chain ID format: %s", tb.chainID)
	}
	namespace := chainParts[0]
	if namespace == "" {
		return nil, fmt.Errorf("empty namespace in chain ID: %s", tb.chainID)
	}

	keyPath := filepath.Join(tb.nodeHome, constant.RelayerSubdir, namespace+".json")

	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read relayer key file %s: %w", keyPath, err)
	}

	var keyBytes []byte
	if err := json.Unmarshal(keyData, &keyBytes); err != nil {
		return nil, fmt.Errorf("failed to parse key file as JSON array: %w", err)
	}

	if len(keyBytes) != 64 {
		return nil, fmt.Errorf("invalid key length: expected 64 bytes, got %d", len(keyBytes))
	}

	privateKey := solana.PrivateKey(keyBytes)

	return privateKey, nil
}

// =============================================================================
//  Recovery ID
// =============================================================================

// determineRecoveryID finds the ECDSA recovery ID (v value, 0-3) for a secp256k1 signature.
//
// Background: An ECDSA signature (r, s) can correspond to up to 4 different public keys.
// The recovery ID tells secp256k1_recover which one to use. In Ethereum, this is the "v" value.
//
// How it works:
//  1. Try each recovery ID (0, 1, 2, 3)
//  2. Recover the public key from (messageHash, signature, recoveryID)
//  3. Derive the ETH address from the recovered public key: keccak256(pubkey[1:])[-20:]
//  4. Compare with the expected TSS ETH address stored on-chain
//  5. Return the recovery ID that matches
//
// This is needed because the TSS signing protocol returns only (r, s) without v.
func (tb *TxBuilder) determineRecoveryID(messageHash []byte, signature []byte, expectedAddress string) (byte, error) {
	for recoveryID := byte(0); recoveryID < 4; recoveryID++ {
		sigWithRecovery := make([]byte, 65)
		copy(sigWithRecovery[:64], signature)
		sigWithRecovery[64] = recoveryID

		pubKey, err := crypto.SigToPub(messageHash, sigWithRecovery)
		if err != nil {
			continue
		}

		pubKeyBytes := crypto.FromECDSAPub(pubKey)
		addressBytes := crypto.Keccak256(pubKeyBytes[1:])[12:]

		expectedBytes, err := hex.DecodeString(removeHexPrefix(expectedAddress))
		if err == nil && len(expectedBytes) == 20 {
			if hex.EncodeToString(addressBytes) == hex.EncodeToString(expectedBytes) {
				return recoveryID, nil
			}
		}
	}

	return 0, fmt.Errorf("failed to determine recovery ID for signature")
}

// =============================================================================
//  Payload Decoding (Execute Mode)
// =============================================================================

// decodePayload decodes the pre-encoded payload from a Push Chain event.
//
// The payload is built off-chain and encodes the operation type plus any
// target program data needed for execution:
//
//	[u32 BE]         accounts_count — how many accounts the target program needs
//	[33 bytes] × N   accounts — each is [pubkey(32) + is_writable(1)]
//	[u32 BE]         ix_data_len — length of the instruction data for the target program
//	[N bytes]        ix_data — the raw instruction data to pass to the target program
//	[u64 BE]         rent_fee — SOL to cover rent for any new accounts created
//	[u8]             instruction_id — 1=withdraw, 2=execute
//
// For withdraw (instruction_id=1): accounts_count=0, ix_data_len=0, rent_fee=0
// For execute (instruction_id=2): accounts and ix_data contain CPI data
func decodePayload(payload []byte) ([]GatewayAccountMeta, []byte, uint64, uint8, error) {
	// Minimum payload: accounts_count(4) + ix_data_len(4) + rent_fee(8) + instruction_id(1) = 17
	if len(payload) < 17 {
		return nil, nil, 0, 0, fmt.Errorf("payload too short: %d bytes (minimum 17)", len(payload))
	}

	offset := 0

	accountsCount := binary.BigEndian.Uint32(payload[offset : offset+4])
	offset += 4

	accounts := make([]GatewayAccountMeta, accountsCount)
	for i := uint32(0); i < accountsCount; i++ {
		if offset+33 > len(payload) {
			return nil, nil, 0, 0, fmt.Errorf("payload too short for account %d", i)
		}
		var pubkey [32]byte
		copy(pubkey[:], payload[offset:offset+32])
		isWritable := payload[offset+32] == 1
		accounts[i] = GatewayAccountMeta{Pubkey: pubkey, IsWritable: isWritable}
		offset += 33
	}

	if offset+4 > len(payload) {
		return nil, nil, 0, 0, fmt.Errorf("payload too short for ix_data length")
	}
	ixDataLen := binary.BigEndian.Uint32(payload[offset : offset+4])
	offset += 4

	if offset+int(ixDataLen) > len(payload) {
		return nil, nil, 0, 0, fmt.Errorf("payload too short for ix_data")
	}
	ixData := make([]byte, ixDataLen)
	copy(ixData, payload[offset:offset+int(ixDataLen)])
	offset += int(ixDataLen)

	if offset+8 > len(payload) {
		return nil, nil, 0, 0, fmt.Errorf("payload too short for rent_fee")
	}
	rentFee := binary.BigEndian.Uint64(payload[offset : offset+8])
	offset += 8

	if offset >= len(payload) {
		return nil, nil, 0, 0, fmt.Errorf("payload too short for instruction_id")
	}
	instructionID := payload[offset]

	return accounts, ixData, rentFee, instructionID, nil
}

// accountsToWritableFlags bitpacks the writable flags for execute accounts into a compact byte array.
//
// The gateway contract expects a compact representation where each bit indicates whether
// the corresponding account is writable. Packing is MSB-first:
//
//	Account index:   0  1  2  3  4  5  6  7  |  8  9  ...
//	Bit position:    7  6  5  4  3  2  1  0  |  7  6  ...
//	Byte index:      --------  0  --------   |  ---  1  ---
//
// Example: accounts [W, R, W, R, R, R, R, R] → bit pattern 10100000 = 0xA0
func accountsToWritableFlags(accounts []GatewayAccountMeta) []byte {
	if len(accounts) == 0 {
		return []byte{}
	}
	flagsLen := (len(accounts) + 7) / 8
	flags := make([]byte, flagsLen)
	for i, acc := range accounts {
		if acc.IsWritable {
			byteIdx := i / 8
			bitIdx := 7 - (i % 8)
			flags[byteIdx] |= 1 << uint(bitIdx)
		}
	}
	return flags
}

// =============================================================================
//  Anchor Discriminator
// =============================================================================

// anchorDiscriminator computes the Anchor framework's instruction discriminator.
//
// Anchor uses the first 8 bytes of sha256("global:<method_name>") to identify which
// instruction handler to call. This is similar to EVM function selectors (first 4 bytes
// of keccak256), but Anchor uses SHA256 and 8 bytes instead.
//
// NOTE: Discriminators use SHA256, NOT keccak256. The TSS message uses keccak256.
// These are two different hash functions used for different purposes.
func anchorDiscriminator(methodName string) []byte {
	h := sha256.Sum256([]byte("global:" + methodName))
	return h[:8]
}

// =============================================================================
//  Instruction Data Builders (Borsh Serialization)
// =============================================================================

// buildWithdrawAndExecuteData constructs the Borsh-serialized instruction data for
// the withdraw_and_execute gateway function.
//
// This is the exact byte layout that the Anchor deserializer expects on-chain.
// The field order MUST match the Rust function signature in withdraw_execute.rs:
//
//	Offset  Size     Field                  Borsh Type
//	0       8        discriminator          sha256("global:withdraw_and_execute")[:8]
//	8       1        instruction_id         u8
//	9       32       tx_id                  [u8; 32]
//	41      32       universal_tx_id        [u8; 32]
//	73      8        amount                 u64 (little-endian)
//	81      20       sender                 [u8; 20]
//	101     4+N      writable_flags         Vec<u8> (4-byte LE length + data)
//	...     4+M      ix_data                Vec<u8> (4-byte LE length + data)
//	...     8        gas_fee                u64 (little-endian)
//	...     8        rent_fee               u64 (little-endian)
//	...     64       signature              [u8; 64] (TSS secp256k1 r||s)
//	...     1        recovery_id            u8 (ECDSA v value, 0-3)
//	...     32       message_hash           [u8; 32] (keccak256 of TSS message)
//	...     8        nonce                  u64 (little-endian)
func (tb *TxBuilder) buildWithdrawAndExecuteData(
	instructionID uint8,
	txID [32]byte,
	universalTxID [32]byte,
	amount uint64,
	sender [20]byte,
	writableFlags []byte,
	ixData []byte,
	gasFee uint64,
	rentFee uint64,
	signature []byte,
	recoveryID byte,
	messageHash []byte,
	nonce uint64,
) []byte {
	discriminator := anchorDiscriminator("withdraw_and_execute")

	data := make([]byte, 0, 256)
	data = append(data, discriminator...)
	data = append(data, instructionID)
	data = append(data, txID[:]...)
	data = append(data, universalTxID[:]...)

	amountBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(amountBytes, amount)
	data = append(data, amountBytes...)

	data = append(data, sender[:]...)

	// Vec<u8> in Borsh = 4-byte LE length prefix + raw bytes
	wfLen := make([]byte, 4)
	binary.LittleEndian.PutUint32(wfLen, uint32(len(writableFlags)))
	data = append(data, wfLen...)
	data = append(data, writableFlags...)

	ixLen := make([]byte, 4)
	binary.LittleEndian.PutUint32(ixLen, uint32(len(ixData)))
	data = append(data, ixLen...)
	data = append(data, ixData...)

	gasFeeBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(gasFeeBytes, gasFee)
	data = append(data, gasFeeBytes...)

	rentFeeBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(rentFeeBytes, rentFee)
	data = append(data, rentFeeBytes...)

	data = append(data, signature...)
	data = append(data, recoveryID)
	data = append(data, messageHash...)

	nonceBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(nonceBytes, nonce)
	data = append(data, nonceBytes...)

	return data
}

// buildRevertData constructs the Borsh-serialized instruction data for
// revert_universal_tx (id=3, native SOL) or revert_universal_tx_token (id=4, SPL).
//
// Both revert functions share the same parameter layout — only the discriminator differs.
//
//	Offset  Size     Field
//	0       8        discriminator          (different for SOL vs SPL)
//	8       32       tx_id                  [u8; 32]
//	40      32       universal_tx_id        [u8; 32]
//	72      8        amount                 u64 (LE)
//	80      32       fund_recipient         Pubkey — who gets the refund
//	112     4+N      revert_msg             Vec<u8> — human-readable revert reason
//	...     8        gas_fee                u64 (LE)
//	...     64       signature              [u8; 64]
//	...     1        recovery_id            u8
//	...     32       message_hash           [u8; 32]
//	...     8        nonce                  u64 (LE)
func (tb *TxBuilder) buildRevertData(
	instructionID uint8,
	txID [32]byte,
	universalTxID [32]byte,
	amount uint64,
	fundRecipient solana.PublicKey,
	revertMsg []byte,
	gasFee uint64,
	signature []byte,
	recoveryID byte,
	messageHash []byte,
	nonce uint64,
) []byte {
	var discriminator []byte
	if instructionID == 3 {
		discriminator = anchorDiscriminator("revert_universal_tx")
	} else {
		discriminator = anchorDiscriminator("revert_universal_tx_token")
	}

	data := make([]byte, 0, 256)
	data = append(data, discriminator...)
	data = append(data, txID[:]...)
	data = append(data, universalTxID[:]...)

	amountBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(amountBytes, amount)
	data = append(data, amountBytes...)

	// RevertInstructions struct: { fund_recipient: Pubkey, revert_msg: Vec<u8> }
	data = append(data, fundRecipient.Bytes()...)
	revertMsgLen := make([]byte, 4)
	binary.LittleEndian.PutUint32(revertMsgLen, uint32(len(revertMsg)))
	data = append(data, revertMsgLen...)
	data = append(data, revertMsg...)

	gasFeeBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(gasFeeBytes, gasFee)
	data = append(data, gasFeeBytes...)

	data = append(data, signature...)
	data = append(data, recoveryID)
	data = append(data, messageHash...)

	nonceBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(nonceBytes, nonce)
	data = append(data, nonceBytes...)

	return data
}

// =============================================================================
//  Accounts List Builders
//
//  Solana instructions require an explicit list of every account they touch,
//  in the exact order the on-chain program expects. Getting this wrong causes
//  "invalid account" errors or silent data corruption.
//
//  The order must match the Rust #[derive(Accounts)] struct in the gateway code.
// =============================================================================

// buildWithdrawAndExecuteAccounts builds the ordered accounts list for the
// withdraw_and_execute instruction.
//
// Must match the WithdrawAndExecute struct in withdraw_execute.rs:
//
//	#   Account                Flags       Notes
//	1   caller                 signer,mut  Relayer who pays for the tx
//	2   config                 read-only   Gateway configuration PDA
//	3   vault_sol              mut         SOL vault PDA (holds native SOL)
//	4   cea_authority          mut         Cross-chain identity PDA for this sender
//	5   tss_pda                mut         TSS state (nonce gets incremented)
//	6   executed_tx            mut         Replay protection PDA (gets created)
//	7   system_program         read-only   Solana system program
//	8   destination_program    read-only   Target program (system_program for withdraw)
//	--- Optional SPL accounts (9-16) ---
//	9   recipient              mut/None    Withdraw: recipient wallet. Execute: None
//	10  vault_ata              mut/None    Vault's token account for the SPL mint
//	11  cea_ata                mut/None    CEA's token account for the SPL mint
//	12  mint                   read/None   The SPL token mint
//	13  token_program          read/None   SPL Token program
//	14  rent                   read/None   Rent sysvar
//	15  associated_token_prog  read/None   ATA program
//	16  recipient_ata          mut/None    Recipient's token account (withdraw SPL only)
//	--- Execute-only remaining accounts ---
//	17+ remaining_accounts     varies      Accounts that the target program needs
//
// For Anchor Option<Account> fields: passing the gateway program's own ID = None.
// This is Anchor's convention for encoding "this optional account is not provided".
func (tb *TxBuilder) buildWithdrawAndExecuteAccounts(
	caller solana.PublicKey,
	configPDA solana.PublicKey,
	vaultPDA solana.PublicKey,
	ceaAuthorityPDA solana.PublicKey,
	tssPDA solana.PublicKey,
	executedTxPDA solana.PublicKey,
	destinationProgram solana.PublicKey,
	isNative bool,
	instructionID uint8,
	recipientPubkey solana.PublicKey,
	mintPubkey solana.PublicKey,
	execAccounts []GatewayAccountMeta,
) []*solana.AccountMeta {
	// First 8 required accounts (always present)
	accounts := []*solana.AccountMeta{
		{PublicKey: caller, IsWritable: true, IsSigner: true},
		{PublicKey: configPDA, IsWritable: false, IsSigner: false},
		{PublicKey: vaultPDA, IsWritable: true, IsSigner: false},
		{PublicKey: ceaAuthorityPDA, IsWritable: true, IsSigner: false},
		{PublicKey: tssPDA, IsWritable: true, IsSigner: false},
		{PublicKey: executedTxPDA, IsWritable: true, IsSigner: false},
		{PublicKey: solana.SystemProgramID, IsWritable: false, IsSigner: false},
		{PublicKey: destinationProgram, IsWritable: false, IsSigner: false},
	}

	// Optional SPL accounts (#9-16)
	// For native SOL: all optional accounts are set to the gateway program ID (= None sentinel)
	// For SPL tokens: real ATAs and program addresses are provided
	if isNative {
		// Recipient: real for withdraw, None for execute
		if instructionID == 1 {
			accounts = append(accounts, &solana.AccountMeta{PublicKey: recipientPubkey, IsWritable: true, IsSigner: false})
		} else {
			accounts = append(accounts, &solana.AccountMeta{PublicKey: tb.gatewayAddress, IsWritable: false, IsSigner: false})
		}
		// All SPL-related accounts are None
		for i := 0; i < 7; i++ {
			accounts = append(accounts, &solana.AccountMeta{PublicKey: tb.gatewayAddress, IsWritable: false, IsSigner: false})
		}
	} else {
		// SPL token flow: derive and pass real ATAs
		ataProgramID := solana.MustPublicKeyFromBase58("ATokenGPvbdGVxr1b2hvZbsiqW5xWH25efTNsLJA8knL")
		rentSysvar := solana.MustPublicKeyFromBase58("SysvarRent111111111111111111111111111111111")

		vaultATA, _, _ := solana.FindProgramAddress(
			[][]byte{accounts[2].PublicKey.Bytes(), solana.TokenProgramID.Bytes(), mintPubkey.Bytes()},
			ataProgramID,
		)
		ceaATA, _, _ := solana.FindProgramAddress(
			[][]byte{ceaAuthorityPDA.Bytes(), solana.TokenProgramID.Bytes(), mintPubkey.Bytes()},
			ataProgramID,
		)

		if instructionID == 1 {
			accounts = append(accounts, &solana.AccountMeta{PublicKey: recipientPubkey, IsWritable: true, IsSigner: false})
		} else {
			accounts = append(accounts, &solana.AccountMeta{PublicKey: tb.gatewayAddress, IsWritable: false, IsSigner: false})
		}
		accounts = append(accounts, &solana.AccountMeta{PublicKey: vaultATA, IsWritable: true, IsSigner: false})
		accounts = append(accounts, &solana.AccountMeta{PublicKey: ceaATA, IsWritable: true, IsSigner: false})
		accounts = append(accounts, &solana.AccountMeta{PublicKey: mintPubkey, IsWritable: false, IsSigner: false})
		accounts = append(accounts, &solana.AccountMeta{PublicKey: solana.TokenProgramID, IsWritable: false, IsSigner: false})
		accounts = append(accounts, &solana.AccountMeta{PublicKey: rentSysvar, IsWritable: false, IsSigner: false})
		accounts = append(accounts, &solana.AccountMeta{PublicKey: ataProgramID, IsWritable: false, IsSigner: false})

		if instructionID == 1 {
			recipientATA, _, _ := solana.FindProgramAddress(
				[][]byte{recipientPubkey.Bytes(), solana.TokenProgramID.Bytes(), mintPubkey.Bytes()},
				ataProgramID,
			)
			accounts = append(accounts, &solana.AccountMeta{PublicKey: recipientATA, IsWritable: true, IsSigner: false})
		} else {
			accounts = append(accounts, &solana.AccountMeta{PublicKey: tb.gatewayAddress, IsWritable: false, IsSigner: false})
		}
	}

	// For execute mode: append the target program's accounts as "remaining_accounts".
	// These are the accounts that the gateway will pass through via CPI to the target program.
	if instructionID == 2 {
		for _, acc := range execAccounts {
			pubkey := solana.PublicKeyFromBytes(acc.Pubkey[:])
			accounts = append(accounts, &solana.AccountMeta{
				PublicKey:  pubkey,
				IsWritable: acc.IsWritable,
				IsSigner:   false,
			})
		}
	}

	return accounts
}

// VerifyBroadcastedTx checks the status of a broadcasted transaction on Solana.
// Returns (found, confirmations, status, error):
// - found=false: tx not found or not yet confirmed
// - found=true: tx exists on-chain
//   - confirmations: number of slots since the tx was included (0 = just confirmed)
//   - status: 0 = failed, 1 = success
func (tb *TxBuilder) VerifyBroadcastedTx(ctx context.Context, txHash string) (found bool, confirmations uint64, status uint8, err error) {
	sig, sigErr := solana.SignatureFromBase58(txHash)
	if sigErr != nil {
		return false, 0, 0, nil
	}

	tx, txErr := tb.rpcClient.GetTransaction(ctx, sig)
	if txErr != nil {
		return false, 0, 0, nil
	}

	if tx == nil {
		return false, 0, 0, nil
	}

	// Calculate confirmations from current slot
	var confs uint64
	if tx.Slot > 0 {
		latestSlot, slotErr := tb.rpcClient.GetLatestSlot(ctx)
		if slotErr == nil && latestSlot >= tx.Slot {
			confs = latestSlot - tx.Slot + 1
		}
	}

	// Check if transaction had an error
	if tx.Meta != nil && tx.Meta.Err != nil {
		return true, confs, 0, nil
	}

	return true, confs, 1, nil
}

// buildSetComputeUnitLimitInstruction creates a SetComputeUnitLimit instruction for the Compute Budget program
// Instruction format: [1-byte instruction type (2 = SetComputeUnitLimit)] + [4-byte u32 units]
// buildRevertSOLAccounts builds the accounts list for revert_universal_tx (native SOL refund).
//
// Must match the RevertUniversalTx struct in revert.rs:
//
//	#  Account          Flags
//	1  config           read-only
//	2  vault            mut         SOL vault (source of refund)
//	3  tss_pda          mut         TSS state (nonce increment)
//	4  recipient        mut         Gets the SOL refund
//	5  executed_tx       mut         Replay protection (gets created)
//	6  caller           signer,mut  Relayer
//	7  system_program   read-only
func (tb *TxBuilder) buildRevertSOLAccounts(
	configPDA solana.PublicKey,
	vaultPDA solana.PublicKey,
	tssPDA solana.PublicKey,
	recipient solana.PublicKey,
	executedTxPDA solana.PublicKey,
	caller solana.PublicKey,
) []*solana.AccountMeta {
	return []*solana.AccountMeta{
		{PublicKey: configPDA, IsWritable: false, IsSigner: false},
		{PublicKey: vaultPDA, IsWritable: true, IsSigner: false},
		{PublicKey: tssPDA, IsWritable: true, IsSigner: false},
		{PublicKey: recipient, IsWritable: true, IsSigner: false},
		{PublicKey: executedTxPDA, IsWritable: true, IsSigner: false},
		{PublicKey: caller, IsWritable: true, IsSigner: true},
		{PublicKey: solana.SystemProgramID, IsWritable: false, IsSigner: false},
	}
}

// buildRevertSPLAccounts builds the accounts list for revert_universal_tx_token (SPL token refund).
//
// Must match the RevertUniversalTxToken struct in revert.rs:
//
//	#   Account                  Flags
//	1   config                   read-only
//	2   vault                    mut         SOL vault PDA (authority for token transfers)
//	3   token_vault              mut         Vault's ATA for the token (source of refund)
//	4   tss_pda                  mut         TSS state (nonce increment)
//	5   recipient_token_account  mut         Recipient's ATA (destination of refund)
//	6   token_mint               read-only   The SPL token mint
//	7   executed_tx              mut         Replay protection
//	8   caller                   signer,mut  Relayer
//	9   vault_sol                mut         Same as vault, needed for gas_fee transfer
//	10  token_program            read-only   SPL Token program
//	11  system_program           read-only
func (tb *TxBuilder) buildRevertSPLAccounts(
	configPDA solana.PublicKey,
	vaultPDA solana.PublicKey,
	tokenVaultATA solana.PublicKey,
	tssPDA solana.PublicKey,
	recipientATA solana.PublicKey,
	tokenMint solana.PublicKey,
	executedTxPDA solana.PublicKey,
	caller solana.PublicKey,
) []*solana.AccountMeta {
	return []*solana.AccountMeta{
		{PublicKey: configPDA, IsWritable: false, IsSigner: false},
		{PublicKey: vaultPDA, IsWritable: true, IsSigner: false},
		{PublicKey: tokenVaultATA, IsWritable: true, IsSigner: false},
		{PublicKey: tssPDA, IsWritable: true, IsSigner: false},
		{PublicKey: recipientATA, IsWritable: true, IsSigner: false},
		{PublicKey: tokenMint, IsWritable: false, IsSigner: false},
		{PublicKey: executedTxPDA, IsWritable: true, IsSigner: false},
		{PublicKey: caller, IsWritable: true, IsSigner: true},
		{PublicKey: vaultPDA, IsWritable: true, IsSigner: false}, // vault_sol = same PDA, needed for gas_fee
		{PublicKey: solana.TokenProgramID, IsWritable: false, IsSigner: false},
		{PublicKey: solana.SystemProgramID, IsWritable: false, IsSigner: false},
	}
}

// =============================================================================
//  Compute Budget
// =============================================================================

// buildSetComputeUnitLimitInstruction creates a Solana Compute Budget instruction
// that tells the runtime how many compute units to allocate for this transaction.
//
// This is Solana's equivalent of EVM gas limit. If the transaction uses more compute
// units than allocated, it fails. Setting it too high wastes priority fee.
//
// Instruction format:
//
//	Byte 0:    instruction type (2 = SetComputeUnitLimit)
//	Bytes 1-4: units (u32, little-endian)
func (tb *TxBuilder) buildSetComputeUnitLimitInstruction(units uint32) solana.Instruction {
	computeBudgetProgramID := solana.MustPublicKeyFromBase58("ComputeBudget111111111111111111111111111111")

	data := make([]byte, 5)
	data[0] = 2 // SetComputeUnitLimit
	binary.LittleEndian.PutUint32(data[1:], units)

	return solana.NewInstruction(
		computeBudgetProgramID,
		[]*solana.AccountMeta{},
		data,
	)
}

// =============================================================================
//  ATA Creation
// =============================================================================

// buildCreateATAIdempotentInstruction builds a CreateIdempotent instruction for the
// Associated Token Account (ATA) program. This creates the recipient's ATA if it
// doesn't exist, or succeeds as a no-op if it already exists.
//
// This is needed for SPL withdraw and SPL revert flows because the gateway contract
// validates that the recipient ATA exists but does NOT create it. The relayer pays
// the ATA rent (~0.002 SOL) which is reimbursed via the gas_fee.
//
// ATA program instruction indices:
//
//	0 = Create (fails if ATA exists)
//	1 = CreateIdempotent (no-op if ATA exists) ← we use this
func (tb *TxBuilder) buildCreateATAIdempotentInstruction(
	payer solana.PublicKey,
	owner solana.PublicKey,
	mint solana.PublicKey,
) solana.Instruction {
	ataProgramID := solana.MustPublicKeyFromBase58("ATokenGPvbdGVxr1b2hvZbsiqW5xWH25efTNsLJA8knL")

	// Derive the ATA address deterministically from (owner, token_program, mint)
	ata, _, _ := solana.FindProgramAddress(
		[][]byte{owner.Bytes(), solana.TokenProgramID.Bytes(), mint.Bytes()},
		ataProgramID,
	)

	accounts := []*solana.AccountMeta{
		{PublicKey: payer, IsWritable: true, IsSigner: true},
		{PublicKey: ata, IsWritable: true, IsSigner: false},
		{PublicKey: owner, IsWritable: false, IsSigner: false},
		{PublicKey: mint, IsWritable: false, IsSigner: false},
		{PublicKey: solana.SystemProgramID, IsWritable: false, IsSigner: false},
		{PublicKey: solana.TokenProgramID, IsWritable: false, IsSigner: false},
	}

	// Instruction index 1 = CreateIdempotent
	return solana.NewInstruction(ataProgramID, accounts, []byte{1})
}
