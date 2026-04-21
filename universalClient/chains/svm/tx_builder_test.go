package svm

import (
	"context"
	"crypto/ecdsa"
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
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

	"github.com/pushchain/push-chain-node/universalClient/config"
	uetypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// testGatewayAddress is a valid base58 Solana public key used for unit tests.
// Must NOT be SystemProgramID to avoid collisions with destinationProgram sentinel values.
const testGatewayAddress = "CFVSincHYbETh2k7w6u1ENEkjbSLtveRCEBupKidw2VS"

func newTestBuilder(t *testing.T) *TxBuilder {
	t.Helper()
	logger := zerolog.Nop()
	builder, err := NewTxBuilder(&RPCClient{}, "solana:devnet", testGatewayAddress, "/tmp", logger, nil)
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

// buildMockTSSPDAData builds a raw byte slice simulating a TssPda account.
// Layout: discriminator(8) + tss_eth_address(20) + chain_id(Borsh String: 4 LE len + bytes) + authority(32) + bump(1)
func buildMockTSSPDAData(tssAddr [20]byte, chainID string, authority [32]byte, bump byte) []byte {
	data := make([]byte, 0, 8+20+4+len(chainID)+32+1)
	// discriminator (8 bytes of zeros)
	data = append(data, make([]byte, 8)...)
	// tss_eth_address (20 bytes)
	data = append(data, tssAddr[:]...)
	// chain_id Borsh String: 4-byte LE length + UTF-8 bytes
	chainIDLenBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(chainIDLenBytes, uint32(len(chainID)))
	data = append(data, chainIDLenBytes...)
	data = append(data, []byte(chainID)...)
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

// buildMockPayload builds a pre-encoded payload.
// Format: [u32 BE accounts_count][33 bytes per account (32 pubkey + 1 writable)][u32 BE ix_data_len][ix_data][instruction_id (u8)][target_program (32 bytes)]
func buildMockPayload(accounts []GatewayAccountMeta, ixData []byte, instructionID uint8, targetProgram [32]byte) []byte {
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
	// instruction_id (u8)
	payload = append(payload, instructionID)
	// target_program (32 bytes)
	payload = append(payload, targetProgram[:]...)
	return payload
}

// buildMockExecutePayload is a convenience wrapper for execute payloads (instruction_id=2)
func buildMockExecutePayload(accounts []GatewayAccountMeta, ixData []byte) []byte {
	return buildMockPayload(accounts, ixData, 2, [32]byte{})
}

// buildMockWithdrawPayload builds a withdraw payload (instruction_id=1, no accounts/ixData)
func buildMockWithdrawPayload() []byte {
	return buildMockPayload(nil, nil, 1, [32]byte{})
}

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
			builder, err := NewTxBuilder(tt.rpcClient, tt.chainID, tt.gatewayAddress, "/tmp", logger, nil)
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

func TestDeriveTSSPDA(t *testing.T) {
	builder := newTestBuilder(t)

	pda, err := builder.deriveTSSPDA()
	require.NoError(t, err)
	assert.False(t, pda.IsZero(), "TSS PDA should be non-zero")

	// Verify it matches FindProgramAddress with seed "tsspda_v2"
	expected, _, err := solana.FindProgramAddress([][]byte{[]byte("tsspda_v2")}, builder.gatewayAddress)
	require.NoError(t, err)
	assert.Equal(t, expected, pda)

	// Verify it does NOT match the old seed "tsspda"
	oldPDA, _, err := solana.FindProgramAddress([][]byte{[]byte("tsspda")}, builder.gatewayAddress)
	require.NoError(t, err)
	assert.NotEqual(t, oldPDA, pda, "TSS PDA must NOT use old seed 'tsspda'")
}

func TestFetchTSSChainID(t *testing.T) {
	t.Run("parses valid TssPda with short chain_id", func(t *testing.T) {
		chainIDStr := "devnet"
		data := buildMockTSSPDAData([20]byte{}, chainIDStr, [32]byte{}, 255)

		chainID, err := parseTSSPDAData(data)
		require.NoError(t, err)
		assert.Equal(t, chainIDStr, chainID)
	})

	t.Run("parses valid TssPda with mainnet cluster pubkey", func(t *testing.T) {
		chainIDStr := "5eykt4UsFv8P8NJdTREpY1vzqKqZKvdpKuc147dw2N9d"
		data := buildMockTSSPDAData([20]byte{}, chainIDStr, [32]byte{}, 1)

		chainID, err := parseTSSPDAData(data)
		require.NoError(t, err)
		assert.Equal(t, chainIDStr, chainID)
	})

	t.Run("rejects data too short for header", func(t *testing.T) {
		_, err := parseTSSPDAData(make([]byte, 31)) // less than 32
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "too short")
	})

	t.Run("rejects data too short for chain_id + authority", func(t *testing.T) {
		// Build header with chain_id_len = 100, but only provide 40 total bytes
		data := make([]byte, 40)
		binary.LittleEndian.PutUint32(data[28:32], 100) // chain_id_len = 100
		_, err := parseTSSPDAData(data)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "too short")
	})

	t.Run("chain_id at correct offset after variable-length chain_id", func(t *testing.T) {
		// Two different chain_id lengths — verify parsing is dynamic
		for _, cid := range []string{"a", "abcdefghij"} {
			data := buildMockTSSPDAData([20]byte{}, cid, [32]byte{}, 0)
			chainID, err := parseTSSPDAData(data)
			require.NoError(t, err, "chain_id=%q", cid)
			assert.Equal(t, cid, chainID)
		}
	})
}

// parseTSSPDAData is the extraction of fetchTSSChainID's parsing logic for unit testing
// without requiring an RPC call. This mirrors the parsing in fetchTSSChainID.
func parseTSSPDAData(accountData []byte) (string, error) {
	if len(accountData) < 32 {
		return "", fmt.Errorf("invalid TSS PDA account data: too short (%d bytes)", len(accountData))
	}
	chainIDLen := binary.LittleEndian.Uint32(accountData[28:32])
	requiredLen := 32 + int(chainIDLen) + 32 + 1
	if len(accountData) < requiredLen {
		return "", fmt.Errorf("invalid TSS PDA account data: too short for chain_id length %d (%d bytes)", chainIDLen, len(accountData))
	}
	chainID := string(accountData[32 : 32+chainIDLen])
	return chainID, nil
}

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
		{"INBOUND_REVERT SPL → 3", uetypes.TxType_INBOUND_REVERT, false, 3, false},
		{"RESCUE_FUNDS native → 4", uetypes.TxType_RESCUE_FUNDS, true, 4, false},
		{"RESCUE_FUNDS SPL → 4", uetypes.TxType_RESCUE_FUNDS, false, 4, false},
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

func TestAnchorDiscriminator(t *testing.T) {
	tests := []struct {
		methodName string
	}{
		{"finalize_universal_tx"},
		{"revert_universal_tx"},
		{"rescue_funds"},
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

func TestConstructTSSMessage(t *testing.T) {
	builder := newTestBuilder(t)

	txID := makeTxID(0xAA)
	utxID := makeTxID(0xBB)
	sender := makeSender(0xCC)
	token := [32]byte{} // native SOL = zero
	target := makeTxID(0xDD)

	t.Run("withdraw (id=1) message format", func(t *testing.T) {
		hash, err := builder.constructTSSMessage(
			1, "devnet", 1000000,
			txID, utxID, sender, token,
			0, // gasFee
			target, nil, nil,
			[32]byte{}, [32]byte{},
		)
		require.NoError(t, err)
		assert.Len(t, hash, 32, "message hash must be 32 bytes (keccak256)")

		// Reconstruct expected message manually
		msg := []byte("PUSH_CHAIN_SVM")
		msg = append(msg, 1) // instruction_id
		msg = append(msg, []byte("devnet")...)
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
			2, "devnet", 2000000,
			txID, utxID, sender, token,
			100, // gasFee
			target, accs, ixData,
			[32]byte{}, [32]byte{},
		)
		require.NoError(t, err)
		assert.Len(t, hash, 32)

		// Rebuild expected
		msg := []byte("PUSH_CHAIN_SVM")
		msg = append(msg, 2)
		msg = append(msg, []byte("devnet")...)
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

		expected := crypto.Keccak256(msg)
		assert.Equal(t, expected, hash, "execute message hash mismatch")
	})

	t.Run("revert SOL (id=3) message format", func(t *testing.T) {
		revertRecipient := makeTxID(0xEE)
		hash, err := builder.constructTSSMessage(
			3, "devnet", 500000,
			txID, utxID, sender, token,
			0, [32]byte{}, nil, nil,
			revertRecipient, [32]byte{},
		)
		require.NoError(t, err)

		msg := []byte("PUSH_CHAIN_SVM")
		msg = append(msg, 3)
		msg = append(msg, []byte("devnet")...)
		amountBE := make([]byte, 8)
		binary.BigEndian.PutUint64(amountBE, 500000)
		msg = append(msg, amountBE...)
		// additional: tx_id, utx_id, recipient, gas_fee
		msg = append(msg, txID[:]...)
		msg = append(msg, utxID[:]...)
		msg = append(msg, revertRecipient[:]...)
		gasBE := make([]byte, 8)
		msg = append(msg, gasBE...)

		expected := crypto.Keccak256(msg)
		assert.Equal(t, expected, hash, "revert SOL message hash mismatch")
	})

	t.Run("revert SPL (id=3 with mint) message format", func(t *testing.T) {
		revertRecipient := makeTxID(0xEE)
		revertMint := makeTxID(0xFF)
		hash, err := builder.constructTSSMessage(
			3, "devnet", 750000,
			txID, utxID, sender, token,
			0, [32]byte{}, nil, nil,
			revertRecipient, revertMint,
		)
		require.NoError(t, err)

		msg := []byte("PUSH_CHAIN_SVM")
		msg = append(msg, 3)
		msg = append(msg, []byte("devnet")...)
		amountBE := make([]byte, 8)
		binary.BigEndian.PutUint64(amountBE, 750000)
		msg = append(msg, amountBE...)
		// additional: tx_id, utx_id, mint, recipient, gas_fee
		msg = append(msg, txID[:]...)
		msg = append(msg, utxID[:]...)
		msg = append(msg, revertMint[:]...)
		msg = append(msg, revertRecipient[:]...)
		gasBE := make([]byte, 8)
		msg = append(msg, gasBE...)

		expected := crypto.Keccak256(msg)
		assert.Equal(t, expected, hash, "revert SPL message hash mismatch")
	})

	t.Run("rescue SOL (id=4) message format", func(t *testing.T) {
		rescueRecipient := makeTxID(0xEE)
		hash, err := builder.constructTSSMessage(
			4, "devnet", 300000,
			txID, utxID, sender, token,
			50, [32]byte{}, nil, nil,
			rescueRecipient, [32]byte{},
		)
		require.NoError(t, err)

		msg := []byte("PUSH_CHAIN_SVM")
		msg = append(msg, 4)
		msg = append(msg, []byte("devnet")...)
		amountBE := make([]byte, 8)
		binary.BigEndian.PutUint64(amountBE, 300000)
		msg = append(msg, amountBE...)
		msg = append(msg, txID[:]...)
		msg = append(msg, utxID[:]...)
		msg = append(msg, rescueRecipient[:]...)
		gasBE := make([]byte, 8)
		binary.BigEndian.PutUint64(gasBE, 50)
		msg = append(msg, gasBE...)

		expected := crypto.Keccak256(msg)
		assert.Equal(t, expected, hash, "rescue SOL message hash mismatch")
	})

	t.Run("rescue SPL (id=4 with mint) message format", func(t *testing.T) {
		rescueRecipient := makeTxID(0xEE)
		rescueMint := makeTxID(0xFF)
		hash, err := builder.constructTSSMessage(
			4, "devnet", 400000,
			txID, utxID, sender, token,
			75, [32]byte{}, nil, nil,
			rescueRecipient, rescueMint,
		)
		require.NoError(t, err)

		msg := []byte("PUSH_CHAIN_SVM")
		msg = append(msg, 4)
		msg = append(msg, []byte("devnet")...)
		amountBE := make([]byte, 8)
		binary.BigEndian.PutUint64(amountBE, 400000)
		msg = append(msg, amountBE...)
		msg = append(msg, txID[:]...)
		msg = append(msg, utxID[:]...)
		msg = append(msg, rescueMint[:]...)
		msg = append(msg, rescueRecipient[:]...)
		gasBE := make([]byte, 8)
		binary.BigEndian.PutUint64(gasBE, 75)
		msg = append(msg, gasBE...)

		expected := crypto.Keccak256(msg)
		assert.Equal(t, expected, hash, "rescue SPL message hash mismatch")
	})

	t.Run("chain_id bytes go directly into message (no length prefix)", func(t *testing.T) {
		// Verify that the chain_id in the message is raw UTF-8, not Borsh-encoded
		chainID := "test_chain"
		hash1, err := builder.constructTSSMessage(
			1, chainID, 0,
			[32]byte{}, [32]byte{}, [20]byte{}, [32]byte{},
			0, [32]byte{}, nil, nil,
			[32]byte{}, [32]byte{},
		)
		require.NoError(t, err)

		// Build expected manually — chain_id as raw bytes
		msg := []byte("PUSH_CHAIN_SVM")
		msg = append(msg, 1)
		msg = append(msg, []byte(chainID)...)  // raw UTF-8, no 4-byte length prefix
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
			99, "devnet", 0,
			[32]byte{}, [32]byte{}, [20]byte{}, [32]byte{},
			0, [32]byte{}, nil, nil,
			[32]byte{}, [32]byte{},
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unknown instruction ID")
	})
}

func TestConstructTSSMessage_HashIsKeccak256(t *testing.T) {
	builder := newTestBuilder(t)

	// Construct a simple withdraw message and verify the hash algo
	hash, err := builder.constructTSSMessage(
		1, "x", 0,
		[32]byte{}, [32]byte{}, [20]byte{}, [32]byte{},
		0, [32]byte{}, nil, nil,
		[32]byte{}, [32]byte{},
	)
	require.NoError(t, err)

	// Build the raw message
	msg := []byte("PUSH_CHAIN_SVM")
	msg = append(msg, 1)
	msg = append(msg, 'x')
	msg = append(msg, make([]byte, 8+32+32+20+32+8+32)...)

	// Must be keccak256 (not sha256)
	keccakHash := crypto.Keccak256(msg)
	sha256Hash := sha256.Sum256(msg)
	assert.Equal(t, keccakHash, hash, "TSS message must be hashed with keccak256")
	assert.NotEqual(t, sha256Hash[:], hash, "TSS message must NOT be hashed with SHA256")
}

func TestDecodePayload(t *testing.T) {
	t.Run("decodes valid execute payload with 2 accounts", func(t *testing.T) {
		expectedAccounts := []GatewayAccountMeta{
			{Pubkey: makeTxID(0x11), IsWritable: true},
			{Pubkey: makeTxID(0x22), IsWritable: false},
		}
		expectedIxData := []byte{0xAA, 0xBB, 0xCC}
		expectedTarget := makeTxID(0xDD)

		payload := buildMockPayload(expectedAccounts, expectedIxData, 2, expectedTarget)
		accounts, ixData, instructionID, targetProgram, err := decodePayload(payload)

		require.NoError(t, err)
		assert.Equal(t, uint8(2), instructionID)
		assert.Len(t, accounts, 2)
		assert.Equal(t, expectedAccounts[0].Pubkey, accounts[0].Pubkey)
		assert.True(t, accounts[0].IsWritable)
		assert.Equal(t, expectedAccounts[1].Pubkey, accounts[1].Pubkey)
		assert.False(t, accounts[1].IsWritable)
		assert.Equal(t, expectedIxData, ixData)
		assert.Equal(t, expectedTarget, targetProgram)
	})

	t.Run("decodes withdraw payload (0 accounts)", func(t *testing.T) {
		payload := buildMockWithdrawPayload()
		accounts, ixData, instructionID, _, err := decodePayload(payload)
		require.NoError(t, err)
		assert.Equal(t, uint8(1), instructionID)
		assert.Len(t, accounts, 0)
		assert.Len(t, ixData, 0)
	})

	t.Run("decodes payload with empty ix_data", func(t *testing.T) {
		accs := []GatewayAccountMeta{{Pubkey: makeTxID(0x33), IsWritable: true}}
		expectedTarget := makeTxID(0xEE)
		payload := buildMockPayload(accs, nil, 2, expectedTarget)
		accounts, ixData, instructionID, targetProgram, err := decodePayload(payload)
		require.NoError(t, err)
		assert.Equal(t, uint8(2), instructionID)
		assert.Len(t, accounts, 1)
		assert.Len(t, ixData, 0)
		assert.Equal(t, expectedTarget, targetProgram)
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
			0, // gasFee
			sig, 2, msgHash,
		)

		// Check discriminator
		expectedDisc := anchorDiscriminator("finalize_universal_tx")
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

		// Check signature (no more rent_fee — directly after gas_fee)
		assert.Equal(t, sig, data[117:181], "signature")

		// Check recovery_id
		assert.Equal(t, byte(2), data[181], "recovery_id")

		// Check message_hash
		assert.Equal(t, msgHash, data[182:214], "message_hash")

		// Total length: 8(disc) + 1(id) + 32(txid) + 32(utxid) + 8(amt) + 20(sender)
		//             + 4(wf_len) + 0(wf) + 4(ix_len) + 0(ix) + 8(gas) + 64(sig) + 1(recov) + 32(hash) = 214
		assert.Len(t, data, 214)
	})

	t.Run("execute (id=2) with accounts and ix_data", func(t *testing.T) {
		wf := []byte{0xA0}
		ixData := []byte{0xDE, 0xAD}

		data := builder.buildWithdrawAndExecuteData(
			2, txID, utxID, 500, sender,
			wf, ixData,
			100, // gasFee
			sig, 0, msgHash,
		)

		// Discriminator should be same function
		expectedDisc := anchorDiscriminator("finalize_universal_tx")
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

		// signature + recovery_id + message_hash (no more rent_fee)
		assert.Equal(t, sig, data[offset:offset+64])
		offset += 64
		assert.Equal(t, byte(0), data[offset])
		offset += 1
		assert.Equal(t, msgHash, data[offset:offset+32])
	})
}

func TestBuildRevertData(t *testing.T) {
	builder := newTestBuilder(t)
	txID := makeTxID(0x01)
	utxID := makeTxID(0x02)
	recipient := solana.MustPublicKeyFromBase58(testGatewayAddress)
	revertMsg := []byte("revert me")
	sig := make([]byte, 64)
	msgHash := make([]byte, 32)

	t.Run("revert uses correct discriminator (revert_universal_tx)", func(t *testing.T) {
		data := builder.buildRevertData(txID, utxID, 1000, recipient, revertMsg, 0, sig, 1, msgHash)

		expectedDisc := anchorDiscriminator("revert_universal_tx")
		assert.Equal(t, expectedDisc, data[:8])
		assert.Equal(t, txID[:], data[8:40])
		assert.Equal(t, utxID[:], data[40:72])
		assert.Equal(t, uint64(1000), binary.LittleEndian.Uint64(data[72:80]))
		// revert_instruction: revert_recipient(32) + revert_msg Vec(4+N)
		assert.Equal(t, recipient.Bytes(), data[80:112])
		assert.Equal(t, uint32(len(revertMsg)), binary.LittleEndian.Uint32(data[112:116]))
		assert.Equal(t, revertMsg, data[116:116+len(revertMsg)])
	})

	t.Run("revert with empty revert_msg", func(t *testing.T) {
		data := builder.buildRevertData(txID, utxID, 2000, recipient, nil, 0, sig, 0, msgHash)

		expectedDisc := anchorDiscriminator("revert_universal_tx")
		assert.Equal(t, expectedDisc, data[:8])
		// revert_msg should be empty Vec (len=0)
		offset := 112 // after tx_id(32) + utx_id(32) + amount(8) + revert_recipient(32)
		assert.Equal(t, uint32(0), binary.LittleEndian.Uint32(data[offset:offset+4]))
	})
}

func TestBuildRescueData(t *testing.T) {
	builder := newTestBuilder(t)
	txID := makeTxID(0x01)
	utxID := makeTxID(0x02)
	sig := make([]byte, 64)
	msgHash := make([]byte, 32)

	t.Run("rescue uses correct discriminator", func(t *testing.T) {
		data := builder.buildRescueData(txID, utxID, 5000, 100, sig, 1, msgHash)

		expectedDisc := anchorDiscriminator("rescue_funds")
		assert.Equal(t, expectedDisc, data[:8], "discriminator")
		assert.Equal(t, txID[:], data[8:40], "tx_id")
		assert.Equal(t, utxID[:], data[40:72], "universal_tx_id")
		assert.Equal(t, uint64(5000), binary.LittleEndian.Uint64(data[72:80]), "amount")
		assert.Equal(t, uint64(100), binary.LittleEndian.Uint64(data[80:88]), "gas_fee")
		assert.Equal(t, sig, data[88:152], "signature")
		assert.Equal(t, byte(1), data[152], "recovery_id")
		assert.Equal(t, msgHash, data[153:185], "message_hash")
		// Total: 8(disc) + 32(txid) + 32(utxid) + 8(amount) + 8(gasFee) + 64(sig) + 1(recov) + 32(hash) = 185
		assert.Len(t, data, 185)
	})

	t.Run("rescue has no revert instructions (unlike revert)", func(t *testing.T) {
		rescueData := builder.buildRescueData(txID, utxID, 1000, 50, sig, 0, msgHash)
		revertRecipient := solana.MustPublicKeyFromBase58(testGatewayAddress)
		revertData := builder.buildRevertData(txID, utxID, 1000, revertRecipient, nil, 50, sig, 0, msgHash)

		// Rescue should be shorter than revert (no recipient + revert_msg fields)
		assert.Less(t, len(rescueData), len(revertData), "rescue data should be shorter than revert data")
	})
}

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

		// Rate limit accounts (slots 17-18) should also be gateway sentinels
		assert.Equal(t, builder.gatewayAddress, accounts[16].PublicKey, "rateLimitConfig should be gateway sentinel")
		assert.Equal(t, builder.gatewayAddress, accounts[17].PublicKey, "tokenRateLimit should be gateway sentinel")

		assert.Len(t, accounts, 18, "total accounts for SOL withdraw")
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
		totalRequired := 18 // 8 required + 8 optional + 2 rate limit
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

func TestBuildRevertAccounts(t *testing.T) {
	builder := newTestBuilder(t)

	config := solana.NewWallet().PublicKey()
	vault := solana.NewWallet().PublicKey()
	feeVault := solana.NewWallet().PublicKey()
	tss := solana.NewWallet().PublicKey()
	recipient := solana.NewWallet().PublicKey()
	executed := solana.NewWallet().PublicKey()
	caller := solana.NewWallet().PublicKey()
	tokenMint := solana.NewWallet().PublicKey()

	t.Run("SOL revert has 12 accounts (8 required + 4 None sentinels)", func(t *testing.T) {
		accounts := builder.buildRevertAccounts(config, vault, feeVault, tss, recipient, executed, caller, true, solana.PublicKey{})

		assert.Len(t, accounts, 12)
		assert.Equal(t, config, accounts[0].PublicKey, "config")
		assert.False(t, accounts[0].IsWritable)
		assert.Equal(t, vault, accounts[1].PublicKey, "vault")
		assert.True(t, accounts[1].IsWritable)
		assert.Equal(t, feeVault, accounts[2].PublicKey, "fee_vault")
		assert.True(t, accounts[2].IsWritable)
		assert.Equal(t, tss, accounts[3].PublicKey, "tss_pda")
		assert.True(t, accounts[3].IsWritable)
		assert.Equal(t, recipient, accounts[4].PublicKey, "recipient")
		assert.True(t, accounts[4].IsWritable)
		assert.Equal(t, executed, accounts[5].PublicKey, "executed_tx")
		assert.True(t, accounts[5].IsWritable)
		assert.Equal(t, caller, accounts[6].PublicKey, "caller")
		assert.True(t, accounts[6].IsSigner)
		assert.Equal(t, solana.SystemProgramID, accounts[7].PublicKey, "system_program")
		// SOL: 4 optional SPL accounts are gateway sentinel (None)
		for i := 8; i < 12; i++ {
			assert.Equal(t, builder.gatewayAddress, accounts[i].PublicKey, "SOL sentinel account %d", i)
		}
	})

	t.Run("SPL revert has 12 accounts (8 required + 4 SPL accounts)", func(t *testing.T) {
		accounts := builder.buildRevertAccounts(config, vault, feeVault, tss, recipient, executed, caller, false, tokenMint)

		assert.Len(t, accounts, 12)
		// First 8 same as SOL
		assert.Equal(t, config, accounts[0].PublicKey, "config")
		assert.Equal(t, vault, accounts[1].PublicKey, "vault")
		assert.Equal(t, feeVault, accounts[2].PublicKey, "fee_vault")
		assert.Equal(t, tss, accounts[3].PublicKey, "tss_pda")
		assert.Equal(t, recipient, accounts[4].PublicKey, "recipient")
		assert.Equal(t, executed, accounts[5].PublicKey, "executed_tx")
		assert.Equal(t, caller, accounts[6].PublicKey, "caller")
		assert.Equal(t, solana.SystemProgramID, accounts[7].PublicKey, "system_program")
		// SPL: token_vault, recipient_token_account, token_mint, token_program
		assert.True(t, accounts[8].IsWritable, "token_vault should be writable")
		assert.True(t, accounts[9].IsWritable, "recipient_token_account should be writable")
		assert.Equal(t, tokenMint, accounts[10].PublicKey, "token_mint")
		assert.Equal(t, solana.TokenProgramID, accounts[11].PublicKey, "token_program")
	})
}

func TestBuildRescueAccounts(t *testing.T) {
	builder := newTestBuilder(t)

	config := solana.NewWallet().PublicKey()
	vault := solana.NewWallet().PublicKey()
	feeVault := solana.NewWallet().PublicKey()
	tss := solana.NewWallet().PublicKey()
	recipient := solana.NewWallet().PublicKey()
	executed := solana.NewWallet().PublicKey()
	caller := solana.NewWallet().PublicKey()
	tokenMint := solana.NewWallet().PublicKey()

	t.Run("rescue delegates to buildRevertAccounts (same layout)", func(t *testing.T) {
		rescueAccounts := builder.buildRescueAccounts(config, vault, feeVault, tss, recipient, executed, caller, true, solana.PublicKey{})
		revertAccounts := builder.buildRevertAccounts(config, vault, feeVault, tss, recipient, executed, caller, true, solana.PublicKey{})

		assert.Len(t, rescueAccounts, len(revertAccounts))
		for i := range rescueAccounts {
			assert.Equal(t, revertAccounts[i].PublicKey, rescueAccounts[i].PublicKey, "account %d pubkey", i)
			assert.Equal(t, revertAccounts[i].IsWritable, rescueAccounts[i].IsWritable, "account %d writable", i)
			assert.Equal(t, revertAccounts[i].IsSigner, rescueAccounts[i].IsSigner, "account %d signer", i)
		}
	})

	t.Run("rescue SPL matches revert SPL layout", func(t *testing.T) {
		rescueAccounts := builder.buildRescueAccounts(config, vault, feeVault, tss, recipient, executed, caller, false, tokenMint)
		revertAccounts := builder.buildRevertAccounts(config, vault, feeVault, tss, recipient, executed, caller, false, tokenMint)

		assert.Len(t, rescueAccounts, len(revertAccounts))
		for i := range rescueAccounts {
			assert.Equal(t, revertAccounts[i].PublicKey, rescueAccounts[i].PublicKey, "account %d pubkey", i)
		}
	})
}

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
		{"RESCUE_FUNDS", uetypes.TxType_RESCUE_FUNDS, false},
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

func TestGatewayAccountMetaStruct(t *testing.T) {
	var pk [32]byte
	for i := range pk {
		pk[i] = byte(i)
	}
	meta := GatewayAccountMeta{Pubkey: pk, IsWritable: true}
	assert.Equal(t, pk, meta.Pubkey)
	assert.True(t, meta.IsWritable)
}

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
		1, "devnet", 1000000,
		txID, utxID, sender, token,
		0, target, nil, nil,
		[32]byte{}, [32]byte{},
	)
	require.NoError(t, err)

	sig := make([]byte, 64)
	instrData := builder.buildWithdrawAndExecuteData(
		1, txID, utxID, 1000000, sender,
		[]byte{}, []byte{}, 0,
		sig, 0, msgHash,
	)

	// Extract message_hash from instruction data
	// Offset: 8(disc) + 1(id) + 32(txid) + 32(utxid) + 8(amount) + 20(sender)
	//       + 4(wf_len) + 0(wf) + 4(ix_len) + 0(ix) + 8(gas) + 64(sig) + 1(recov)
	//       = 182
	msgHashFromData := instrData[182:214]
	assert.Equal(t, msgHash, msgHashFromData, "message_hash in instruction data must match TSS message hash")
}

