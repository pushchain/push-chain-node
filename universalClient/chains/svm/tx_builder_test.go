package svm

import (
	"context"
	"crypto/ecdsa"
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	uetypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// ============================================================
//  Constants & helpers shared across tests
// ============================================================

// testGatewayAddress is a valid base58 Solana public key used for unit tests.
// It is NOT a real deployed gateway — only used for PDA derivations in offline tests.
const testGatewayAddress = "11111111111111111111111111111111" // system program, valid base58

func newTestBuilder(t *testing.T) *TxBuilder {
	t.Helper()
	logger := zerolog.Nop()
	builder, err := NewTxBuilder(&RPCClient{}, "solana:devnet", testGatewayAddress, "/tmp", logger)
	require.NoError(t, err)
	return builder
}

// makeTxID returns a deterministic 32-byte txID where every byte = fill
func makeTxID(fill byte) [32]byte {
	var id [32]byte
	for i := range id {
		id[i] = fill
	}
	return id
}

// makeSender returns a deterministic 20-byte sender address
func makeSender(fill byte) [20]byte {
	var s [20]byte
	for i := range s {
		s[i] = fill
	}
	return s
}

// makeToken returns a 32-byte token (Pubkey). Zero for native SOL.
func makeToken(fill byte) [32]byte {
	var t [32]byte
	for i := range t {
		t[i] = fill
	}
	return t
}

// buildMockTSSPDAData builds a raw byte slice simulating a TssPda account.
// Layout: discriminator(8) + tss_eth_address(20) + chain_id(Borsh String: 4 LE len + bytes) + nonce(u64 LE) + authority(32) + bump(1)
func buildMockTSSPDAData(tssAddr [20]byte, chainID string, nonce uint64, authority [32]byte, bump byte) []byte {
	data := make([]byte, 0, 8+20+4+len(chainID)+8+32+1)
	// discriminator (8 bytes of zeros)
	data = append(data, make([]byte, 8)...)
	// tss_eth_address (20 bytes)
	data = append(data, tssAddr[:]...)
	// chain_id Borsh String: 4-byte LE length + UTF-8 bytes
	chainIDLenBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(chainIDLenBytes, uint32(len(chainID)))
	data = append(data, chainIDLenBytes...)
	data = append(data, []byte(chainID)...)
	// nonce (u64 LE)
	nonceBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(nonceBytes, nonce)
	data = append(data, nonceBytes...)
	// authority (32 bytes)
	data = append(data, authority[:]...)
	// bump (1 byte)
	data = append(data, bump)
	return data
}

// generateTestEVMKey generates a fresh secp256k1 private key and returns
// the key, its 20-byte ETH address, and the hex-encoded address (no 0x prefix).
func generateTestEVMKey(t *testing.T) (*ecdsa.PrivateKey, [20]byte, string) {
	t.Helper()
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	pubBytes := crypto.FromECDSAPub(&key.PublicKey)
	addrBytes := crypto.Keccak256(pubBytes[1:])[12:]
	var addr [20]byte
	copy(addr[:], addrBytes)
	return key, addr, hex.EncodeToString(addrBytes)
}

// signMessageHash signs a 32-byte hash with the given EVM private key
// and returns the 64-byte signature (r||s) and the recovery ID (v).
func signMessageHash(t *testing.T, key *ecdsa.PrivateKey, hash []byte) ([]byte, byte) {
	t.Helper()
	require.Len(t, hash, 32, "hash must be 32 bytes")
	sig, err := crypto.Sign(hash, key)
	require.NoError(t, err)
	require.Len(t, sig, 65, "crypto.Sign must return 65 bytes (r||s||v)")
	return sig[:64], sig[64] // signature (r||s), recovery_id (v)
}

// buildMockExecutePayload builds a pre-encoded execute payload.
// Format: [u32 BE accounts_count][33 bytes per account (32 pubkey + 1 writable)][u32 BE ix_data_len][ix_data][u64 BE rent_fee]
func buildMockPayload(accounts []GatewayAccountMeta, ixData []byte, rentFee uint64, instructionID uint8) []byte {
	payload := make([]byte, 0, 256)
	// accounts count (u32 BE)
	countBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(countBytes, uint32(len(accounts)))
	payload = append(payload, countBytes...)
	// each account: 32 pubkey + 1 writable
	for _, acc := range accounts {
		payload = append(payload, acc.Pubkey[:]...)
		if acc.IsWritable {
			payload = append(payload, 1)
		} else {
			payload = append(payload, 0)
		}
	}
	// ix_data length (u32 BE)
	ixLenBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(ixLenBytes, uint32(len(ixData)))
	payload = append(payload, ixLenBytes...)
	// ix_data
	payload = append(payload, ixData...)
	// rent_fee (u64 BE)
	rentFeeBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(rentFeeBytes, rentFee)
	payload = append(payload, rentFeeBytes...)
	// instruction_id (u8)
	payload = append(payload, instructionID)
	return payload
}

// buildMockExecutePayload is a convenience wrapper for execute payloads (instruction_id=2)
func buildMockExecutePayload(accounts []GatewayAccountMeta, ixData []byte, rentFee uint64) []byte {
	return buildMockPayload(accounts, ixData, rentFee, 2)
}

// buildMockWithdrawPayload builds a withdraw payload (instruction_id=1, no accounts/ixData)
func buildMockWithdrawPayload() []byte {
	return buildMockPayload(nil, nil, 0, 1)
}

// ============================================================
//  TestNewTxBuilder
// ============================================================

func TestNewTxBuilder(t *testing.T) {
	logger := zerolog.Nop()

	tests := []struct {
		name           string
		rpcClient      *RPCClient
		chainID        string
		gatewayAddress string
		expectError    bool
		errorContains  string
	}{
		{
			name:           "valid inputs",
			rpcClient:      &RPCClient{},
			chainID:        "solana:devnet",
			gatewayAddress: testGatewayAddress,
			expectError:    false,
		},
		{
			name:           "nil rpcClient",
			rpcClient:      nil,
			chainID:        "solana:devnet",
			gatewayAddress: testGatewayAddress,
			expectError:    true,
			errorContains:  "rpcClient is required",
		},
		{
			name:           "empty chainID",
			rpcClient:      &RPCClient{},
			chainID:        "",
			gatewayAddress: testGatewayAddress,
			expectError:    true,
			errorContains:  "chainID is required",
		},
		{
			name:           "empty gatewayAddress",
			rpcClient:      &RPCClient{},
			chainID:        "solana:devnet",
			gatewayAddress: "",
			expectError:    true,
			errorContains:  "gatewayAddress is required",
		},
		{
			name:           "invalid gatewayAddress",
			rpcClient:      &RPCClient{},
			chainID:        "solana:devnet",
			gatewayAddress: "not-a-valid-base58",
			expectError:    true,
			errorContains:  "invalid gateway address",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder, err := NewTxBuilder(tt.rpcClient, tt.chainID, tt.gatewayAddress, "/tmp", logger)
			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				assert.Nil(t, builder)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, builder)
				assert.Equal(t, tt.chainID, builder.chainID)
			}
		})
	}
}

// ============================================================
//  TestDefaultComputeUnitLimit
// ============================================================

func TestDefaultComputeUnitLimit(t *testing.T) {
	assert.Equal(t, uint32(200000), uint32(DefaultComputeUnitLimit))
}

// ============================================================
//  TestDeriveTSSPDA — seed must be "tsspda"
// ============================================================

func TestDeriveTSSPDA(t *testing.T) {
	builder := newTestBuilder(t)

	pda, err := builder.deriveTSSPDA()
	require.NoError(t, err)
	assert.False(t, pda.IsZero(), "TSS PDA should be non-zero")

	// Verify it matches FindProgramAddress with seed "tsspda"
	expected, _, err := solana.FindProgramAddress([][]byte{[]byte("tsspda")}, builder.gatewayAddress)
	require.NoError(t, err)
	assert.Equal(t, expected, pda)

	// Verify it does NOT match the old buggy seed "tss"
	oldPDA, _, err := solana.FindProgramAddress([][]byte{[]byte("tss")}, builder.gatewayAddress)
	require.NoError(t, err)
	assert.NotEqual(t, oldPDA, pda, "TSS PDA must NOT use old seed 'tss'")
}

// ============================================================
//  TestFetchTSSNonce — Borsh String parsing
// ============================================================

