package svm

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/gagliardetto/solana-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func packPC20Payload(t *testing.T, name, symbol string, decimals uint8, userData []byte) []byte {
	t.Helper()
	require.NoError(t, pc20ExportABIErr)
	encoded, err := pc20ExportABIArgs.Pack(name, symbol, decimals, userData)
	require.NoError(t, err)
	return append(append([]byte{}, pc20Selector[:]...), encoded...)
}

func TestIsPC20Payload(t *testing.T) {
	assert.True(t, isPC20Payload([]byte("PC20")))
	assert.True(t, isPC20Payload([]byte{0x50, 0x43, 0x32, 0x30, 0xff}))
	assert.False(t, isPC20Payload([]byte("PC2")))
	assert.False(t, isPC20Payload([]byte{0x50, 0x43, 0x32, 0x31}))
	assert.False(t, isPC20Payload(nil))
}

func TestParsePC20ExportPayload_RoundTrip(t *testing.T) {
	userData := []byte{0xde, 0xad, 0xbe, 0xef}
	payload := packPC20Payload(t, "PC BOB", "pBOB", 9, userData)

	meta, err := parsePC20ExportPayload(payload)
	require.NoError(t, err)
	assert.Equal(t, "PC BOB", meta.Name)
	assert.Equal(t, "pBOB", meta.Symbol)
	assert.Equal(t, uint8(9), meta.Decimals)
	assert.Equal(t, userData, meta.UserData)
}

func TestParsePC20ExportPayload_EmptyUserData(t *testing.T) {
	payload := packPC20Payload(t, "Token", "TKN", 18, []byte{})
	meta, err := parsePC20ExportPayload(payload)
	require.NoError(t, err)
	assert.Empty(t, meta.UserData)
}

func TestParsePC20ExportPayload_Errors(t *testing.T) {
	_, err := parsePC20ExportPayload([]byte("nope"))
	assert.Error(t, err)

	_, err = parsePC20ExportPayload([]byte{0x50, 0x43, 0x32, 0x30, 0x01, 0x02})
	assert.Error(t, err)
}

func TestBuildPC20ExportIxData_Golden(t *testing.T) {
	sourceAsset := makeSender(0xaa)
	meta := &pc20ExportMeta{Name: "AB", Symbol: "C", Decimals: 6, UserData: []byte{0x01, 0x02}}

	got := buildPC20ExportIxData(sourceAsset, meta)

	// selector || source_asset(20) || borsh String name || borsh String symbol || u8 || borsh Vec<u8>
	expected := []byte{0x50, 0x43, 0x32, 0x30}
	expected = append(expected, sourceAsset[:]...)
	expected = append(expected, 0x02, 0x00, 0x00, 0x00, 'A', 'B')
	expected = append(expected, 0x01, 0x00, 0x00, 0x00, 'C')
	expected = append(expected, 0x06)
	expected = append(expected, 0x02, 0x00, 0x00, 0x00, 0x01, 0x02)

	assert.Equal(t, expected, got)
}

func TestConstructPC20ExportTSSMessage(t *testing.T) {
	chainID := "solana-devnet"
	deadline := int64(1_752_000_000)
	amount := uint64(10_000_000)
	txID := makeTxID(0x11)
	utxID := makeTxID(0x22)
	pushAccount := makeSender(0x33)
	sourceAsset := makeSender(0x44)
	recipient := solana.MustPublicKeyFromBase58(testGatewayAddress)
	gasFee := uint64(5_000_000)

	expectedFor := func(meta *pc20ExportMeta) []byte {
		msg := []byte("PUSH_CHAIN_SVM")
		msg = append(msg, 5)
		msg = append(msg, []byte(chainID)...)
		buf8 := make([]byte, 8)
		binary.BigEndian.PutUint64(buf8, uint64(deadline))
		msg = append(msg, buf8...)
		binary.BigEndian.PutUint64(buf8, amount)
		msg = append(msg, buf8...)
		msg = append(msg, txID[:]...)
		msg = append(msg, utxID[:]...)
		msg = append(msg, pushAccount[:]...)
		msg = append(msg, sourceAsset[:]...)
		msg = append(msg, recipient.Bytes()...)
		buf4 := make([]byte, 4)
		binary.BigEndian.PutUint32(buf4, uint32(len(meta.Name)))
		msg = append(msg, buf4...)
		msg = append(msg, []byte(meta.Name)...)
		binary.BigEndian.PutUint32(buf4, uint32(len(meta.Symbol)))
		msg = append(msg, buf4...)
		msg = append(msg, []byte(meta.Symbol)...)
		msg = append(msg, meta.Decimals)
		binary.BigEndian.PutUint64(buf8, gasFee)
		msg = append(msg, buf8...)
		if len(meta.UserData) > 0 {
			binary.BigEndian.PutUint32(buf4, uint32(len(meta.UserData)))
			msg = append(msg, buf4...)
			msg = append(msg, meta.UserData...)
		}
		return crypto.Keccak256(msg)
	}

	t.Run("direct export (no user_data)", func(t *testing.T) {
		meta := &pc20ExportMeta{Name: "PC BOB", Symbol: "pBOB", Decimals: 9}
		got := constructPC20ExportTSSMessage(chainID, deadline, amount, txID, utxID, pushAccount, sourceAsset, recipient, meta, gasFee)
		assert.Equal(t, expectedFor(meta), got)
	})

	t.Run("payload export (with user_data)", func(t *testing.T) {
		meta := &pc20ExportMeta{Name: "PC BOB", Symbol: "pBOB", Decimals: 9, UserData: []byte{0x0a, 0x0b, 0x0c}}
		got := constructPC20ExportTSSMessage(chainID, deadline, amount, txID, utxID, pushAccount, sourceAsset, recipient, meta, gasFee)
		assert.Equal(t, expectedFor(meta), got)

		noUserData := &pc20ExportMeta{Name: "PC BOB", Symbol: "pBOB", Decimals: 9}
		assert.NotEqual(t, expectedFor(noUserData), got)
	})
}

