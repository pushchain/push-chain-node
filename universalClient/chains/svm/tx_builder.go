package svm

import (
	"context"
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

// DefaultComputeUnitLimit is used when gas limit is not provided in the outbound event data
// This is Solana's equivalent of EVM gas limit
const DefaultComputeUnitLimit = 200000

// TxBuilder implements OutboundTxBuilder for Solana chains
type TxBuilder struct {
	rpcClient      *RPCClient
	chainID        string
	gatewayAddress solana.PublicKey
	nodeHome       string
	logger         zerolog.Logger
}

// NewTxBuilder creates a new Solana transaction builder
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

// GetOutboundSigningRequest creates a signing request from outbound event data
// The signing hash is the keccak256 hash of the TSS message constructed according to the gateway contract
func (tb *TxBuilder) GetOutboundSigningRequest(
	ctx context.Context,
	data *uetypes.OutboundCreatedEvent,
	gasPrice *big.Int,
	signerAddress string,
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
	if signerAddress == "" {
		return nil, fmt.Errorf("signerAddress is required")
	}

	// Parse amount
	amount := new(big.Int)
	amount, ok := amount.SetString(data.Amount, 10)
	if !ok {
		return nil, fmt.Errorf("invalid amount: %s", data.Amount)
	}

	// Parse asset address (token mint for SPL, empty for native SOL)
	assetAddr := data.AssetAddr
	isNative := assetAddr == "" || assetAddr == "0x0" || assetAddr == "0x0000000000000000000000000000000000000000"

	// Parse TxType
	txType, err := parseTxType(data.TxType)
	if err != nil {
		return nil, fmt.Errorf("invalid tx type: %w", err)
	}

	// Derive TSS PDA
	tssPDA, err := tb.deriveTSSPDA()
	if err != nil {
		return nil, fmt.Errorf("failed to derive TSS PDA: %w", err)
	}

	// Fetch nonce from TSS PDA
	nonce, chainID, err := tb.fetchTSSNonce(ctx, tssPDA)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch TSS nonce: %w", err)
	}

	// Determine instruction ID based on TxType and asset type
	instructionID, err := tb.determineInstructionID(txType, isNative)
	if err != nil {
		return nil, fmt.Errorf("failed to determine instruction ID: %w", err)
	}

	// Parse recipient address (Solana Pubkey - 32 bytes)
	// Try parsing as Solana base58 first, then as hex
	var recipientPubkey solana.PublicKey
	var recipientBytes []byte
	recipientPubkey, err = solana.PublicKeyFromBase58(data.Recipient)
	if err != nil {
		// Try hex format
		hexBytes, hexErr := hex.DecodeString(removeHexPrefix(data.Recipient))
		if hexErr != nil || len(hexBytes) != 32 {
			return nil, fmt.Errorf("invalid recipient address format (expected Solana Pubkey): %s", data.Recipient)
		}
		recipientPubkey = solana.PublicKeyFromBytes(hexBytes)
	}
	recipientBytes = recipientPubkey.Bytes()

	// Construct TSS message for signing
	messageHash, err := tb.constructTSSMessage(
		instructionID,
		chainID,
		nonce,
		amount.Uint64(),
		isNative,
		assetAddr,
		recipientBytes,
		txType,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to construct TSS message: %w", err)
	}

	// Convert gas price to lamports (Solana's native unit)
	prioritizationFee := gasPrice.Uint64()

	return &common.UnSignedOutboundTxReq{
		SigningHash: messageHash, // This is the keccak256 hash to be signed by TSS
		Signer:      signerAddress,
		Nonce:       nonce,
		GasPrice:    big.NewInt(int64(prioritizationFee)),
	}, nil
}