func TestFetchTSSNonce(t *testing.T) {
	t.Run("parses valid TssPda with short chain_id", func(t *testing.T) {
		chainIDStr := "devnet"
		data := buildMockTSSPDAData([20]byte{}, chainIDStr, 42, [32]byte{}, 255)

		nonce, chainID, err := parseTSSPDAData(data)
		require.NoError(t, err)
		assert.Equal(t, uint64(42), nonce)
		assert.Equal(t, chainIDStr, chainID)
	})

	t.Run("parses valid TssPda with mainnet cluster pubkey", func(t *testing.T) {
		chainIDStr := "5eykt4UsFv8P8NJdTREpY1vzqKqZKvdpKuc147dw2N9d"
		data := buildMockTSSPDAData([20]byte{}, chainIDStr, 99, [32]byte{}, 1)

		nonce, chainID, err := parseTSSPDAData(data)
		require.NoError(t, err)
		assert.Equal(t, uint64(99), nonce)
		assert.Equal(t, chainIDStr, chainID)
	})

	t.Run("rejects data too short for header", func(t *testing.T) {
		_, _, err := parseTSSPDAData(make([]byte, 31)) // less than 32
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "too short")
	})

	t.Run("rejects data too short for chain_id + nonce", func(t *testing.T) {
		// Build header with chain_id_len = 100, but only provide 40 total bytes
		data := make([]byte, 40)
		binary.LittleEndian.PutUint32(data[28:32], 100) // chain_id_len = 100
		_, _, err := parseTSSPDAData(data)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "too short")
	})

	t.Run("nonce at correct offset after variable-length chain_id", func(t *testing.T) {
		// Two different chain_id lengths, same nonce — verify nonce offset is dynamic
		for _, cid := range []string{"a", "abcdefghij"} {
			data := buildMockTSSPDAData([20]byte{}, cid, 777, [32]byte{}, 0)
			nonce, chainID, err := parseTSSPDAData(data)
			require.NoError(t, err, "chain_id=%q", cid)
			assert.Equal(t, uint64(777), nonce)
			assert.Equal(t, cid, chainID)
		}
	})
}

// parseTSSPDAData is the extraction of fetchTSSNonce's parsing logic for unit testing
// without requiring an RPC call. This mirrors the parsing in fetchTSSNonce.
func parseTSSPDAData(accountData []byte) (uint64, string, error) {
	if len(accountData) < 32 {
		return 0, "", fmt.Errorf("invalid TSS PDA account data: too short (%d bytes)", len(accountData))
	}
	chainIDLen := binary.LittleEndian.Uint32(accountData[28:32])
	requiredLen := 32 + int(chainIDLen) + 8
	if len(accountData) < requiredLen {
		return 0, "", fmt.Errorf("invalid TSS PDA account data: too short for chain_id length %d (%d bytes)", chainIDLen, len(accountData))
	}
	chainID := string(accountData[32 : 32+chainIDLen])
	nonceOffset := 32 + int(chainIDLen)
	nonce := binary.LittleEndian.Uint64(accountData[nonceOffset : nonceOffset+8])
	return nonce, chainID, nil
}

// ============================================================
//  TestDetermineInstructionID
// ============================================================

func TestDetermineInstructionID(t *testing.T) {
	builder := newTestBuilder(t)

	tests := []struct {
		name     string
		txType   uetypes.TxType
		isNative bool
		expected uint8
		wantErr  bool
	}{
		{"FUNDS native → 1 (withdraw)", uetypes.TxType_FUNDS, true, 1, false},
		{"FUNDS SPL → 1 (withdraw)", uetypes.TxType_FUNDS, false, 1, false},
		{"FUNDS_AND_PAYLOAD → 2 (execute)", uetypes.TxType_FUNDS_AND_PAYLOAD, true, 2, false},
		{"GAS_AND_PAYLOAD → 2 (execute)", uetypes.TxType_GAS_AND_PAYLOAD, false, 2, false},
		{"INBOUND_REVERT native → 3", uetypes.TxType_INBOUND_REVERT, true, 3, false},
		{"INBOUND_REVERT SPL → 4", uetypes.TxType_INBOUND_REVERT, false, 4, false},
		{"UNSPECIFIED → error", uetypes.TxType_UNSPECIFIED_TX, true, 0, true},
		{"GAS → error", uetypes.TxType_GAS, true, 0, true},
		{"PAYLOAD → error", uetypes.TxType_PAYLOAD, true, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, err := builder.determineInstructionID(tt.txType, tt.isNative)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, id)
			}
		})
	}
}

// ============================================================
//  TestAnchorDiscriminator — SHA256, not Keccak
// ============================================================

func TestAnchorDiscriminator(t *testing.T) {
	tests := []struct {
		methodName string
	}{
		{"withdraw_and_execute"},
		{"revert_universal_tx"},
		{"revert_universal_tx_token"},
	}

	for _, tt := range tests {
		t.Run(tt.methodName, func(t *testing.T) {
			disc := anchorDiscriminator(tt.methodName)
			assert.Len(t, disc, 8, "discriminator must be 8 bytes")

			// Verify it matches sha256("global:<method>")[:8]
			h := sha256.Sum256([]byte("global:" + tt.methodName))
			assert.Equal(t, h[:8], disc)

			// Verify it does NOT match keccak256 (the old buggy approach)
			keccak := crypto.Keccak256([]byte("global:" + tt.methodName))[:8]
			assert.NotEqual(t, keccak, disc, "discriminator must use SHA256, not Keccak256")
		})
	}
}

// ============================================================
//  TestConstructTSSMessage — message format
// ============================================================