func TestConstructPC20RemintTSSMessage(t *testing.T) {
	chainID := "solana-devnet"
	deadline := int64(1_752_000_000)
	amount := uint64(2_000_000)
	txID := makeTxID(0x55)
	utxID := makeTxID(0x66)
	var mint, recipient [32]byte
	copy(mint[:], bytes.Repeat([]byte{0x77}, 32))
	copy(recipient[:], bytes.Repeat([]byte{0x88}, 32))
	gasFee := uint64(1_000_000)
	revertMsg := []byte("failed")
	sourceAsset := makeSender(0x99)

	expectedFor := func(id uint8, withRevertMsg bool) []byte {
		msg := []byte("PUSH_CHAIN_SVM")
		msg = append(msg, id)
		msg = append(msg, []byte(chainID)...)
		buf8 := make([]byte, 8)
		binary.BigEndian.PutUint64(buf8, uint64(deadline))
		msg = append(msg, buf8...)
		binary.BigEndian.PutUint64(buf8, amount)
		msg = append(msg, buf8...)
		msg = append(msg, txID[:]...)
		msg = append(msg, utxID[:]...)
		msg = append(msg, mint[:]...)
		msg = append(msg, recipient[:]...)
		binary.BigEndian.PutUint64(buf8, gasFee)
		msg = append(msg, buf8...)
		if withRevertMsg {
			msg = append(msg, crypto.Keccak256(revertMsg)...)
		}
		msg = append(msg, []byte("PC20")...)
		msg = append(msg, sourceAsset[:]...)
		return crypto.Keccak256(msg)
	}

	t.Run("revert (id=3) binds keccak(revert_msg)", func(t *testing.T) {
		got, err := constructPC20RemintTSSMessage(3, chainID, deadline, amount, txID, utxID, mint, recipient, gasFee, revertMsg, sourceAsset)
		require.NoError(t, err)
		assert.Equal(t, expectedFor(3, true), got)
	})

	t.Run("rescue (id=4) has no revert_msg", func(t *testing.T) {
		got, err := constructPC20RemintTSSMessage(4, chainID, deadline, amount, txID, utxID, mint, recipient, gasFee, nil, sourceAsset)
		require.NoError(t, err)
		assert.Equal(t, expectedFor(4, false), got)
	})

	t.Run("invalid instruction id", func(t *testing.T) {
		_, err := constructPC20RemintTSSMessage(5, chainID, deadline, amount, txID, utxID, mint, recipient, gasFee, nil, sourceAsset)
		assert.Error(t, err)
	})
}