// BroadcastOutboundSigningRequest assembles and broadcasts a signed transaction from the signing request, event data, and signature
// NOTE: This method will internally use a relayer key to create and send the Solana transaction
// The signature provided is the TSS signature for the message hash, which will be included in the instruction data
func (tb *TxBuilder) BroadcastOutboundSigningRequest(
	ctx context.Context,
	req *common.UnSignedOutboundTxReq,
	data *uetypes.OutboundCreatedEvent,
	signature []byte,
) (string, error) {
	if req == nil {
		return "", fmt.Errorf("signing request is nil")
	}
	if data == nil {
		return "", fmt.Errorf("outbound event data is nil")
	}
	if len(signature) != 64 {
		return "", fmt.Errorf("signature must be 64 bytes, got %d", len(signature))
	}

	// Load relayer keypair
	relayerKeypair, err := tb.loadRelayerKeypair()
	if err != nil {
		return "", fmt.Errorf("failed to load relayer keypair: %w", err)
	}

	// Reconstruct parameters from event data (same as GetOutboundSigningRequest)
	amount := new(big.Int)
	amount, ok := amount.SetString(data.Amount, 10)
	if !ok {
		return "", fmt.Errorf("invalid amount: %s", data.Amount)
	}

	assetAddr := data.AssetAddr
	isNative := assetAddr == "" || assetAddr == "0x0" || assetAddr == "0x0000000000000000000000000000000000000000"

	txType, err := parseTxType(data.TxType)
	if err != nil {
		return "", fmt.Errorf("invalid tx type: %w", err)
	}

	// Determine instruction ID
	instructionID, err := tb.determineInstructionID(txType, isNative)
	if err != nil {
		return "", fmt.Errorf("failed to determine instruction ID: %w", err)
	}

	// Parse recipient
	recipientPubkey, err := solana.PublicKeyFromBase58(data.Recipient)
	if err != nil {
		hexBytes, hexErr := hex.DecodeString(removeHexPrefix(data.Recipient))
		if hexErr != nil || len(hexBytes) != 32 {
			return "", fmt.Errorf("invalid recipient address format: %s", data.Recipient)
		}
		recipientPubkey = solana.PublicKeyFromBytes(hexBytes)
	}

	// Derive all required PDAs
	configPDA, _, err := solana.FindProgramAddress([][]byte{[]byte("config")}, tb.gatewayAddress)
	if err != nil {
		return "", fmt.Errorf("failed to derive config PDA: %w", err)
	}

	vaultPDA, _, err := solana.FindProgramAddress([][]byte{[]byte("vault")}, tb.gatewayAddress)
	if err != nil {
		return "", fmt.Errorf("failed to derive vault PDA: %w", err)
	}

	tssPDA, _, err := solana.FindProgramAddress([][]byte{[]byte("tss")}, tb.gatewayAddress)
	if err != nil {
		return "", fmt.Errorf("failed to derive TSS PDA: %w", err)
	}

	whitelistPDA, _, err := solana.FindProgramAddress([][]byte{[]byte("whitelist")}, tb.gatewayAddress)
	if err != nil {
		return "", fmt.Errorf("failed to derive whitelist PDA: %w", err)
	}

	// Derive token vault PDA and recipient token account for SPL tokens
	var tokenVaultPDA solana.PublicKey
	var recipientTokenAccount solana.PublicKey
	if !isNative {
		mintPubkey, err := solana.PublicKeyFromBase58(assetAddr)
		if err != nil {
			hexBytes, hexErr := hex.DecodeString(removeHexPrefix(assetAddr))
			if hexErr != nil || len(hexBytes) != 32 {
				return "", fmt.Errorf("invalid asset address format: %s", assetAddr)
			}
			mintPubkey = solana.PublicKeyFromBytes(hexBytes)
		}

		tokenVaultPDA, _, err = solana.FindProgramAddress([][]byte{[]byte("token_vault"), mintPubkey.Bytes()}, tb.gatewayAddress)
		if err != nil {
			return "", fmt.Errorf("failed to derive token vault PDA: %w", err)
		}

		// Derive associated token account for recipient
		// ATA derivation: PDA of [owner, token_program_id, mint] under AssociatedTokenProgram
		// Note: This is a simplified derivation. In production, use the proper ATA derivation
		// which uses the AssociatedTokenProgram (AToken9kL4eMniUP6vNrqG5nBR56fZc4f2TU3qH6w7)
		ataSeeds := [][]byte{
			recipientPubkey.Bytes(),
			solana.TokenProgramID.Bytes(),
			mintPubkey.Bytes(),
		}
		// Associated Token Program ID (standard Solana ATA program)
		ataProgramID := solana.MustPublicKeyFromBase58("ATokenGPvbdGVxr1b2hvZbsiqW5xWH25efTNsLJA8knL")
		recipientTokenAccount, _, err = solana.FindProgramAddress(ataSeeds, ataProgramID)
		if err != nil {
			return "", fmt.Errorf("failed to derive recipient token account: %w", err)
		}
	}

	// Determine recovery ID by verifying the signature against the message hash
	// The message hash is req.SigningHash (keccak256 hash of the TSS message)
	// We need to get the TSS Ethereum address from the TSS PDA account to verify the signature
	tssAccountData, err := tb.rpcClient.GetAccountData(ctx, tssPDA)
	if err != nil {
		return "", fmt.Errorf("failed to fetch TSS PDA account for recovery ID: %w", err)
	}
	// TSS Ethereum address is at offset 8 (after 8-byte discriminator), 20 bytes
	if len(tssAccountData) < 28 {
		return "", fmt.Errorf("invalid TSS PDA account data for recovery ID")
	}
	tssEthAddress := tssAccountData[8:28] // 20-byte Ethereum address

	recoveryID, err := tb.determineRecoveryID(req.SigningHash, signature, hex.EncodeToString(tssEthAddress))
	if err != nil {
		return "", fmt.Errorf("failed to determine recovery ID: %w", err)
	}

	// Parse revert message from event data
	revertMsgBytes, err := hex.DecodeString(removeHexPrefix(data.RevertMsg))
	if err != nil {
		revertMsgBytes = []byte{} // Default to empty if decoding fails
	}

	// Build instruction data
	// Format: [8-byte Anchor discriminator] + [1-byte instruction_id] + [64-byte signature] + [1-byte recovery_id] + [additional data]
	instructionData := tb.buildInstructionData(instructionID, signature, recoveryID, amount.Uint64(), recipientPubkey, isNative, assetAddr, revertMsgBytes)

	// Build accounts list
	accounts := tb.buildAccountsList(
		configPDA,
		vaultPDA,
		tssPDA,
		whitelistPDA,
		tokenVaultPDA,
		recipientPubkey,
		recipientTokenAccount,
		relayerKeypair.PublicKey(),
		isNative,
	)

	// Create main instruction
	instruction := solana.NewInstruction(
		tb.gatewayAddress,
		accounts,
		instructionData,
	)

	// Parse compute unit limit from gas limit, use default if not provided
	var computeUnitLimit uint32
	if data.GasLimit == "" || data.GasLimit == "0" {
		computeUnitLimit = DefaultComputeUnitLimit
	} else {
		parsedLimit, err := strconv.ParseUint(data.GasLimit, 10, 32)
		if err != nil {
			computeUnitLimit = DefaultComputeUnitLimit
		} else {
			computeUnitLimit = uint32(parsedLimit)
		}
	}

	// Create compute budget instruction for setting compute unit limit
	computeBudgetInstruction := tb.buildSetComputeUnitLimitInstruction(computeUnitLimit)

	// Get recent blockhash
	recentBlockhash, err := tb.rpcClient.GetRecentBlockhash(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get recent blockhash: %w", err)
	}

	// Create transaction with compute budget instruction first, then main instruction
	tx, err := solana.NewTransaction(
		[]solana.Instruction{computeBudgetInstruction, instruction},
		recentBlockhash,
		solana.TransactionPayer(relayerKeypair.PublicKey()),
	)
	if err != nil {
		return "", fmt.Errorf("failed to create transaction: %w", err)
	}

	// Sign transaction with relayer keypair
	_, err = tx.Sign(func(key solana.PublicKey) *solana.PrivateKey {
		if key.Equals(relayerKeypair.PublicKey()) {
			privKey := relayerKeypair
			return &privKey
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("failed to sign transaction: %w", err)
	}

	// Broadcast transaction
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

// Helper functions

func removeHexPrefix(s string) string {
	if len(s) >= 2 && s[0:2] == "0x" {
		return s[2:]
	}
	return s
}

// deriveTSSPDA derives the TSS PDA address using seeds [b"tss"]
func (tb *TxBuilder) deriveTSSPDA() (solana.PublicKey, error) {
	seeds := [][]byte{[]byte("tss")}
	address, _, err := solana.FindProgramAddress(seeds, tb.gatewayAddress)
	return address, err
}

// fetchTSSNonce fetches the nonce and chain ID from the TSS PDA account
// TSS PDA account structure (from state.rs):
// - discriminator: 8 bytes
// - tss_eth_address: 20 bytes
// - chain_id: 8 bytes (u64, little-endian)
// - nonce: 8 bytes (u64, little-endian)
// - authority: 32 bytes (Pubkey)
// - bump: 1 byte
func (tb *TxBuilder) fetchTSSNonce(ctx context.Context, tssPDA solana.PublicKey) (uint64, uint64, error) {
	accountData, err := tb.rpcClient.GetAccountData(ctx, tssPDA)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to fetch TSS PDA account: %w", err)
	}

	// Minimum account size check
	// discriminator (8) + tss_eth_address (20) + chain_id (8) + nonce (8) = 44 bytes minimum
	if len(accountData) < 44 {
		return 0, 0, fmt.Errorf("invalid TSS PDA account data: too short (%d bytes)", len(accountData))
	}

	// Skip discriminator (8 bytes) and tss_eth_address (20 bytes) = offset 28
	// Read chain_id (8 bytes, little-endian)
	chainID := binary.LittleEndian.Uint64(accountData[28:36])

	// Read nonce (8 bytes, little-endian)
	nonce := binary.LittleEndian.Uint64(accountData[36:44])

	return nonce, chainID, nil
}

// determineInstructionID determines the instruction ID based on TxType and asset type
// instruction_id = 1 for SOL withdraw
// instruction_id = 2 for SPL withdraw
// instruction_id = 3 for SOL revert
// instruction_id = 4 for SPL revert
func (tb *TxBuilder) determineInstructionID(txType uetypes.TxType, isNative bool) (uint8, error) {
	switch txType {
	case uetypes.TxType_FUNDS:
		if isNative {
			return 1, nil // withdraw_tss (SOL)
		}
		return 2, nil // withdraw_spl_token_tss (SPL)

	case uetypes.TxType_INBOUND_REVERT:
		if isNative {
			return 3, nil // revert_withdraw (SOL)
		}
		return 4, nil // revert_withdraw_spl_token (SPL)

	default:
		return 0, fmt.Errorf("unsupported tx type for SVM: %s", txType.String())
	}
}

// constructTSSMessage constructs the TSS message according to the gateway contract
// Message format: "PUSH_CHAIN_SVM" + instruction_id + chain_id + nonce + amount + additional_data
// Then hash with keccak256
func (tb *TxBuilder) constructTSSMessage(
	instructionID uint8,
	chainID uint64,
	nonce uint64,
	amount uint64,
	isNative bool,
	assetAddr string,
	recipient []byte,
	txType uetypes.TxType,
) ([]byte, error) {
	// Start with prefix
	message := []byte("PUSH_CHAIN_SVM")

	// Add instruction_id (1 byte)
	message = append(message, instructionID)

	// Add chain_id (8 bytes, big-endian)
	chainIDBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(chainIDBytes, chainID)
	message = append(message, chainIDBytes...)

	// Add nonce (8 bytes, big-endian)
	nonceBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(nonceBytes, nonce)
	message = append(message, nonceBytes...)

	// Add amount (8 bytes, big-endian)
	amountBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(amountBytes, amount)
	message = append(message, amountBytes...)

	// Add additional_data based on instruction type
	switch instructionID {
	case 1, 3: // SOL withdraw or revert
		// Additional data: recipient bytes (32 bytes for Solana Pubkey)
		// From withdraw.rs: recipient.key().to_bytes() or revert_instruction.fund_recipient.to_bytes()
		if len(recipient) != 32 {
			return nil, fmt.Errorf("invalid recipient length: expected 32 bytes (Solana Pubkey), got %d", len(recipient))
		}
		message = append(message, recipient...)

	case 2, 4: // SPL withdraw or revert
		// Additional data: token mint (32 bytes)
		if assetAddr == "" {
			return nil, fmt.Errorf("asset address required for SPL token operations")
		}
		// Parse asset address (should be a Solana public key in base58 or hex)
		mintPubkey, err := solana.PublicKeyFromBase58(assetAddr)
		if err != nil {
			// Try hex format
			hexBytes, hexErr := hex.DecodeString(removeHexPrefix(assetAddr))
			if hexErr != nil || len(hexBytes) != 32 {
				return nil, fmt.Errorf("invalid asset address format: %s", assetAddr)
			}
			mintPubkey = solana.PublicKeyFromBytes(hexBytes)
		}
		mintBytes := mintPubkey.Bytes()
		message = append(message, mintBytes...)

	default:
		return nil, fmt.Errorf("unknown instruction ID: %d", instructionID)
	}

	// Hash with keccak256
	messageHash := crypto.Keccak256(message)

	return messageHash, nil
}

// parseTxType parses the TxType string to uetypes.TxType enum
func parseTxType(txTypeStr string) (uetypes.TxType, error) {
	// Remove any whitespace and convert to uppercase
	txTypeStr = strings.TrimSpace(strings.ToUpper(txTypeStr))

	// Try to parse as enum name
	if val, ok := uetypes.TxType_value[txTypeStr]; ok {
		return uetypes.TxType(val), nil
	}

	// Try to parse as number
	if num, err := strconv.ParseInt(txTypeStr, 10, 32); err == nil {
		return uetypes.TxType(num), nil
	}

	return uetypes.TxType_UNSPECIFIED_TX, fmt.Errorf("unknown tx type: %s", txTypeStr)
}

// loadRelayerKeypair loads the Solana relayer keypair from file
// The filename is derived from the first part of the chain ID (before the colon)
// e.g., "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1" -> "solana.json"
func (tb *TxBuilder) loadRelayerKeypair() (solana.PrivateKey, error) {
	// Extract the first part of chain ID (namespace) for filename
	chainParts := strings.Split(tb.chainID, ":")
	if len(chainParts) == 0 {
		return nil, fmt.Errorf("invalid chain ID format: %s", tb.chainID)
	}
	namespace := chainParts[0]
	if namespace == "" {
		return nil, fmt.Errorf("empty namespace in chain ID: %s", tb.chainID)
	}

	// Construct file path: <nodeHome>/relayer/<namespace>.json
	keyPath := filepath.Join(tb.nodeHome, constant.RelayerSubdir, namespace+".json")

	// Read the key file (Solana keypairs are stored as JSON array of 64 bytes)
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read relayer key file %s: %w", keyPath, err)
	}

	// Parse JSON array
	var keyBytes []byte
	if err := json.Unmarshal(keyData, &keyBytes); err != nil {
		return nil, fmt.Errorf("failed to parse key file as JSON array: %w", err)
	}

	// Solana keypair is 64 bytes: 32-byte private key + 32-byte public key
	if len(keyBytes) != 64 {
		return nil, fmt.Errorf("invalid key length: expected 64 bytes, got %d", len(keyBytes))
	}

	// Extract private key (first 32 bytes)
	privateKey := solana.PrivateKey(keyBytes[:32])

	return privateKey, nil
}

// determineRecoveryID determines the recovery ID for the ECDSA signature
// It tries recovery IDs 0-3 and verifies which one recovers the correct public key
func (tb *TxBuilder) determineRecoveryID(messageHash []byte, signature []byte, expectedAddress string) (byte, error) {
	// For ECDSA signatures, recovery ID can be 0, 1, 2, or 3
	// Try each one and verify against the expected address
	for recoveryID := byte(0); recoveryID < 4; recoveryID++ {
		// Construct full signature with recovery ID
		sigWithRecovery := make([]byte, 65)
		copy(sigWithRecovery[:64], signature)
		sigWithRecovery[64] = recoveryID

		// Recover public key from signature
		pubKey, err := crypto.SigToPub(messageHash, sigWithRecovery)
		if err != nil {
			continue
		}

		// Convert public key to address (last 20 bytes of keccak256 hash)
		pubKeyBytes := crypto.FromECDSAPub(pubKey)
		addressBytes := crypto.Keccak256(pubKeyBytes[1:])[12:] // Skip 0x04 prefix, take last 20 bytes

		// Compare with expected address (convert to hex for comparison)
		expectedBytes, err := hex.DecodeString(removeHexPrefix(expectedAddress))
		if err == nil && len(expectedBytes) == 20 {
			if hex.EncodeToString(addressBytes) == hex.EncodeToString(expectedBytes) {
				return recoveryID, nil
			}
		}
	}

	return 0, fmt.Errorf("failed to determine recovery ID for signature")
}

// buildInstructionData constructs the Anchor instruction data
// Format: [8-byte discriminator] + [1-byte instruction_id] + [64-byte signature] + [1-byte recovery_id] + [additional data]
func (tb *TxBuilder) buildInstructionData(
	instructionID uint8,
	signature []byte,
	recoveryID byte,
	amount uint64,
	recipient solana.PublicKey,
	isNative bool,
	assetAddr string,
	revertMsg []byte,
) []byte {
	// Anchor discriminator is typically the first 8 bytes of sha256("global:method_name")
	// For now, we'll use a placeholder - this should match the actual gateway contract
	// Method names: withdraw_tss, withdraw_spl_token_tss, revert_withdraw, revert_withdraw_spl_token
	var discriminator []byte
	switch instructionID {
	case 1: // withdraw_tss
		discriminator = crypto.Keccak256([]byte("global:withdraw_tss"))[:8]
	case 2: // withdraw_spl_token_tss
		discriminator = crypto.Keccak256([]byte("global:withdraw_spl_token_tss"))[:8]
	case 3: // revert_withdraw
		discriminator = crypto.Keccak256([]byte("global:revert_withdraw"))[:8]
	case 4: // revert_withdraw_spl_token
		discriminator = crypto.Keccak256([]byte("global:revert_withdraw_spl_token"))[:8]
	default:
		discriminator = make([]byte, 8) // Placeholder
	}

	data := make([]byte, 0, 8+1+64+1+8+32) // discriminator + id + sig + recovery + amount + recipient/mint
	data = append(data, discriminator...)
	data = append(data, instructionID)
	data = append(data, signature...)
	data = append(data, recoveryID)

	// Add amount (8 bytes, little-endian)
	amountBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(amountBytes, amount)
	data = append(data, amountBytes...)

	// Add additional data based on instruction type
	// For revert operations (3, 4), include RevertInstructions struct
	if instructionID == 3 || instructionID == 4 {
		// RevertInstructions struct (Borsh serialized):
		// - fund_recipient: Pubkey (32 bytes)
		// - revert_msg: Vec<u8> (4-byte length prefix + data)
		data = append(data, recipient.Bytes()...) // fund_recipient = recipient
		// Append revert_msg length (4 bytes, little-endian)
		revertMsgLen := make([]byte, 4)
		binary.LittleEndian.PutUint32(revertMsgLen, uint32(len(revertMsg)))
		data = append(data, revertMsgLen...)
		// Append revert_msg data
		data = append(data, revertMsg...)
	}

	// For SPL token operations, add mint address
	if !isNative && (instructionID == 2 || instructionID == 4) {
		mintPubkey, err := solana.PublicKeyFromBase58(assetAddr)
		if err == nil {
			data = append(data, mintPubkey.Bytes()...)
		} else {
			// Try hex format
			hexBytes, _ := hex.DecodeString(removeHexPrefix(assetAddr))
			if len(hexBytes) == 32 {
				data = append(data, hexBytes...)
			}
		}
	}

	return data
}

// buildAccountsList constructs the accounts list for the instruction
// Order and flags must match the gateway contract's account structure
func (tb *TxBuilder) buildAccountsList(
	configPDA solana.PublicKey,
	vaultPDA solana.PublicKey,
	tssPDA solana.PublicKey,
	whitelistPDA solana.PublicKey,
	tokenVaultPDA solana.PublicKey,
	recipient solana.PublicKey,
	recipientTokenAccount solana.PublicKey,
	relayer solana.PublicKey,
	isNative bool,
) []*solana.AccountMeta {
	accounts := []*solana.AccountMeta{
		{PublicKey: configPDA, IsWritable: true, IsSigner: false},
		{PublicKey: vaultPDA, IsWritable: true, IsSigner: false},
		{PublicKey: tssPDA, IsWritable: true, IsSigner: false},
		{PublicKey: whitelistPDA, IsWritable: false, IsSigner: false},
		{PublicKey: recipient, IsWritable: true, IsSigner: false},
		{PublicKey: relayer, IsWritable: true, IsSigner: true}, // Relayer is fee payer and signer
	}

	if !isNative {
		// For SPL tokens, add token vault and recipient token account
		accounts = append(accounts,
			&solana.AccountMeta{PublicKey: tokenVaultPDA, IsWritable: true, IsSigner: false},
			&solana.AccountMeta{PublicKey: recipientTokenAccount, IsWritable: true, IsSigner: false},
			&solana.AccountMeta{PublicKey: solana.TokenProgramID, IsWritable: false, IsSigner: false},
		)
	} else {
		// For SOL, add system program
		accounts = append(accounts,
			&solana.AccountMeta{PublicKey: solana.SystemProgramID, IsWritable: false, IsSigner: false},
		)
	}

	return accounts
}

// buildSetComputeUnitLimitInstruction creates a SetComputeUnitLimit instruction for the Compute Budget program
// Instruction format: [1-byte instruction type (2 = SetComputeUnitLimit)] + [4-byte u32 units]
func (tb *TxBuilder) buildSetComputeUnitLimitInstruction(units uint32) solana.Instruction {
	// Compute Budget Program ID
	computeBudgetProgramID := solana.MustPublicKeyFromBase58("ComputeBudget111111111111111111111111111111")

	// Instruction data: 1 byte for instruction type + 4 bytes for units (little-endian)
	// Instruction type 2 = SetComputeUnitLimit
	data := make([]byte, 5)
	data[0] = 2 // SetComputeUnitLimit instruction type
	binary.LittleEndian.PutUint32(data[1:], units)

	return solana.NewInstruction(
		computeBudgetProgramID,
		[]*solana.AccountMeta{}, // No accounts required
		data,
	)
}