func TestConstructTSSMessage(t *testing.T) {
	builder := newTestBuilder(t)

	txID := makeTxID(0xAA)
	utxID := makeTxID(0xBB)
	sender := makeSender(0xCC)
	token := makeToken(0x00) // native SOL = zero
	target := makeTxID(0xDD)

	t.Run("withdraw (id=1) message format", func(t *testing.T) {
		hash, err := builder.constructTSSMessage(
			1, "devnet", 0, 1000000,
			txID, utxID, sender, token,
			0, // gasFee
			target, nil, nil, 0,
			[32]byte{}, [32]byte{},
		)
		require.NoError(t, err)
		assert.Len(t, hash, 32, "message hash must be 32 bytes (keccak256)")

		// Reconstruct expected message manually
		msg := []byte("PUSH_CHAIN_SVM")
		msg = append(msg, 1) // instruction_id
		msg = append(msg, []byte("devnet")...)
		nonceBE := make([]byte, 8)
		binary.BigEndian.PutUint64(nonceBE, 0)
		msg = append(msg, nonceBE...)
		amountBE := make([]byte, 8)
		binary.BigEndian.PutUint64(amountBE, 1000000)
		msg = append(msg, amountBE...)
		// additional: tx_id, utx_id, sender, token, gas_fee, target
		msg = append(msg, txID[:]...)
		msg = append(msg, utxID[:]...)
		msg = append(msg, sender[:]...)
		msg = append(msg, token[:]...)
		gasBE := make([]byte, 8)
		msg = append(msg, gasBE...)
		msg = append(msg, target[:]...)

		expected := crypto.Keccak256(msg)
		assert.Equal(t, expected, hash, "withdraw message hash mismatch")
	})

	t.Run("execute (id=2) message format", func(t *testing.T) {
		accs := []GatewayAccountMeta{
			{Pubkey: makeTxID(0x11), IsWritable: true},
			{Pubkey: makeTxID(0x22), IsWritable: false},
		}
		ixData := []byte{0xDE, 0xAD, 0xBE, 0xEF}

		hash, err := builder.constructTSSMessage(
			2, "devnet", 5, 2000000,
			txID, utxID, sender, token,
			100,                       // gasFee
			target, accs, ixData, 500, // rentFee
			[32]byte{}, [32]byte{},
		)
		require.NoError(t, err)
		assert.Len(t, hash, 32)

		// Rebuild expected
		msg := []byte("PUSH_CHAIN_SVM")
		msg = append(msg, 2)
		msg = append(msg, []byte("devnet")...)
		nonceBE := make([]byte, 8)
		binary.BigEndian.PutUint64(nonceBE, 5)
		msg = append(msg, nonceBE...)
		amountBE := make([]byte, 8)
		binary.BigEndian.PutUint64(amountBE, 2000000)
		msg = append(msg, amountBE...)
		msg = append(msg, txID[:]...)
		msg = append(msg, utxID[:]...)
		msg = append(msg, sender[:]...)
		msg = append(msg, token[:]...)
		gasBE := make([]byte, 8)
		binary.BigEndian.PutUint64(gasBE, 100)
		msg = append(msg, gasBE...)
		msg = append(msg, target[:]...)
		// accounts_buf
		accCount := make([]byte, 4)
		binary.BigEndian.PutUint32(accCount, 2)
		msg = append(msg, accCount...)
		acc1Key := makeTxID(0x11)
		msg = append(msg, acc1Key[:]...)
		msg = append(msg, 1) // writable
		acc2Key := makeTxID(0x22)
		msg = append(msg, acc2Key[:]...)
		msg = append(msg, 0) // not writable
		// ix_data_buf
		ixLenBE := make([]byte, 4)
		binary.BigEndian.PutUint32(ixLenBE, 4)
		msg = append(msg, ixLenBE...)
		msg = append(msg, 0xDE, 0xAD, 0xBE, 0xEF)
		// rent_fee
		rentBE := make([]byte, 8)
		binary.BigEndian.PutUint64(rentBE, 500)
		msg = append(msg, rentBE...)

		expected := crypto.Keccak256(msg)
		assert.Equal(t, expected, hash, "execute message hash mismatch")
	})

	t.Run("revert SOL (id=3) message format", func(t *testing.T) {
		revertRecipient := makeTxID(0xEE)
		hash, err := builder.constructTSSMessage(
			3, "devnet", 10, 500000,
			txID, utxID, sender, token,
			0, [32]byte{}, nil, nil, 0,
			revertRecipient, [32]byte{},
		)
		require.NoError(t, err)

		msg := []byte("PUSH_CHAIN_SVM")
		msg = append(msg, 3)
		msg = append(msg, []byte("devnet")...)
		nonceBE := make([]byte, 8)
		binary.BigEndian.PutUint64(nonceBE, 10)
		msg = append(msg, nonceBE...)
		amountBE := make([]byte, 8)
		binary.BigEndian.PutUint64(amountBE, 500000)
		msg = append(msg, amountBE...)
		// additional: utx_id, tx_id, recipient, gas_fee
		msg = append(msg, utxID[:]...)
		msg = append(msg, txID[:]...)
		msg = append(msg, revertRecipient[:]...)
		gasBE := make([]byte, 8)
		msg = append(msg, gasBE...)

		expected := crypto.Keccak256(msg)
		assert.Equal(t, expected, hash, "revert SOL message hash mismatch")
	})

	t.Run("revert SPL (id=4) message format", func(t *testing.T) {
		revertRecipient := makeTxID(0xEE)
		revertMint := makeTxID(0xFF)
		hash, err := builder.constructTSSMessage(
			4, "devnet", 20, 750000,
			txID, utxID, sender, token,
			0, [32]byte{}, nil, nil, 0,
			revertRecipient, revertMint,
		)
		require.NoError(t, err)

		msg := []byte("PUSH_CHAIN_SVM")
		msg = append(msg, 4)
		msg = append(msg, []byte("devnet")...)
		nonceBE := make([]byte, 8)
		binary.BigEndian.PutUint64(nonceBE, 20)
		msg = append(msg, nonceBE...)
		amountBE := make([]byte, 8)
		binary.BigEndian.PutUint64(amountBE, 750000)
		msg = append(msg, amountBE...)
		// additional: utx_id, tx_id, mint, recipient, gas_fee
		msg = append(msg, utxID[:]...)
		msg = append(msg, txID[:]...)
		msg = append(msg, revertMint[:]...)
		msg = append(msg, revertRecipient[:]...)
		gasBE := make([]byte, 8)
		msg = append(msg, gasBE...)

		expected := crypto.Keccak256(msg)
		assert.Equal(t, expected, hash, "revert SPL message hash mismatch")
	})

	t.Run("chain_id bytes go directly into message (no length prefix)", func(t *testing.T) {
		// Verify that the chain_id in the message is raw UTF-8, not Borsh-encoded
		chainID := "test_chain"
		hash1, err := builder.constructTSSMessage(
			1, chainID, 0, 0,
			[32]byte{}, [32]byte{}, [20]byte{}, [32]byte{},
			0, [32]byte{}, nil, nil, 0,
			[32]byte{}, [32]byte{},
		)
		require.NoError(t, err)

		// Build expected manually — chain_id as raw bytes
		msg := []byte("PUSH_CHAIN_SVM")
		msg = append(msg, 1)
		msg = append(msg, []byte(chainID)...)  // raw UTF-8, no 4-byte length prefix
		msg = append(msg, make([]byte, 8)...)  // nonce BE
		msg = append(msg, make([]byte, 8)...)  // amount BE
		msg = append(msg, make([]byte, 32)...) // tx_id
		msg = append(msg, make([]byte, 32)...) // utx_id
		msg = append(msg, make([]byte, 20)...) // sender
		msg = append(msg, make([]byte, 32)...) // token
		msg = append(msg, make([]byte, 8)...)  // gas_fee
		msg = append(msg, make([]byte, 32)...) // target

		expected := crypto.Keccak256(msg)
		assert.Equal(t, expected, hash1)
	})

	t.Run("unknown instruction ID returns error", func(t *testing.T) {
		_, err := builder.constructTSSMessage(
			99, "devnet", 0, 0,
			[32]byte{}, [32]byte{}, [20]byte{}, [32]byte{},
			0, [32]byte{}, nil, nil, 0,
			[32]byte{}, [32]byte{},
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unknown instruction ID")
	})
}

// ============================================================
//  TestConstructTSSMessage_HashIsKeccak256
// ============================================================

func TestConstructTSSMessage_HashIsKeccak256(t *testing.T) {
	builder := newTestBuilder(t)

	// Construct a simple withdraw message and verify the hash algo
	hash, err := builder.constructTSSMessage(
		1, "x", 0, 0,
		[32]byte{}, [32]byte{}, [20]byte{}, [32]byte{},
		0, [32]byte{}, nil, nil, 0,
		[32]byte{}, [32]byte{},
	)
	require.NoError(t, err)

	// Build the raw message
	msg := []byte("PUSH_CHAIN_SVM")
	msg = append(msg, 1)
	msg = append(msg, 'x')
	msg = append(msg, make([]byte, 8+8+32+32+20+32+8+32)...)

	// Must be keccak256 (not sha256)
	keccakHash := crypto.Keccak256(msg)
	sha256Hash := sha256.Sum256(msg)
	assert.Equal(t, keccakHash, hash, "TSS message must be hashed with keccak256")
	assert.NotEqual(t, sha256Hash[:], hash, "TSS message must NOT be hashed with SHA256")
}

// ============================================================
//  TestDecodePayload
// ============================================================

func TestDecodePayload(t *testing.T) {
	t.Run("decodes valid execute payload with 2 accounts", func(t *testing.T) {
		expectedAccounts := []GatewayAccountMeta{
			{Pubkey: makeTxID(0x11), IsWritable: true},
			{Pubkey: makeTxID(0x22), IsWritable: false},
		}
		expectedIxData := []byte{0xAA, 0xBB, 0xCC}
		expectedRentFee := uint64(12345)

		payload := buildMockPayload(expectedAccounts, expectedIxData, expectedRentFee, 2)
		accounts, ixData, rentFee, instructionID, err := decodePayload(payload)

		require.NoError(t, err)
		assert.Equal(t, uint8(2), instructionID)
		assert.Len(t, accounts, 2)
		assert.Equal(t, expectedAccounts[0].Pubkey, accounts[0].Pubkey)
		assert.True(t, accounts[0].IsWritable)
		assert.Equal(t, expectedAccounts[1].Pubkey, accounts[1].Pubkey)
		assert.False(t, accounts[1].IsWritable)
		assert.Equal(t, expectedIxData, ixData)
		assert.Equal(t, expectedRentFee, rentFee)
	})

	t.Run("decodes withdraw payload (0 accounts)", func(t *testing.T) {
		payload := buildMockWithdrawPayload()
		accounts, ixData, rentFee, instructionID, err := decodePayload(payload)
		require.NoError(t, err)
		assert.Equal(t, uint8(1), instructionID)
		assert.Len(t, accounts, 0)
		assert.Len(t, ixData, 0)
		assert.Equal(t, uint64(0), rentFee)
	})

	t.Run("decodes payload with empty ix_data", func(t *testing.T) {
		accs := []GatewayAccountMeta{{Pubkey: makeTxID(0x33), IsWritable: true}}
		payload := buildMockPayload(accs, nil, 999, 2)
		accounts, ixData, rentFee, instructionID, err := decodePayload(payload)
		require.NoError(t, err)
		assert.Equal(t, uint8(2), instructionID)
		assert.Len(t, accounts, 1)
		assert.Len(t, ixData, 0)
		assert.Equal(t, uint64(999), rentFee)
	})

	t.Run("rejects too-short payload", func(t *testing.T) {
		_, _, _, _, err := decodePayload([]byte{0, 0})
		assert.Error(t, err)
	})

	t.Run("rejects truncated account data", func(t *testing.T) {
		// Says 1 account but only provides 10 bytes (need 33)
		payload := make([]byte, 4+10)
		binary.BigEndian.PutUint32(payload[0:4], 1)
		_, _, _, _, err := decodePayload(payload)
		assert.Error(t, err)
	})

	t.Run("rejects truncated ix_data", func(t *testing.T) {
		// 0 accounts, says ix_data len=100 but only 4 bytes remain
		payload := make([]byte, 4+4+4)
		binary.BigEndian.PutUint32(payload[0:4], 0)   // 0 accounts
		binary.BigEndian.PutUint32(payload[4:8], 100) // ix_data_len = 100
		_, _, _, _, err := decodePayload(payload)
		assert.Error(t, err)
	})
}

// ============================================================
//  TestAccountsToWritableFlags
// ============================================================

func TestAccountsToWritableFlags(t *testing.T) {
	t.Run("empty accounts → empty flags", func(t *testing.T) {
		flags := accountsToWritableFlags(nil)
		assert.Empty(t, flags)
	})

	t.Run("single writable account → 0x80", func(t *testing.T) {
		accs := []GatewayAccountMeta{{IsWritable: true}}
		flags := accountsToWritableFlags(accs)
		assert.Equal(t, []byte{0x80}, flags) // bit 7 set (MSB-first)
	})

	t.Run("single non-writable account → 0x00", func(t *testing.T) {
		accs := []GatewayAccountMeta{{IsWritable: false}}
		flags := accountsToWritableFlags(accs)
		assert.Equal(t, []byte{0x00}, flags)
	})

	t.Run("8 accounts all writable → 0xFF", func(t *testing.T) {
		accs := make([]GatewayAccountMeta, 8)
		for i := range accs {
			accs[i].IsWritable = true
		}
		flags := accountsToWritableFlags(accs)
		assert.Equal(t, []byte{0xFF}, flags)
	})

	t.Run("9 accounts → 2 bytes", func(t *testing.T) {
		accs := make([]GatewayAccountMeta, 9)
		accs[0].IsWritable = true // byte 0, bit 7 → 0x80
		accs[8].IsWritable = true // byte 1, bit 7 → 0x80
		flags := accountsToWritableFlags(accs)
		assert.Len(t, flags, 2)
		assert.Equal(t, byte(0x80), flags[0])
		assert.Equal(t, byte(0x80), flags[1])
	})

	t.Run("MSB-first bit ordering matches gateway contract", func(t *testing.T) {
		// accounts: [W, R, W, R, R, R, R, R] → bit pattern: 10100000 = 0xA0
		accs := make([]GatewayAccountMeta, 8)
		accs[0].IsWritable = true
		accs[2].IsWritable = true
		flags := accountsToWritableFlags(accs)
		assert.Equal(t, []byte{0xA0}, flags)
	})
}

// ============================================================
//  TestBuildWithdrawAndExecuteData — Borsh layout
// ============================================================

func TestBuildWithdrawAndExecuteData(t *testing.T) {
	builder := newTestBuilder(t)
	txID := makeTxID(0x01)
	utxID := makeTxID(0x02)
	sender := makeSender(0x03)
	sig := make([]byte, 64)
	for i := range sig {
		sig[i] = byte(i)
	}
	msgHash := make([]byte, 32)
	for i := range msgHash {
		msgHash[i] = byte(0xFF - i)
	}

	t.Run("withdraw (id=1) data layout", func(t *testing.T) {
		data := builder.buildWithdrawAndExecuteData(
			1, txID, utxID, 1000000, sender,
			[]byte{}, []byte{}, // empty writable_flags and ix_data for withdraw
			0, 0, // gasFee, rentFee
			sig, 2, msgHash, 42,
		)

		// Check discriminator
		expectedDisc := anchorDiscriminator("withdraw_and_execute")
		assert.Equal(t, expectedDisc, data[:8], "discriminator")

		// Check instruction_id
		assert.Equal(t, byte(1), data[8], "instruction_id")

		// Check tx_id
		assert.Equal(t, txID[:], data[9:41], "tx_id")

		// Check universal_tx_id
		assert.Equal(t, utxID[:], data[41:73], "universal_tx_id")

		// Check amount (u64 LE)
		assert.Equal(t, uint64(1000000), binary.LittleEndian.Uint64(data[73:81]), "amount")

		// Check sender
		assert.Equal(t, sender[:], data[81:101], "sender")

		// Check writable_flags Vec<u8>: len=0
		assert.Equal(t, uint32(0), binary.LittleEndian.Uint32(data[101:105]), "writable_flags len")

		// Check ix_data Vec<u8>: len=0
		assert.Equal(t, uint32(0), binary.LittleEndian.Uint32(data[105:109]), "ix_data len")

		// Check gas_fee (u64 LE)
		assert.Equal(t, uint64(0), binary.LittleEndian.Uint64(data[109:117]), "gas_fee")

		// Check rent_fee (u64 LE)
		assert.Equal(t, uint64(0), binary.LittleEndian.Uint64(data[117:125]), "rent_fee")

		// Check signature
		assert.Equal(t, sig, data[125:189], "signature")

		// Check recovery_id
		assert.Equal(t, byte(2), data[189], "recovery_id")

		// Check message_hash
		assert.Equal(t, msgHash, data[190:222], "message_hash")

		// Check nonce (u64 LE)
		assert.Equal(t, uint64(42), binary.LittleEndian.Uint64(data[222:230]), "nonce")

		// Total length
		assert.Len(t, data, 230)
	})

	t.Run("execute (id=2) with accounts and ix_data", func(t *testing.T) {
		wf := []byte{0xA0}
		ixData := []byte{0xDE, 0xAD}

		data := builder.buildWithdrawAndExecuteData(
			2, txID, utxID, 500, sender,
			wf, ixData,
			100, 50, // gasFee, rentFee
			sig, 0, msgHash, 7,
		)

		// Discriminator should be same function
		expectedDisc := anchorDiscriminator("withdraw_and_execute")
		assert.Equal(t, expectedDisc, data[:8])
		assert.Equal(t, byte(2), data[8])

		// Verify writable_flags Vec has length=1
		offset := 101
		assert.Equal(t, uint32(1), binary.LittleEndian.Uint32(data[offset:offset+4]))
		offset += 4
		assert.Equal(t, byte(0xA0), data[offset])
		offset += 1

		// Verify ix_data Vec has length=2
		assert.Equal(t, uint32(2), binary.LittleEndian.Uint32(data[offset:offset+4]))
		offset += 4
		assert.Equal(t, []byte{0xDE, 0xAD}, data[offset:offset+2])
		offset += 2

		// gas_fee
		assert.Equal(t, uint64(100), binary.LittleEndian.Uint64(data[offset:offset+8]))
		offset += 8

		// rent_fee
		assert.Equal(t, uint64(50), binary.LittleEndian.Uint64(data[offset:offset+8]))
		offset += 8

		// signature + recovery_id + message_hash + nonce
		assert.Equal(t, sig, data[offset:offset+64])
		offset += 64
		assert.Equal(t, byte(0), data[offset])
		offset += 1
		assert.Equal(t, msgHash, data[offset:offset+32])
		offset += 32
		assert.Equal(t, uint64(7), binary.LittleEndian.Uint64(data[offset:offset+8]))
	})
}

// ============================================================
//  TestBuildRevertData
// ============================================================

func TestBuildRevertData(t *testing.T) {
	builder := newTestBuilder(t)
	txID := makeTxID(0x01)
	utxID := makeTxID(0x02)
	recipient := solana.MustPublicKeyFromBase58(testGatewayAddress)
	revertMsg := []byte("revert me")
	sig := make([]byte, 64)
	msgHash := make([]byte, 32)

	t.Run("revert SOL (id=3) uses correct discriminator", func(t *testing.T) {
		data := builder.buildRevertData(3, txID, utxID, 1000, recipient, revertMsg, 0, sig, 1, msgHash, 5)

		expectedDisc := anchorDiscriminator("revert_universal_tx")
		assert.Equal(t, expectedDisc, data[:8])
		assert.Equal(t, txID[:], data[8:40])
		assert.Equal(t, utxID[:], data[40:72])
		assert.Equal(t, uint64(1000), binary.LittleEndian.Uint64(data[72:80]))
		// revert_instruction: fund_recipient(32) + revert_msg Vec(4+N)
		assert.Equal(t, recipient.Bytes(), data[80:112])
		assert.Equal(t, uint32(len(revertMsg)), binary.LittleEndian.Uint32(data[112:116]))
		assert.Equal(t, revertMsg, data[116:116+len(revertMsg)])
	})

	t.Run("revert SPL (id=4) uses correct discriminator", func(t *testing.T) {
		data := builder.buildRevertData(4, txID, utxID, 2000, recipient, nil, 0, sig, 0, msgHash, 10)

		expectedDisc := anchorDiscriminator("revert_universal_tx_token")
		assert.Equal(t, expectedDisc, data[:8])
		// revert_msg should be empty Vec (len=0)
		offset := 112 // after tx_id(32) + utx_id(32) + amount(8) + fund_recipient(32)
		assert.Equal(t, uint32(0), binary.LittleEndian.Uint32(data[offset:offset+4]))
	})
}

// ============================================================
//  TestBuildWithdrawAndExecuteAccounts — accounts list
// ============================================================

func TestBuildWithdrawAndExecuteAccounts(t *testing.T) {
	builder := newTestBuilder(t)

	caller := solana.NewWallet().PublicKey()
	config := solana.NewWallet().PublicKey()
	vault := solana.NewWallet().PublicKey()
	cea := solana.NewWallet().PublicKey()
	tss := solana.NewWallet().PublicKey()
	executed := solana.NewWallet().PublicKey()
	recipient := solana.NewWallet().PublicKey()

	t.Run("withdraw SOL (id=1) has correct account order", func(t *testing.T) {
		accounts := builder.buildWithdrawAndExecuteAccounts(
			caller, config, vault, cea, tss, executed,
			solana.SystemProgramID, // destination_program = system for withdraw
			true, 1,                // isNative, instructionID
			recipient, solana.PublicKey{}, // recipient, mint (unused for native)
			nil, // no execute accounts
		)

		// First 8 required accounts
		assert.Equal(t, caller, accounts[0].PublicKey, "caller")
		assert.True(t, accounts[0].IsSigner)
		assert.True(t, accounts[0].IsWritable)

		assert.Equal(t, config, accounts[1].PublicKey, "config")
		assert.False(t, accounts[1].IsWritable, "config is read-only")

		assert.Equal(t, vault, accounts[2].PublicKey, "vault_sol")
		assert.True(t, accounts[2].IsWritable)

		assert.Equal(t, cea, accounts[3].PublicKey, "cea_authority")
		assert.True(t, accounts[3].IsWritable)

		assert.Equal(t, tss, accounts[4].PublicKey, "tss_pda")
		assert.True(t, accounts[4].IsWritable)

		assert.Equal(t, executed, accounts[5].PublicKey, "executed_tx")
		assert.True(t, accounts[5].IsWritable)

		assert.Equal(t, solana.SystemProgramID, accounts[6].PublicKey, "system_program")
		assert.False(t, accounts[6].IsWritable)

		assert.Equal(t, solana.SystemProgramID, accounts[7].PublicKey, "destination_program")
		assert.False(t, accounts[7].IsWritable)

		// For native SOL withdraw: recipient is real, rest are gateway sentinels
		assert.Equal(t, recipient, accounts[8].PublicKey, "recipient (withdraw SOL)")
		assert.True(t, accounts[8].IsWritable)

		// Remaining optional accounts should be gateway address (None sentinels)
		for i := 9; i <= 15; i++ {
			assert.Equal(t, builder.gatewayAddress, accounts[i].PublicKey,
				"optional account %d should be gateway sentinel", i)
		}

		assert.Len(t, accounts, 16, "total accounts for SOL withdraw")
	})

	t.Run("execute (id=2) appends remaining_accounts", func(t *testing.T) {
		execAccounts := []GatewayAccountMeta{
			{Pubkey: makeTxID(0xAA), IsWritable: true},
			{Pubkey: makeTxID(0xBB), IsWritable: false},
		}

		accounts := builder.buildWithdrawAndExecuteAccounts(
			caller, config, vault, cea, tss, executed,
			recipient, // destination_program = target program
			true, 2,   // isNative, instructionID=execute
			solana.PublicKey{}, solana.PublicKey{},
			execAccounts,
		)

		// For execute: recipient should be gateway sentinel (None)
		assert.Equal(t, builder.gatewayAddress, accounts[8].PublicKey, "recipient should be None for execute")

		// remaining_accounts appended at the end
		totalRequired := 16 // 8 required + 8 optional
		assert.Len(t, accounts, totalRequired+2)

		// Check remaining_accounts
		expectedPk1 := makeTxID(0xAA)
		acc1 := accounts[totalRequired]
		assert.Equal(t, solana.PublicKeyFromBytes(expectedPk1[:]), acc1.PublicKey)
		assert.True(t, acc1.IsWritable)
		assert.False(t, acc1.IsSigner)

		expectedPk2 := makeTxID(0xBB)
		acc2 := accounts[totalRequired+1]
		assert.Equal(t, solana.PublicKeyFromBytes(expectedPk2[:]), acc2.PublicKey)
		assert.False(t, acc2.IsWritable)
	})
}

// ============================================================
//  TestBuildRevertAccounts
// ============================================================

func TestBuildRevertSOLAccounts(t *testing.T) {
	builder := newTestBuilder(t)

	config := solana.NewWallet().PublicKey()
	vault := solana.NewWallet().PublicKey()
	tss := solana.NewWallet().PublicKey()
	recipient := solana.NewWallet().PublicKey()
	executed := solana.NewWallet().PublicKey()
	caller := solana.NewWallet().PublicKey()

	accounts := builder.buildRevertSOLAccounts(config, vault, tss, recipient, executed, caller)

	assert.Len(t, accounts, 7)
	assert.Equal(t, config, accounts[0].PublicKey, "config")
	assert.Equal(t, vault, accounts[1].PublicKey, "vault")
	assert.True(t, accounts[1].IsWritable)
	assert.Equal(t, tss, accounts[2].PublicKey, "tss_pda")
	assert.True(t, accounts[2].IsWritable)
	assert.Equal(t, recipient, accounts[3].PublicKey, "recipient")
	assert.True(t, accounts[3].IsWritable)
	assert.Equal(t, executed, accounts[4].PublicKey, "executed_tx")
	assert.True(t, accounts[4].IsWritable)
	assert.Equal(t, caller, accounts[5].PublicKey, "caller")
	assert.True(t, accounts[5].IsSigner)
	assert.Equal(t, solana.SystemProgramID, accounts[6].PublicKey, "system_program")
}

func TestBuildRevertSPLAccounts(t *testing.T) {
	builder := newTestBuilder(t)

	config := solana.NewWallet().PublicKey()
	vault := solana.NewWallet().PublicKey()
	tokenVault := solana.NewWallet().PublicKey()
	tss := solana.NewWallet().PublicKey()
	recipientATA := solana.NewWallet().PublicKey()
	tokenMint := solana.NewWallet().PublicKey()
	executed := solana.NewWallet().PublicKey()
	caller := solana.NewWallet().PublicKey()

	accounts := builder.buildRevertSPLAccounts(config, vault, tokenVault, tss, recipientATA, tokenMint, executed, caller)

	assert.Len(t, accounts, 11)
	assert.Equal(t, config, accounts[0].PublicKey, "config")
	assert.Equal(t, vault, accounts[1].PublicKey, "vault")
	assert.Equal(t, tokenVault, accounts[2].PublicKey, "token_vault")
	assert.True(t, accounts[2].IsWritable)
	assert.Equal(t, tss, accounts[3].PublicKey, "tss_pda")
	assert.Equal(t, recipientATA, accounts[4].PublicKey, "recipient_token_account")
	assert.Equal(t, tokenMint, accounts[5].PublicKey, "token_mint")
	assert.Equal(t, executed, accounts[6].PublicKey, "executed_tx")
	assert.Equal(t, caller, accounts[7].PublicKey, "caller")
	assert.True(t, accounts[7].IsSigner)
	assert.Equal(t, vault, accounts[8].PublicKey, "vault_sol (same as vault)")
	assert.Equal(t, solana.TokenProgramID, accounts[9].PublicKey, "token_program")
	assert.Equal(t, solana.SystemProgramID, accounts[10].PublicKey, "system_program")
}

// ============================================================
//  TestRemoveHexPrefix
// ============================================================

func TestRemoveHexPrefix(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"0xabcdef", "abcdef"},
		{"0XABCDEF", "0XABCDEF"}, // only lowercase 0x
		{"abcdef", "abcdef"},
		{"", ""},
		{"0x", ""},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expected, removeHexPrefix(tt.input))
	}
}