func TestBuildPC20ExportAccounts_Direct(t *testing.T) {
	tb := newTestBuilder(t)
	caller := solana.MustPublicKeyFromBase58("So11111111111111111111111111111111111111112")
	configPDA, _, _ := solana.FindProgramAddress([][]byte{configSeed}, tb.gatewayAddress)
	vaultPDA, _, _ := solana.FindProgramAddress([][]byte{vaultSeed}, tb.gatewayAddress)
	tssPDA, _, _ := solana.FindProgramAddress([][]byte{tssSeed}, tb.gatewayAddress)
	txID := makeTxID(0x01)
	executedTxPDA, _, _ := solana.FindProgramAddress([][]byte{executedSubTxSeed, txID[:]}, tb.gatewayAddress)
	ceaSender := makeSender(0x02)
	cea, _, _ := solana.FindProgramAddress([][]byte{ceaAuthoritySeed, ceaSender[:]}, tb.gatewayAddress)
	recipient := solana.MustPublicKeyFromBase58("Vote111111111111111111111111111111111111111")

	sourceAsset := makeSender(0x03)
	mint, err := tb.derivePC20MintPDA(sourceAsset)
	require.NoError(t, err)
	state, err := tb.derivePC20StatePDA(mint)
	require.NoError(t, err)

	accounts, err := tb.buildPC20ExportAccounts(
		caller, configPDA, vaultPDA, cea, tssPDA, executedTxPDA,
		solana.SystemProgramID, recipient, mint, state, false, nil,
	)
	require.NoError(t, err)

	// 20 typed slots + [pc20_state, pc20_mint, recipient_ata]
	require.Len(t, accounts, 23)

	assert.Equal(t, caller, accounts[0].PublicKey)
	assert.True(t, accounts[0].IsSigner)
	assert.Equal(t, solana.SystemProgramID, accounts[7].PublicKey) // destination_program
	assert.Equal(t, recipient, accounts[8].PublicKey)
	assert.True(t, accounts[8].IsWritable)

	// None sentinels: vault_ata(9), cea_ata(10), mint(11), recipient_ata(16), rate limits(17,18), ref slots(19,20)
	for _, idx := range []int{9, 10, 11, 16, 17, 18, 19} {
		assert.Equal(t, tb.gatewayAddress, accounts[idx].PublicKey, "slot %d must be None sentinel", idx)
	}
	assert.Equal(t, solana.TokenProgramID, accounts[12].PublicKey)
	assert.Equal(t, solana.SysVarRentPubkey, accounts[13].PublicKey)
	assert.Equal(t, solana.SPLAssociatedTokenAccountProgramID, accounts[14].PublicKey)

	// Remaining accounts
	assert.Equal(t, state, accounts[20].PublicKey)
	assert.True(t, accounts[20].IsWritable)
	assert.Equal(t, mint, accounts[21].PublicKey)
	assert.True(t, accounts[21].IsWritable)
	expectedATA, _, _ := solana.FindProgramAddress(
		[][]byte{recipient.Bytes(), solana.TokenProgramID.Bytes(), mint.Bytes()},
		solana.SPLAssociatedTokenAccountProgramID,
	)
	assert.Equal(t, expectedATA, accounts[22].PublicKey)
	assert.True(t, accounts[22].IsWritable)
}

func TestBuildPC20ExportAccounts_WithPayload(t *testing.T) {
	tb := newTestBuilder(t)
	ceaSender := makeSender(0x02)
	cea, _, _ := solana.FindProgramAddress([][]byte{ceaAuthoritySeed, ceaSender[:]}, tb.gatewayAddress)
	recipient := solana.MustPublicKeyFromBase58("Vote111111111111111111111111111111111111111")
	target := solana.MustPublicKeyFromBase58("So11111111111111111111111111111111111111112")

	sourceAsset := makeSender(0x03)
	mint, err := tb.derivePC20MintPDA(sourceAsset)
	require.NoError(t, err)
	state, err := tb.derivePC20StatePDA(mint)
	require.NoError(t, err)

	var execAcc GatewayAccountMeta
	copy(execAcc.Pubkey[:], recipient.Bytes())
	execAcc.IsWritable = true

	accounts, err := tb.buildPC20ExportAccounts(
		recipient, recipient, recipient, cea, recipient, recipient,
		target, recipient, mint, state, true, []GatewayAccountMeta{execAcc},
	)
	require.NoError(t, err)

	// 20 typed slots + [pc20_state, pc20_mint] + 1 payload account
	require.Len(t, accounts, 23)
	assert.Equal(t, target, accounts[7].PublicKey) // destination_program = payload target

	// cea_ata (slot 10) is the CEA's ATA, writable
	expectedCeaATA, _, _ := solana.FindProgramAddress(
		[][]byte{cea.Bytes(), solana.TokenProgramID.Bytes(), mint.Bytes()},
		solana.SPLAssociatedTokenAccountProgramID,
	)
	assert.Equal(t, expectedCeaATA, accounts[10].PublicKey)
	assert.True(t, accounts[10].IsWritable)

	// remaining: state, mint, then payload accounts (no recipient_ata)
	assert.Equal(t, state, accounts[20].PublicKey)
	assert.Equal(t, mint, accounts[21].PublicKey)
	assert.Equal(t, recipient, accounts[22].PublicKey)
	assert.True(t, accounts[22].IsWritable)
}