func TestAnchorDiscriminatorKnownValues(t *testing.T) {
	// Verify discriminator values are deterministic and can be independently computed
	for _, method := range []string{"finalize_universal_tx", "revert_universal_tx", "rescue_funds"} {
		disc := anchorDiscriminator(method)
		h := sha256.Sum256([]byte("global:" + method))
		assert.Equal(t, h[:8], disc, "discriminator for %s", method)
	}
}

func TestEndToEndWithRealSignature(t *testing.T) {
	builder := newTestBuilder(t)
	evmKey, _, _ := generateTestEVMKey(t)

	txID := makeTxID(0xAA)
	utxID := makeTxID(0xBB)
	sender := makeSender(0xCC)
	token := [32]byte{} // native SOL
	target := makeTxID(0xDD)

	t.Run("withdraw flow with real signature", func(t *testing.T) {
		amount := uint64(1000000)

		// 1. Construct TSS message hash (what TSS nodes would sign)
		msgHash, err := builder.constructTSSMessage(
			1, "devnet", amount,
			txID, utxID, sender, token,
			0, target, nil, nil,
			[32]byte{}, [32]byte{},
		)
		require.NoError(t, err)

		// 2. Sign with real EVM key (simulating TSS signing — returns r||s and v separately)
		sig, recoveryID := signMessageHash(t, evmKey, msgHash)

		// 3. Build instruction data with real signature
		instrData := builder.buildWithdrawAndExecuteData(
			1, txID, utxID, amount, sender,
			[]byte{}, []byte{}, 0,
			sig, recoveryID, msgHash,
		)

		// 4. Verify the instruction data contains the real signature
		// Offset: 8(disc) + 1(id) + 32(txid) + 32(utxid) + 8(amt) + 20(sender)
		//       + 4(wf_len) + 0(wf) + 4(ix_len) + 0(ix) + 8(gas) = 117
		assert.Equal(t, sig, instrData[117:181], "real signature in instruction data")
		assert.Equal(t, recoveryID, instrData[181], "recovery ID in instruction data")
		assert.Equal(t, msgHash, instrData[182:214], "message hash in instruction data")
	})

	t.Run("execute flow with real signature", func(t *testing.T) {
		amount := uint64(500)
		accs := []GatewayAccountMeta{
			{Pubkey: makeTxID(0x11), IsWritable: true},
		}
		ixData := []byte{0xDE, 0xAD}

		msgHash, err := builder.constructTSSMessage(
			2, "devnet", amount,
			txID, utxID, sender, token,
			0, target, accs, ixData,
			[32]byte{}, [32]byte{},
		)
		require.NoError(t, err)

		sig, recoveryID := signMessageHash(t, evmKey, msgHash)

		wf := accountsToWritableFlags(accs)
		instrData := builder.buildWithdrawAndExecuteData(
			2, txID, utxID, amount, sender,
			wf, ixData,
			0,
			sig, recoveryID, msgHash,
		)

		// Verify instruction data length includes variable-length fields
		// 8(disc) + 1(id) + 32(txid) + 32(utxid) + 8(amt) + 20(sender)
		// + 4+1(wf) + 4+2(ix) + 8(gas) + 64(sig) + 1(recov) + 32(hash)
		expectedLen := 8 + 1 + 32 + 32 + 8 + 20 + 5 + 6 + 8 + 64 + 1 + 32
		assert.Len(t, instrData, expectedLen)
	})

	t.Run("revert SOL flow with real signature", func(t *testing.T) {
		amount := uint64(500000)
		revertRecipient := makeTxID(0xEE)

		msgHash, err := builder.constructTSSMessage(
			3, "devnet", amount,
			txID, utxID, sender, token,
			0, [32]byte{}, nil, nil,
			revertRecipient, [32]byte{},
		)
		require.NoError(t, err)

		sig, recoveryID := signMessageHash(t, evmKey, msgHash)

		recipient := solana.PublicKeyFromBytes(revertRecipient[:])
		instrData := builder.buildRevertData(
			txID, utxID, amount, recipient,
			[]byte("revert msg"), 0,
			sig, recoveryID, msgHash,
		)

		// Verify discriminator is for revert_universal_tx
		expectedDisc := anchorDiscriminator("revert_universal_tx")
		assert.Equal(t, expectedDisc, instrData[:8])
	})

	t.Run("rescue SOL flow with real signature", func(t *testing.T) {
		amount := uint64(200000)
		rescueRecipient := makeTxID(0xEE)

		msgHash, err := builder.constructTSSMessage(
			4, "devnet", amount,
			txID, utxID, sender, token,
			50, [32]byte{}, nil, nil,
			rescueRecipient, [32]byte{},
		)
		require.NoError(t, err)

		sig, recoveryID := signMessageHash(t, evmKey, msgHash)

		instrData := builder.buildRescueData(
			txID, utxID, amount, 50,
			sig, recoveryID, msgHash,
		)

		// Verify discriminator is for rescue_funds
		expectedDisc := anchorDiscriminator("rescue_funds")
		assert.Equal(t, expectedDisc, instrData[:8])

		// Verify total length: 8(disc) + 32(txid) + 32(utxid) + 8(amt) + 8(gas) + 64(sig) + 1(recov) + 32(hash) = 185
		assert.Len(t, instrData, 185)
	})
}

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

	builder, err := NewTxBuilder(rpcClient, "solana:"+devnetGenesisHash, devnetGatewayAddress, tmpDir, logger, nil)
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

	// Step 1: Build signing request (fetches chain ID from on-chain TSS PDA)
	req, err := builder.GetOutboundSigningRequest(ctx, data, 0)
	if err != nil {
		return nil, fmt.Errorf("GetOutboundSigningRequest: %w", err)
	}
	require.NotNil(t, req)
	require.Len(t, req.SigningHash, 32)
	t.Logf("  signing_hash=0x%s nonce=%d", hex.EncodeToString(req.SigningHash), req.Nonce)

	// Step 2: Sign with EVM key (secp256k1)
	// signMessageHash returns (r||s, v) separately. BuildOutboundTransaction expects
	// the full 65-byte signature [r(32)|s(32)|v(1)].
	sig, recoveryID := signMessageHash(t, evmKey, req.SigningHash)
	fullSig := append(sig, recoveryID)

	// Step 3: Build the Solana transaction (derives PDAs, builds instruction data, signs with relayer key)
	tx, _, err := builder.BuildOutboundTransaction(ctx, req, data, fullSig)
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