// ============================================================
//  TestParseTxType
// ============================================================

func TestParseTxType(t *testing.T) {
	tests := []struct {
		input    string
		expected uetypes.TxType
		wantErr  bool
	}{
		{"GAS", uetypes.TxType_GAS, false},
		{"FUNDS", uetypes.TxType_FUNDS, false},
		{"PAYLOAD", uetypes.TxType_PAYLOAD, false},
		{"FUNDS_AND_PAYLOAD", uetypes.TxType_FUNDS_AND_PAYLOAD, false},
		{"GAS_AND_PAYLOAD", uetypes.TxType_GAS_AND_PAYLOAD, false},
		{"INBOUND_REVERT", uetypes.TxType_INBOUND_REVERT, false},
		{"1", uetypes.TxType(1), false},
		{"3", uetypes.TxType(3), false},
		{"invalid", uetypes.TxType_UNSPECIFIED_TX, true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := parseTxType(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

// ============================================================
//  TestComputeUnitLimitInstruction
// ============================================================

func TestBuildSetComputeUnitLimitInstruction(t *testing.T) {
	builder := newTestBuilder(t)
	ix := builder.buildSetComputeUnitLimitInstruction(300000)

	// Verify program ID is Compute Budget
	expectedProgramID := solana.MustPublicKeyFromBase58("ComputeBudget111111111111111111111111111111")
	assert.Equal(t, expectedProgramID, ix.ProgramID())

	// Verify instruction data
	data, err := ix.Data()
	require.NoError(t, err)
	assert.Len(t, data, 5)
	assert.Equal(t, byte(2), data[0], "instruction type = SetComputeUnitLimit")
	assert.Equal(t, uint32(300000), binary.LittleEndian.Uint32(data[1:5]))
}

// ============================================================
//  TestGatewayAccountMetaStruct
// ============================================================

func TestGatewayAccountMetaStruct(t *testing.T) {
	var pk [32]byte
	for i := range pk {
		pk[i] = byte(i)
	}
	meta := GatewayAccountMeta{Pubkey: pk, IsWritable: true}
	assert.Equal(t, pk, meta.Pubkey)
	assert.True(t, meta.IsWritable)
}

// ============================================================
//  TestGasLimitParsing
// ============================================================

func TestGasLimitParsing(t *testing.T) {
	tests := []struct {
		name     string
		gasLimit string
		expected uint32
	}{
		{"empty → default", "", DefaultComputeUnitLimit},
		{"zero → default", "0", DefaultComputeUnitLimit},
		{"valid", "300000", 300000},
		{"invalid number → default", "abc", DefaultComputeUnitLimit},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result uint32
			if tt.gasLimit == "" || tt.gasLimit == "0" {
				result = DefaultComputeUnitLimit
			} else {
				parsed, err := parseUint32(tt.gasLimit)
				if err != nil {
					result = DefaultComputeUnitLimit
				} else {
					result = parsed
				}
			}
			assert.Equal(t, tt.expected, result)
		})
	}
}

func parseUint32(s string) (uint32, error) {
	val, err := parseUint64(s)
	if err != nil {
		return 0, err
	}
	return uint32(val), nil
}

func parseUint64(s string) (uint64, error) {
	var val uint64
	_, err := fmt.Sscanf(s, "%d", &val)
	return val, err
}

// ============================================================
//  TestEndToEndMessageAndDataConsistency
// ============================================================

func TestEndToEndWithdrawMessageAndData(t *testing.T) {
	// Verifies that the TSS message hash (signed by TSS) ends up in the
	// instruction data's message_hash field at the correct offset.
	builder := newTestBuilder(t)

	txID := makeTxID(0xAA)
	utxID := makeTxID(0xBB)
	sender := makeSender(0xCC)
	token := [32]byte{} // native SOL
	target := makeTxID(0xDD)

	msgHash, err := builder.constructTSSMessage(
		1, "devnet", 42, 1000000,
		txID, utxID, sender, token,
		0, target, nil, nil, 0,
		[32]byte{}, [32]byte{},
	)
	require.NoError(t, err)

	sig := make([]byte, 64)
	instrData := builder.buildWithdrawAndExecuteData(
		1, txID, utxID, 1000000, sender,
		[]byte{}, []byte{}, 0, 0,
		sig, 0, msgHash, 42,
	)

	// Extract message_hash from instruction data
	// Offset: 8(disc) + 1(id) + 32(txid) + 32(utxid) + 8(amount) + 20(sender)
	//       + 4(wf_len) + 0(wf) + 4(ix_len) + 0(ix) + 8(gas) + 8(rent) + 64(sig) + 1(recov)
	//       = 190
	msgHashFromData := instrData[190:222]
	assert.Equal(t, msgHash, msgHashFromData, "message_hash in instruction data must match TSS message hash")

	// Extract nonce from instruction data
	nonceFromData := binary.LittleEndian.Uint64(instrData[222:230])
	assert.Equal(t, uint64(42), nonceFromData, "nonce in instruction data must match")
}

// ============================================================
//  TestAnchorDiscriminatorKnownValues
// ============================================================

func TestAnchorDiscriminatorKnownValues(t *testing.T) {
	// Verify discriminator values are deterministic and can be independently computed
	for _, method := range []string{"withdraw_and_execute", "revert_universal_tx", "revert_universal_tx_token"} {
		disc := anchorDiscriminator(method)
		h := sha256.Sum256([]byte("global:" + method))
		assert.Equal(t, h[:8], disc, "discriminator for %s", method)
	}
}

// ============================================================
//  TestDetermineRecoveryID — real EVM key signing
// ============================================================

func TestDetermineRecoveryID(t *testing.T) {
	builder := newTestBuilder(t)
	evmKey, _, ethAddrHex := generateTestEVMKey(t)

	t.Run("recovers correct ID from real signature", func(t *testing.T) {
		// Construct a real TSS message hash
		msgHash, err := builder.constructTSSMessage(
			1, "devnet", 0, 1000000,
			makeTxID(0xAA), makeTxID(0xBB), makeSender(0xCC), [32]byte{},
			0, makeTxID(0xDD), nil, nil, 0,
			[32]byte{}, [32]byte{},
		)
		require.NoError(t, err)

		sig, expectedRecoveryID := signMessageHash(t, evmKey, msgHash)

		recoveryID, err := builder.determineRecoveryID(msgHash, sig, ethAddrHex)
		require.NoError(t, err)
		assert.Equal(t, expectedRecoveryID, recoveryID)
	})

	t.Run("fails with wrong address", func(t *testing.T) {
		msgHash := crypto.Keccak256([]byte("test message"))
		sig, _ := signMessageHash(t, evmKey, msgHash)

		_, err := builder.determineRecoveryID(msgHash, sig, "0000000000000000000000000000000000000000")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to determine recovery ID")
	})

	t.Run("works with 0x-prefixed address", func(t *testing.T) {
		msgHash := crypto.Keccak256([]byte("another test"))
		sig, expectedRecoveryID := signMessageHash(t, evmKey, msgHash)

		recoveryID, err := builder.determineRecoveryID(msgHash, sig, "0x"+ethAddrHex)
		require.NoError(t, err)
		assert.Equal(t, expectedRecoveryID, recoveryID)
	})
}

// ============================================================
//  TestEndToEndWithRealSignature
//  Full offline end-to-end: construct TSS message → sign with
//  real EVM key → build instruction data → verify recovery
// ============================================================

func TestEndToEndWithRealSignature(t *testing.T) {
	builder := newTestBuilder(t)
	evmKey, _, ethAddrHex := generateTestEVMKey(t)

	txID := makeTxID(0xAA)
	utxID := makeTxID(0xBB)
	sender := makeSender(0xCC)
	token := [32]byte{} // native SOL
	target := makeTxID(0xDD)

	t.Run("withdraw flow with real signature", func(t *testing.T) {
		nonce := uint64(42)
		amount := uint64(1000000)

		// 1. Construct TSS message hash (what TSS nodes would sign)
		msgHash, err := builder.constructTSSMessage(
			1, "devnet", nonce, amount,
			txID, utxID, sender, token,
			0, target, nil, nil, 0,
			[32]byte{}, [32]byte{},
		)
		require.NoError(t, err)

		// 2. Sign with real EVM key (simulating TSS signing)
		sig, _ := signMessageHash(t, evmKey, msgHash)

		// 3. Determine recovery ID (what the relayer does)
		recoveryID, err := builder.determineRecoveryID(msgHash, sig, ethAddrHex)
		require.NoError(t, err)

		// 4. Build instruction data with real signature
		instrData := builder.buildWithdrawAndExecuteData(
			1, txID, utxID, amount, sender,
			[]byte{}, []byte{}, 0, 0,
			sig, recoveryID, msgHash, nonce,
		)

		// 5. Verify the instruction data contains the real signature
		assert.Equal(t, sig, instrData[125:189], "real signature in instruction data")
		assert.Equal(t, recoveryID, instrData[189], "recovery ID in instruction data")
		assert.Equal(t, msgHash, instrData[190:222], "message hash in instruction data")
	})

	t.Run("execute flow with real signature", func(t *testing.T) {
		nonce := uint64(7)
		amount := uint64(500)
		accs := []GatewayAccountMeta{
			{Pubkey: makeTxID(0x11), IsWritable: true},
		}
		ixData := []byte{0xDE, 0xAD}
		rentFee := uint64(5000)

		msgHash, err := builder.constructTSSMessage(
			2, "devnet", nonce, amount,
			txID, utxID, sender, token,
			0, target, accs, ixData, rentFee,
			[32]byte{}, [32]byte{},
		)
		require.NoError(t, err)

		sig, _ := signMessageHash(t, evmKey, msgHash)
		recoveryID, err := builder.determineRecoveryID(msgHash, sig, ethAddrHex)
		require.NoError(t, err)

		wf := accountsToWritableFlags(accs)
		instrData := builder.buildWithdrawAndExecuteData(
			2, txID, utxID, amount, sender,
			wf, ixData,
			0, rentFee,
			sig, recoveryID, msgHash, nonce,
		)

		// Verify instruction data length includes variable-length fields
		// 8(disc) + 1(id) + 32(txid) + 32(utxid) + 8(amt) + 20(sender)
		// + 4+1(wf) + 4+2(ix) + 8(gas) + 8(rent) + 64(sig) + 1(recov) + 32(hash) + 8(nonce)
		expectedLen := 8 + 1 + 32 + 32 + 8 + 20 + 5 + 6 + 8 + 8 + 64 + 1 + 32 + 8
		assert.Len(t, instrData, expectedLen)
	})

	t.Run("revert SOL flow with real signature", func(t *testing.T) {
		nonce := uint64(10)
		amount := uint64(500000)
		revertRecipient := makeTxID(0xEE)

		msgHash, err := builder.constructTSSMessage(
			3, "devnet", nonce, amount,
			txID, utxID, sender, token,
			0, [32]byte{}, nil, nil, 0,
			revertRecipient, [32]byte{},
		)
		require.NoError(t, err)

		sig, _ := signMessageHash(t, evmKey, msgHash)
		recoveryID, err := builder.determineRecoveryID(msgHash, sig, ethAddrHex)
		require.NoError(t, err)

		recipient := solana.PublicKeyFromBytes(revertRecipient[:])
		instrData := builder.buildRevertData(
			3, txID, utxID, amount, recipient,
			[]byte("revert msg"), 0,
			sig, recoveryID, msgHash, nonce,
		)

		// Verify discriminator is for revert_universal_tx
		expectedDisc := anchorDiscriminator("revert_universal_tx")
		assert.Equal(t, expectedDisc, instrData[:8])
	})
}

// ============================================================
//  Simulation Tests — live devnet end-to-end
//
//  Run: go test -run TestSimulate -v -count=1 -timeout 120s
//
//  Each test does the full pipeline:
//    1. Connect to devnet RPC
//    2. Generate fresh Solana relayer keypair (written to temp dir)
//    3. Generate fresh EVM key for signing
//    4. GetOutboundSigningRequest (fetches TSS PDA nonce from chain)
//    5. Sign the message hash with the EVM key (secp256k1)
//    6. BroadcastOutboundSigningRequest (assembles & sends the Solana tx)
//
//  Expected: Steps 1-5 always succeed. Step 6 fails with
//  "failed to determine recovery ID" because the generated EVM key
//  doesn't match the TSS ETH address stored on-chain. This validates
//  the entire assembly pipeline up to the on-chain auth check.
// ============================================================

const (
	devnetGatewayAddress = "DJoFYDpgbTfxbXBv1QYhYGc9FK4J5FUKpYXAfSkHryXp"
	devnetRPCURL         = "https://api.devnet.solana.com"
	devnetGenesisHash    = "EtWTRABZaYq6iMfeYKouRu166VU2xqa1wcaWoxPkrZBG"
	devnetSPLMint        = "EiXDnrAg9ea2Q6vEPV7E5TpTU1vh41jcuZqKjU5Dc4ZF"
	devnetMemoProgram    = "MemoSq4gqABAXKb96qnH8TysNcWxMyWCqXgDLGmfcHr"

	// Hardcoded EVM private key for simulation tests.
	// ETH address: 0xc681e7bdacfe4dc7209a15ff052f897c3d87008f
	// Set this address in the TSS PDA on the gateway contract for signatures to pass on-chain.
	testEVMPrivKeyHex = "d54b0eb459b7c0b82e3c21ced25f52a0a7fae6ed1a8614df46dda86c8d5f1e59"

	// Hardcoded Solana relayer keypair for simulation tests.
	// Pubkey: AdWDRaQfvWJqW4TaxTrXP5WogCWJMJBrtBfGjjHUDADM
	testSolanaKeypairJSON = `[226,7,176,193,18,2,55,106,191,150,176,87,157,216,118,97,236,128,2,104,181,206,160,147,5,152,0,115,23,8,103,189,143,19,31,194,227,248,222,123,219,13,143,47,154,104,201,235,13,16,11,45,117,154,117,37,130,196,58,154,89,228,136,32]`
)

// setupDevnetSimulation creates RPCClient and TxBuilder for devnet.
// Uses the hardcoded Solana relayer keypair (AdWDRaQfvWJqW4TaxTrXP5WogCWJMJBrtBfGjjHUDADM).
func setupDevnetSimulation(t *testing.T) (*RPCClient, *TxBuilder) {

	t.Skip("skipping simulation tests") // DELIBERATELY SKIPPING SIMULATION TESTS
	t.Helper()
	if testing.Short() {
		t.Skip("skipping simulation test in short mode")
	}

	logger := zerolog.New(zerolog.NewTestWriter(t)).Level(zerolog.DebugLevel)
	rpcClient, err := NewRPCClient([]string{devnetRPCURL}, devnetGenesisHash, logger)
	if err != nil {
		t.Skipf("skipping: failed to connect to Devnet RPC: %v", err)
	}

	// Write the hardcoded keypair JSON to the temp dir so loadRelayerKeypair can find it.
	tmpDir := t.TempDir()
	relayerDir := filepath.Join(tmpDir, "relayer")
	require.NoError(t, os.MkdirAll(relayerDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(relayerDir, "solana.json"), []byte(testSolanaKeypairJSON), 0o600))

	builder, err := NewTxBuilder(rpcClient, "solana:"+devnetGenesisHash, devnetGatewayAddress, tmpDir, logger)
	require.NoError(t, err)

	t.Logf("relayer pubkey: AdWDRaQfvWJqW4TaxTrXP5WogCWJMJBrtBfGjjHUDADM")
	return rpcClient, builder
}

// loadTestEVMKey loads the hardcoded EVM private key and returns the key + ETH address hex.
func loadTestEVMKey(t *testing.T) (*ecdsa.PrivateKey, string) {
	t.Helper()
	privBytes, err := hex.DecodeString(testEVMPrivKeyHex)
	require.NoError(t, err)
	key, err := crypto.ToECDSA(privBytes)
	require.NoError(t, err)
	pubBytes := crypto.FromECDSAPub(&key.PublicKey)
	addrBytes := crypto.Keccak256(pubBytes[1:])[12:]
	return key, hex.EncodeToString(addrBytes)
}

// newDevnetOutbound creates an OutboundCreatedEvent using the hardcoded EVM key as sender
// and a fresh Solana wallet as recipient. Uses random tx_id/utx_id for each call to avoid
// executed_tx PDA collisions between tests.
func newDevnetOutbound(t *testing.T, amount, assetAddr, payload, revertMsg, txType string) (*uetypes.OutboundCreatedEvent, *ecdsa.PrivateKey) {
	t.Helper()

	evmKey, ethAddrHex := loadTestEVMKey(t)
	recipientWallet := solana.NewWallet()

	txIDBytes := make([]byte, 32)
	utxIDBytes := make([]byte, 32)
	_, err := crand.Read(txIDBytes)
	require.NoError(t, err)
	_, err = crand.Read(utxIDBytes)
	require.NoError(t, err)
	return &uetypes.OutboundCreatedEvent{
		TxID:             "0x" + hex.EncodeToString(txIDBytes),
		UniversalTxId:    "0x" + hex.EncodeToString(utxIDBytes),
		DestinationChain: "solana:" + devnetGenesisHash,
		Sender:           "0x" + ethAddrHex,
		Recipient:        recipientWallet.PublicKey().String(),
		Amount:           amount,
		AssetAddr:        assetAddr,
		Payload:          payload,
		GasLimit:         "400000",
		TxType:           txType,
		RevertMsg:        revertMsg,
	}, evmKey
}

// buildAndSimulate runs the full pipeline: GetOutboundSigningRequest → sign → BuildOutboundTransaction → SimulateTransaction.
// Uses simulation (no broadcast), so no on-chain state is modified (nonce stays the same, no SOL spent).
// Returns the simulation result and any build errors.
func buildAndSimulate(t *testing.T, rpcClient *RPCClient, builder *TxBuilder, data *uetypes.OutboundCreatedEvent, evmKey *ecdsa.PrivateKey) (*rpc.SimulateTransactionResult, error) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Step 1: Build signing request (fetches TSS PDA nonce from on-chain)
	req, err := builder.GetOutboundSigningRequest(ctx, data, big.NewInt(1000), 0)
	if err != nil {
		return nil, fmt.Errorf("GetOutboundSigningRequest: %w", err)
	}
	require.NotNil(t, req)
	require.Len(t, req.SigningHash, 32)
	t.Logf("  signing_hash=0x%s nonce=%d", hex.EncodeToString(req.SigningHash), req.Nonce)

	// Step 2: Sign with EVM key (secp256k1)
	sig, _ := signMessageHash(t, evmKey, req.SigningHash)

	// Step 3: Build the Solana transaction (derives PDAs, builds instruction data, signs with relayer key)
	tx, _, err := builder.BuildOutboundTransaction(ctx, req, data, sig)
	if err != nil {
		return nil, fmt.Errorf("BuildOutboundTransaction: %w", err)
	}

	// Step 4: Simulate against devnet (no broadcast, no state changes)
	result, err := rpcClient.SimulateTransaction(ctx, tx)
	if err != nil {
		return nil, fmt.Errorf("SimulateTransaction: %w", err)
	}

	return result, nil
}

// requireSimulationSuccess asserts that a simulation completed without errors and logs the result.
func requireSimulationSuccess(t *testing.T, result *rpc.SimulateTransactionResult) {
	t.Helper()
	require.Nil(t, result.Err, "simulation failed: %v\nlogs: %v", result.Err, result.Logs)
	t.Logf("simulation passed (%d compute units)", *result.UnitsConsumed)
	for _, log := range result.Logs {
		t.Logf("  %s", log)
	}
}

// ---- Withdraw ----

func TestSimulate_Withdraw_NativeSOL(t *testing.T) {
	rpcClient, builder := setupDevnetSimulation(t)
	defer rpcClient.Close()

	// Amount must be >= rent-exempt minimum (~890,880 lamports) since the recipient
	// is a fresh account. Solana rejects transfers that leave accounts below rent-exempt.
	// Payload includes instruction_id=1 (withdraw) per integration guide.
	withdrawPayload := "0x" + hex.EncodeToString(buildMockWithdrawPayload())
	data, evmKey := newDevnetOutbound(t, "1000000", "", withdrawPayload, "", "FUNDS")

	result, err := buildAndSimulate(t, rpcClient, builder, data, evmKey)
	require.NoError(t, err)
	requireSimulationSuccess(t, result)
}

func TestSimulate_Withdraw_SPLToken(t *testing.T) {
	rpcClient, builder := setupDevnetSimulation(t)
	defer rpcClient.Close()

	withdrawPayload := "0x" + hex.EncodeToString(buildMockWithdrawPayload())
	data, evmKey := newDevnetOutbound(t, "1000000", devnetSPLMint, withdrawPayload, "", "FUNDS")

	result, err := buildAndSimulate(t, rpcClient, builder, data, evmKey)
	require.NoError(t, err)
	requireSimulationSuccess(t, result)
}

// ---- Execute ----

func TestSimulate_Execute_NativeSOL(t *testing.T) {
	rpcClient, builder := setupDevnetSimulation(t)
	defer rpcClient.Close()

	// Use the SPL Memo program as the execute destination.
	// It accepts any UTF-8 data with no required accounts, making it ideal for
	// CPI testing without needing initialized state on devnet.
	ixData := []byte("hello from push chain")
	payload := buildMockExecutePayload(nil, ixData, 0)
	payloadHex := "0x" + hex.EncodeToString(payload)

	// Recipient = Memo program (becomes destination_program in execute mode)
	data, evmKey := newDevnetOutbound(t, "10000000", "", payloadHex, "", "FUNDS_AND_PAYLOAD")
	data.Recipient = devnetMemoProgram

	result, err := buildAndSimulate(t, rpcClient, builder, data, evmKey)
	require.NoError(t, err)
	requireSimulationSuccess(t, result)
}

func TestSimulate_Execute_SPLToken(t *testing.T) {
	rpcClient, builder := setupDevnetSimulation(t)
	defer rpcClient.Close()

	// Use the SPL Memo program as the execute destination (same as SOL test above).
	ixData := []byte("spl execute memo")
	payload := buildMockExecutePayload(nil, ixData, 0)
	payloadHex := "0x" + hex.EncodeToString(payload)

	// Recipient = Memo program (becomes destination_program in execute mode)
	data, evmKey := newDevnetOutbound(t, "500000", devnetSPLMint, payloadHex, "", "FUNDS_AND_PAYLOAD")
	data.Recipient = devnetMemoProgram

	result, err := buildAndSimulate(t, rpcClient, builder, data, evmKey)
	require.NoError(t, err)
	requireSimulationSuccess(t, result)
}

// ---- Revert ----

func TestSimulate_Revert_NativeSOL(t *testing.T) {
	rpcClient, builder := setupDevnetSimulation(t)
	defer rpcClient.Close()

	data, evmKey := newDevnetOutbound(t, "10000000", "", "0x", hex.EncodeToString([]byte("revert native")), "INBOUND_REVERT")

	result, err := buildAndSimulate(t, rpcClient, builder, data, evmKey)
	require.NoError(t, err)
	requireSimulationSuccess(t, result)
}

func TestSimulate_Revert_SPLToken(t *testing.T) {
	rpcClient, builder := setupDevnetSimulation(t)
	defer rpcClient.Close()

	data, evmKey := newDevnetOutbound(t, "500000", devnetSPLMint, "0x", hex.EncodeToString([]byte("revert spl")), "INBOUND_REVERT")

	result, err := buildAndSimulate(t, rpcClient, builder, data, evmKey)
	require.NoError(t, err)
	requireSimulationSuccess(t, result)
}