func TestBuildPC20RemintAccounts(t *testing.T) {
	tb := newTestBuilder(t)
	configPDA, _, _ := solana.FindProgramAddress([][]byte{configSeed}, tb.gatewayAddress)
	vaultPDA, _, _ := solana.FindProgramAddress([][]byte{vaultSeed}, tb.gatewayAddress)
	feeVaultPDA, _, _ := solana.FindProgramAddress([][]byte{feeVaultSeed}, tb.gatewayAddress)
	tssPDA, _, _ := solana.FindProgramAddress([][]byte{tssSeed}, tb.gatewayAddress)
	txID := makeTxID(0x01)
	executedTxPDA, _, _ := solana.FindProgramAddress([][]byte{executedSubTxSeed, txID[:]}, tb.gatewayAddress)
	caller := solana.MustPublicKeyFromBase58("So11111111111111111111111111111111111111112")
	recipient := solana.MustPublicKeyFromBase58("Vote111111111111111111111111111111111111111")

	sourceAsset := makeSender(0x03)
	mint, err := tb.derivePC20MintPDA(sourceAsset)
	require.NoError(t, err)
	state, err := tb.derivePC20StatePDA(mint)
	require.NoError(t, err)

	accounts, err := tb.buildPC20RemintAccounts(
		configPDA, vaultPDA, feeVaultPDA, tssPDA, recipient, executedTxPDA, caller, mint,
	)
	require.NoError(t, err)

	// 12 typed slots + 5 remaining
	require.Len(t, accounts, 17)

	assert.Equal(t, configPDA, accounts[0].PublicKey)
	assert.Equal(t, recipient, accounts[4].PublicKey)
	assert.Equal(t, caller, accounts[6].PublicKey)
	assert.True(t, accounts[6].IsSigner)

	// token_vault(8) and recipient_token_account(9) are None sentinels
	assert.Equal(t, tb.gatewayAddress, accounts[8].PublicKey)
	assert.Equal(t, tb.gatewayAddress, accounts[9].PublicKey)
	assert.Equal(t, mint, accounts[10].PublicKey)
	assert.Equal(t, solana.TokenProgramID, accounts[11].PublicKey)

	// remaining: [pc20_state(ro), pc20_mint(w), recipient_ata(w), ATA program(ro), rent(ro)]
	assert.Equal(t, state, accounts[12].PublicKey)
	assert.False(t, accounts[12].IsWritable)
	assert.Equal(t, mint, accounts[13].PublicKey)
	assert.True(t, accounts[13].IsWritable)
	expectedATA, _, _ := solana.FindProgramAddress(
		[][]byte{recipient.Bytes(), solana.TokenProgramID.Bytes(), mint.Bytes()},
		solana.SPLAssociatedTokenAccountProgramID,
	)
	assert.Equal(t, expectedATA, accounts[14].PublicKey)
	assert.True(t, accounts[14].IsWritable)
	assert.Equal(t, solana.SPLAssociatedTokenAccountProgramID, accounts[15].PublicKey)
	assert.False(t, accounts[15].IsWritable)
	assert.Equal(t, solana.SysVarRentPubkey, accounts[16].PublicKey)
	assert.False(t, accounts[16].IsWritable)
}

func TestValidatePC20UserData(t *testing.T) {
	assert.NoError(t, validatePC20UserData(nil))

	// Valid execute wire format: 0 accounts, 1-byte ix_data, id=2, target program
	var target [32]byte
	copy(target[:], solana.SystemProgramID.Bytes())
	userData := make([]byte, 0)
	userData = binary.BigEndian.AppendUint32(userData, 0) // accountsCount
	userData = binary.BigEndian.AppendUint32(userData, 1) // ixDataLen
	userData = append(userData, 0xaa)
	userData = append(userData, 2) // instruction_id
	userData = append(userData, target[:]...)
	assert.NoError(t, validatePC20UserData(userData))

	// Withdraw id inside user_data is invalid for PC20
	bad := make([]byte, 0)
	bad = binary.BigEndian.AppendUint32(bad, 0)
	bad = binary.BigEndian.AppendUint32(bad, 0)
	bad = append(bad, 1)
	bad = append(bad, target[:]...)
	assert.Error(t, validatePC20UserData(bad))

	assert.Error(t, validatePC20UserData([]byte{0x01}))
}

func TestParsePC20SourceAsset(t *testing.T) {
	sa, err := parsePC20SourceAsset("0x" + "11" + "22334455667788990011223344556677889900")
	require.NoError(t, err)
	assert.Equal(t, byte(0x11), sa[0])

	_, err = parsePC20SourceAsset("0x1234")
	assert.Error(t, err)

	_, err = parsePC20SourceAsset("0x0000000000000000000000000000000000000000")
	assert.Error(t, err)
}