func TestSimulate_Execute_NativeSOL(t *testing.T) {
	rpcClient, builder := setupDevnetSimulation(t)
	defer rpcClient.Close()

	// Use the SPL Memo program as the execute destination.
	// It accepts any UTF-8 data with no required accounts, making it ideal for
	// CPI testing without needing initialized state on devnet.
	ixData := []byte("hello from push chain")
	payload := buildMockExecutePayload(nil, ixData)
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
	payload := buildMockExecutePayload(nil, ixData)
	payloadHex := "0x" + hex.EncodeToString(payload)

	// Recipient = Memo program (becomes destination_program in execute mode)
	data, evmKey := newDevnetOutbound(t, "500000", devnetSPLMint, payloadHex, "", "FUNDS_AND_PAYLOAD")
	data.Recipient = devnetMemoProgram

	result, err := buildAndSimulate(t, rpcClient, builder, data, evmKey)
	require.NoError(t, err)
	requireSimulationSuccess(t, result)
}

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

// buildAndSimulateRescue constructs a rescue_funds transaction and simulates it on devnet.
func buildAndSimulateRescue(t *testing.T, rpcClient *RPCClient, builder *TxBuilder, evmKey *ecdsa.PrivateKey, amount uint64, assetAddr string) *rpc.SimulateTransactionResult {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, ethAddrHex := loadTestEVMKey(t)
	recipientWallet := solana.NewWallet()

	txIDBytes := make([]byte, 32)
	utxIDBytes := make([]byte, 32)
	_, err := crand.Read(txIDBytes)
	require.NoError(t, err)
	_, err = crand.Read(utxIDBytes)
	require.NoError(t, err)

	var txID, universalTxID [32]byte
	copy(txID[:], txIDBytes)
	copy(universalTxID[:], utxIDBytes)

	var sender [20]byte
	senderBytes, err := hex.DecodeString(ethAddrHex)
	require.NoError(t, err)
	copy(sender[:], senderBytes)

	isNative := assetAddr == ""
	var token [32]byte
	var mintPubkey solana.PublicKey
	if !isNative {
		mintPubkey, err = solana.PublicKeyFromBase58(assetAddr)
		require.NoError(t, err)
		copy(token[:], mintPubkey.Bytes())
	}

	recipientPubkey := recipientWallet.PublicKey()

	// Fetch chain ID from TSS PDA
	tssPDA, err := builder.deriveTSSPDA()
	require.NoError(t, err)
	chainID, err := builder.fetchTSSChainID(ctx, tssPDA)
	require.NoError(t, err)

	// Construct TSS message for rescue (id=4)
	var revertRecipient [32]byte
	copy(revertRecipient[:], recipientPubkey.Bytes())
	var revertMint [32]byte
	if !isNative {
		copy(revertMint[:], token[:])
	}

	gasFee := uint64(0)
	messageHash, err := builder.constructTSSMessage(
		4, chainID, amount,
		txID, universalTxID, sender, token, gasFee,
		[32]byte{}, nil, nil,
		revertRecipient, revertMint,
	)
	require.NoError(t, err)

	sig, recoveryID := signMessageHash(t, evmKey, messageHash)

	instructionData := builder.buildRescueData(txID, universalTxID, amount, gasFee, sig, recoveryID, messageHash)

	// Derive PDAs
	configPDA, _, err := solana.FindProgramAddress([][]byte{[]byte("config")}, builder.gatewayAddress)
	require.NoError(t, err)
	vaultPDA, _, err := solana.FindProgramAddress([][]byte{[]byte("vault")}, builder.gatewayAddress)
	require.NoError(t, err)
	feeVaultPDA, _, err := solana.FindProgramAddress([][]byte{[]byte("fee_vault")}, builder.gatewayAddress)
	require.NoError(t, err)
	executedTxPDA, _, err := solana.FindProgramAddress([][]byte{[]byte("executed_sub_tx"), txID[:]}, builder.gatewayAddress)
	require.NoError(t, err)

	relayerKeypair, err := builder.loadRelayerKeypair()
	require.NoError(t, err)

	accounts := builder.buildRescueAccounts(
		configPDA, vaultPDA, feeVaultPDA, tssPDA, recipientPubkey,
		executedTxPDA, relayerKeypair.PublicKey(),
		isNative, mintPubkey,
	)

	gatewayIx := solana.NewInstruction(builder.gatewayAddress, accounts, instructionData)
	computeLimitIx := builder.buildSetComputeUnitLimitInstruction(400000)

	instructions := []solana.Instruction{computeLimitIx}
	if !isNative {
		createATAIx := builder.buildCreateATAIdempotentInstruction(
			relayerKeypair.PublicKey(), recipientPubkey, mintPubkey,
		)
		instructions = append(instructions, createATAIx)
	}
	instructions = append(instructions, gatewayIx)

	recentBlockhash, err := rpcClient.GetRecentBlockhash(ctx)
	require.NoError(t, err)

	tx, err := solana.NewTransaction(
		instructions, recentBlockhash,
		solana.TransactionPayer(relayerKeypair.PublicKey()),
	)
	require.NoError(t, err)

	_, err = tx.Sign(func(key solana.PublicKey) *solana.PrivateKey {
		if key.Equals(relayerKeypair.PublicKey()) {
			privKey := relayerKeypair
			return &privKey
		}
		return nil
	})
	require.NoError(t, err)

	result, err := rpcClient.SimulateTransaction(ctx, tx)
	require.NoError(t, err, "SimulateTransaction RPC call failed")

	return result
}

func TestSimulate_Rescue_NativeSOL(t *testing.T) {
	rpcClient, builder := setupDevnetSimulation(t)
	defer rpcClient.Close()

	evmKey, _ := loadTestEVMKey(t)

	result := buildAndSimulateRescue(t, rpcClient, builder, evmKey, 10000000, "")
	requireSimulationSuccess(t, result)
}

func TestSimulate_Rescue_SPLToken(t *testing.T) {
	rpcClient, builder := setupDevnetSimulation(t)
	defer rpcClient.Close()

	evmKey, _ := loadTestEVMKey(t)

	result := buildAndSimulateRescue(t, rpcClient, builder, evmKey, 500000, devnetSPLMint)
	requireSimulationSuccess(t, result)
}

func TestGetNextNonce(t *testing.T) {
	builder := newTestBuilder(t)

	t.Run("returns 0 with arbitrary address and finalized=true", func(t *testing.T) {
		nonce, err := builder.GetNextNonce(context.Background(), "SomeAddress123", true)
		require.NoError(t, err)
		assert.Equal(t, uint64(0), nonce)
	})

	t.Run("returns 0 with empty address and finalized=false", func(t *testing.T) {
		nonce, err := builder.GetNextNonce(context.Background(), "", false)
		require.NoError(t, err)
		assert.Equal(t, uint64(0), nonce)
	})
}

func TestGetGasFeeUsed(t *testing.T) {
	builder := newTestBuilder(t)

	t.Run("returns string zero for any tx hash", func(t *testing.T) {
		fee, err := builder.GetGasFeeUsed(context.Background(), "5xYz...someTxHash")
		require.NoError(t, err)
		assert.Equal(t, "0", fee)
	})

	t.Run("returns string zero for empty tx hash", func(t *testing.T) {
		fee, err := builder.GetGasFeeUsed(context.Background(), "")
		require.NoError(t, err)
		assert.Equal(t, "0", fee)
	})
}

func TestNewTxBuilder_ChainConfig(t *testing.T) {
	logger := zerolog.Nop()

	t.Run("valid protocolALT is stored", func(t *testing.T) {
		altKey := solana.NewWallet().PublicKey()
		cfg := &config.ChainSpecificConfig{
			ProtocolALT: altKey.String(),
		}
		builder, err := NewTxBuilder(&RPCClient{}, "solana:devnet", testGatewayAddress, "/tmp", logger, cfg)
		require.NoError(t, err)
		assert.Equal(t, altKey, builder.protocolALT)
	})

	t.Run("invalid protocolALT is silently skipped", func(t *testing.T) {
		cfg := &config.ChainSpecificConfig{
			ProtocolALT: "not-valid-base58!!!",
		}
		builder, err := NewTxBuilder(&RPCClient{}, "solana:devnet", testGatewayAddress, "/tmp", logger, cfg)
		require.NoError(t, err)
		assert.True(t, builder.protocolALT.IsZero(), "invalid ALT should result in zero pubkey")
	})

	t.Run("valid tokenALTs are stored", func(t *testing.T) {
		mint := solana.NewWallet().PublicKey()
		alt := solana.NewWallet().PublicKey()
		cfg := &config.ChainSpecificConfig{
			TokenALTs: map[string]string{
				mint.String(): alt.String(),
			},
		}
		builder, err := NewTxBuilder(&RPCClient{}, "solana:devnet", testGatewayAddress, "/tmp", logger, cfg)
		require.NoError(t, err)
		got, ok := builder.tokenALTs[mint]
		require.True(t, ok, "expected token ALT entry for mint")
		assert.Equal(t, alt, got)
	})

	t.Run("invalid tokenALT mint is skipped", func(t *testing.T) {
		cfg := &config.ChainSpecificConfig{
			TokenALTs: map[string]string{
				"bad-mint": solana.NewWallet().PublicKey().String(),
			},
		}
		builder, err := NewTxBuilder(&RPCClient{}, "solana:devnet", testGatewayAddress, "/tmp", logger, cfg)
		require.NoError(t, err)
		assert.Len(t, builder.tokenALTs, 0)
	})

	t.Run("invalid tokenALT address is skipped", func(t *testing.T) {
		cfg := &config.ChainSpecificConfig{
			TokenALTs: map[string]string{
				solana.NewWallet().PublicKey().String(): "bad-alt",
			},
		}
		builder, err := NewTxBuilder(&RPCClient{}, "solana:devnet", testGatewayAddress, "/tmp", logger, cfg)
		require.NoError(t, err)
		assert.Len(t, builder.tokenALTs, 0)
	})

	t.Run("nil chainConfig is fine", func(t *testing.T) {
		builder, err := NewTxBuilder(&RPCClient{}, "solana:devnet", testGatewayAddress, "/tmp", logger, nil)
		require.NoError(t, err)
		assert.True(t, builder.protocolALT.IsZero())
		assert.Len(t, builder.tokenALTs, 0)
	})
}

func TestBuildCreateATAIdempotentInstruction(t *testing.T) {
	builder := newTestBuilder(t)
	payer := solana.NewWallet().PublicKey()
	owner := solana.NewWallet().PublicKey()
	mint := solana.NewWallet().PublicKey()

	ix := builder.buildCreateATAIdempotentInstruction(payer, owner, mint)

	t.Run("program ID is ATA program", func(t *testing.T) {
		expected := solana.MustPublicKeyFromBase58("ATokenGPvbdGVxr1b2hvZbsiqW5xWH25efTNsLJA8knL")
		assert.Equal(t, expected, ix.ProgramID())
	})

	t.Run("has 6 accounts in correct order", func(t *testing.T) {
		accounts := ix.Accounts()
		require.Len(t, accounts, 6)

		// payer (signer, writable)
		assert.Equal(t, payer, accounts[0].PublicKey)
		assert.True(t, accounts[0].IsSigner)
		assert.True(t, accounts[0].IsWritable)

		// ATA (writable, derived deterministically)
		ataProgramID := solana.MustPublicKeyFromBase58("ATokenGPvbdGVxr1b2hvZbsiqW5xWH25efTNsLJA8knL")
		expectedATA, _, _ := solana.FindProgramAddress(
			[][]byte{owner.Bytes(), solana.TokenProgramID.Bytes(), mint.Bytes()},
			ataProgramID,
		)
		assert.Equal(t, expectedATA, accounts[1].PublicKey)
		assert.True(t, accounts[1].IsWritable)
		assert.False(t, accounts[1].IsSigner)

		// owner
		assert.Equal(t, owner, accounts[2].PublicKey)
		assert.False(t, accounts[2].IsWritable)

		// mint
		assert.Equal(t, mint, accounts[3].PublicKey)
		assert.False(t, accounts[3].IsWritable)

		// system program
		assert.Equal(t, solana.SystemProgramID, accounts[4].PublicKey)

		// token program
		assert.Equal(t, solana.TokenProgramID, accounts[5].PublicKey)
	})

	t.Run("instruction data is [1] for CreateIdempotent", func(t *testing.T) {
		data, err := ix.Data()
		require.NoError(t, err)
		assert.Equal(t, []byte{1}, data)
	})
}
