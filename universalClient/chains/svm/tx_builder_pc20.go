package svm

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"

	"github.com/pushchain/push-chain-node/universalClient/chains/common"
	uetypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// PC20 gateway-protocol values — must match pc20.rs / state.rs.
var (
	pc20Selector  = [4]byte{'P', 'C', '2', '0'} // 0x50433230
	pc20MintSeed  = []byte("pc20_mint")
	pc20StateSeed = []byte("pc20_state")
)

const (
	pc20FinalizeInstructionID uint8 = 5
	pc20StateAccountLen             = 8 + 20 + 32 + 1 + 1 // disc + source_asset + wrapped_mint + decimals + bump
)

// Core stores the vault-ready export payload: selector || abi.encode(name, symbol, decimals, userData).
var pc20ExportABIArgs, pc20ExportABIErr = buildPC20ExportABIArgs()

func buildPC20ExportABIArgs() (abi.Arguments, error) {
	stringT, err := abi.NewType("string", "", nil)
	if err != nil {
		return nil, err
	}
	uint8T, err := abi.NewType("uint8", "", nil)
	if err != nil {
		return nil, err
	}
	bytesT, err := abi.NewType("bytes", "", nil)
	if err != nil {
		return nil, err
	}
	return abi.Arguments{{Type: stringT}, {Type: stringT}, {Type: uint8T}, {Type: bytesT}}, nil
}

type pc20ExportMeta struct {
	Name     string
	Symbol   string
	Decimals uint8
	UserData []byte
}

func isPC20Payload(payload []byte) bool {
	return len(payload) >= len(pc20Selector) && bytes.Equal(payload[:len(pc20Selector)], pc20Selector[:])
}

// decodeHexPayload decodes an optionally 0x-prefixed hex string; nil on failure.
func decodeHexPayload(payload string) []byte {
	b, err := hex.DecodeString(removeHexPrefix(payload))
	if err != nil {
		return nil
	}
	return b
}

func parsePC20ExportPayload(payload []byte) (*pc20ExportMeta, error) {
	if !isPC20Payload(payload) {
		return nil, fmt.Errorf("payload does not start with PC20 selector")
	}
	if pc20ExportABIErr != nil {
		return nil, fmt.Errorf("pc20 abi init failed: %w", pc20ExportABIErr)
	}
	vals, err := pc20ExportABIArgs.Unpack(payload[len(pc20Selector):])
	if err != nil {
		return nil, fmt.Errorf("failed to abi-decode pc20 export payload: %w", err)
	}
	name, ok1 := vals[0].(string)
	symbol, ok2 := vals[1].(string)
	decimals, ok3 := vals[2].(uint8)
	userData, ok4 := vals[3].([]byte)
	if !ok1 || !ok2 || !ok3 || !ok4 {
		return nil, fmt.Errorf("unexpected pc20 export payload field types")
	}
	return &pc20ExportMeta{Name: name, Symbol: symbol, Decimals: decimals, UserData: userData}, nil
}

func parsePC20SourceAsset(assetAddr string) ([20]byte, error) {
	var sourceAsset [20]byte
	b, err := hex.DecodeString(removeHexPrefix(assetAddr))
	if err != nil || len(b) != 20 {
		return sourceAsset, fmt.Errorf("invalid pc20 source asset (expected 20-byte hex): %s", assetAddr)
	}
	copy(sourceAsset[:], b)
	if sourceAsset == ([20]byte{}) {
		return sourceAsset, fmt.Errorf("pc20 source asset is zero address")
	}
	return sourceAsset, nil
}

func parseSolanaAddress(addr string) (solana.PublicKey, error) {
	pk, err := solana.PublicKeyFromBase58(addr)
	if err == nil {
		return pk, nil
	}
	b, hexErr := hex.DecodeString(removeHexPrefix(addr))
	if hexErr != nil || len(b) != 32 {
		return solana.PublicKey{}, fmt.Errorf("invalid solana address: %s", addr)
	}
	return solana.PublicKeyFromBytes(b), nil
}

func parseGasFee(gasFee string) uint64 {
	if gasFee == "" {
		return 0
	}
	v, _ := strconv.ParseUint(gasFee, 10, 64)
	return v
}

// buildPC20ExportIxData builds ix_data for the finalize id=5 route:
// selector || borsh(Pc20ExportIxData{source_asset, name, symbol, decimals, user_data}).
func buildPC20ExportIxData(sourceAsset [20]byte, m *pc20ExportMeta) []byte {
	data := make([]byte, 0, 4+20+4+len(m.Name)+4+len(m.Symbol)+1+4+len(m.UserData))
	data = append(data, pc20Selector[:]...)
	data = append(data, sourceAsset[:]...)
	data = appendBorshBytes(data, []byte(m.Name))
	data = appendBorshBytes(data, []byte(m.Symbol))
	data = append(data, m.Decimals)
	data = appendBorshBytes(data, m.UserData)
	return data
}

// appendBorshBytes appends a Borsh Vec<u8>/String: u32 LE length + raw bytes.
func appendBorshBytes(dst, b []byte) []byte {
	l := make([]byte, 4)
	binary.LittleEndian.PutUint32(l, uint32(len(b)))
	dst = append(dst, l...)
	return append(dst, b...)
}

// appendU32BEBytes appends serialize_string/serialize_ix_data framing: u32 BE length + raw bytes.
func appendU32BEBytes(dst, b []byte) []byte {
	l := make([]byte, 4)
	binary.BigEndian.PutUint32(l, uint32(len(b)))
	dst = append(dst, l...)
	return append(dst, b...)
}

func appendU64BE(dst []byte, v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	return append(dst, b...)
}

// constructPC20ExportTSSMessage mirrors build_and_validate_pc20_finalize_tss (pc20.rs):
// PREFIX || 5 || chain_id || deadline(i64 BE) || amount(u64 BE) || sub_tx_id || universal_tx_id
// || push_account || source_asset || recipient || len(name)||name || len(symbol)||symbol
// || decimals || gas_fee(u64 BE) [|| len(user_data)||user_data]     (length prefixes u32 BE)
func constructPC20ExportTSSMessage(
	chainID string,
	deadlineUnix int64,
	amount uint64,
	txID [32]byte,
	universalTxID [32]byte,
	pushAccount [20]byte,
	sourceAsset [20]byte,
	recipient solana.PublicKey,
	m *pc20ExportMeta,
	gasFee uint64,
) []byte {
	msg := append([]byte(nil), tssMessagePrefix...)
	msg = append(msg, pc20FinalizeInstructionID)
	msg = append(msg, []byte(chainID)...)
	msg = appendU64BE(msg, uint64(deadlineUnix))
	msg = appendU64BE(msg, amount)
	msg = append(msg, txID[:]...)
	msg = append(msg, universalTxID[:]...)
	msg = append(msg, pushAccount[:]...)
	msg = append(msg, sourceAsset[:]...)
	msg = append(msg, recipient.Bytes()...)
	msg = appendU32BEBytes(msg, []byte(m.Name))
	msg = appendU32BEBytes(msg, []byte(m.Symbol))
	msg = append(msg, m.Decimals)
	msg = appendU64BE(msg, gasFee)
	if len(m.UserData) > 0 {
		msg = appendU32BEBytes(msg, m.UserData)
	}
	return crypto.Keccak256(msg)
}

// constructPC20RemintTSSMessage mirrors the PC20 branches of revert.rs (id=3) and
// rescue.rs (id=4): the SPL layout plus the trailing "PC20" || source_asset domain suffix.
func constructPC20RemintTSSMessage(
	instructionID uint8,
	chainID string,
	deadlineUnix int64,
	amount uint64,
	txID [32]byte,
	universalTxID [32]byte,
	mint [32]byte,
	recipient [32]byte,
	gasFee uint64,
	revertMsg []byte,
	sourceAsset [20]byte,
) ([]byte, error) {
	if instructionID != 3 && instructionID != 4 {
		return nil, fmt.Errorf("pc20 remint message: unsupported instruction ID %d", instructionID)
	}
	msg := append([]byte(nil), tssMessagePrefix...)
	msg = append(msg, instructionID)
	msg = append(msg, []byte(chainID)...)
	msg = appendU64BE(msg, uint64(deadlineUnix))
	msg = appendU64BE(msg, amount)
	msg = append(msg, txID[:]...)
	msg = append(msg, universalTxID[:]...)
	msg = append(msg, mint[:]...)
	msg = append(msg, recipient[:]...)
	msg = appendU64BE(msg, gasFee)
	if instructionID == 3 {
		msg = append(msg, crypto.Keccak256(revertMsg)...)
	}
	msg = append(msg, pc20Selector[:]...)
	msg = append(msg, sourceAsset[:]...)
	return crypto.Keccak256(msg), nil
}

func (tb *TxBuilder) derivePC20MintPDA(sourceAsset [20]byte) (solana.PublicKey, error) {
	pda, _, err := solana.FindProgramAddress([][]byte{pc20MintSeed, sourceAsset[:]}, tb.gatewayAddress)
	if err != nil {
		return solana.PublicKey{}, fmt.Errorf("failed to derive pc20_mint PDA: %w", err)
	}
	return pda, nil
}

func (tb *TxBuilder) derivePC20StatePDA(mint solana.PublicKey) (solana.PublicKey, error) {
	pda, _, err := solana.FindProgramAddress([][]byte{pc20StateSeed, mint.Bytes()}, tb.gatewayAddress)
	if err != nil {
		return solana.PublicKey{}, fmt.Errorf("failed to derive pc20_state PDA: %w", err)
	}
	return pda, nil
}

// fetchPC20SourceAsset resolves mint → Push Chain source asset via the Pc20State PDA.
// Returns nil (no error) when mint is not a canonical PC20 wrapped mint. Pc20State is
// immutable once created, so this read is deterministic across validators.
func (tb *TxBuilder) fetchPC20SourceAsset(ctx context.Context, mint solana.PublicKey) (*[20]byte, error) {
	statePDA, err := tb.derivePC20StatePDA(mint)
	if err != nil {
		return nil, err
	}
	data, err := tb.rpcClient.GetAccountData(ctx, statePDA)
	if err != nil {
		if isAccountNotFoundErr(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to fetch pc20_state %s: %w", statePDA, err)
	}
	if len(data) < pc20StateAccountLen {
		return nil, fmt.Errorf("invalid pc20_state account data: %d bytes", len(data))
	}
	var sourceAsset [20]byte
	copy(sourceAsset[:], data[8:28])
	wrappedMint := solana.PublicKeyFromBytes(data[28:60])
	if !wrappedMint.Equals(mint) {
		return nil, fmt.Errorf("pc20_state wrapped_mint mismatch: state has %s, expected %s", wrappedMint, mint)
	}
	expectedMint, err := tb.derivePC20MintPDA(sourceAsset)
	if err != nil {
		return nil, err
	}
	if !expectedMint.Equals(mint) {
		return nil, fmt.Errorf("pc20 mint PDA mismatch for source asset 0x%x", sourceAsset)
	}
	return &sourceAsset, nil
}

func isAccountNotFoundErr(err error) bool {
	return errors.Is(err, rpc.ErrNotFound) || strings.Contains(err.Error(), "not found")
}

// getPC20ExportSigningRequest builds the id=5 signing hash for a PC20 export.
func (tb *TxBuilder) getPC20ExportSigningRequest(
	data *uetypes.OutboundCreatedEvent,
	nonce uint64,
	chainID string,
	amount uint64,
	txID [32]byte,
	universalTxID [32]byte,
	pushAccount [20]byte,
	payload []byte,
) (*common.UnsignedSigningReq, error) {
	sourceAsset, err := parsePC20SourceAsset(data.AssetAddr)
	if err != nil {
		return nil, err
	}
	meta, err := parsePC20ExportPayload(payload)
	if err != nil {
		return nil, err
	}
	if err := validatePC20UserData(meta.UserData); err != nil {
		return nil, err
	}
	recipient, err := parseSolanaAddress(data.Recipient)
	if err != nil {
		return nil, err
	}
	messageHash := constructPC20ExportTSSMessage(
		chainID, data.SigningDeadline, amount,
		txID, universalTxID, pushAccount, sourceAsset,
		recipient, meta, parseGasFee(data.GasFee),
	)
	return &common.UnsignedSigningReq{
		SigningHash: messageHash,
		Nonce:       nonce,
	}, nil
}

// validatePC20UserData checks that a non-empty user_data is a valid execute payload
// (wire format, instruction_id=2) — the gateway rejects anything else on-chain.
func validatePC20UserData(userData []byte) error {
	if len(userData) == 0 {
		return nil
	}
	_, _, instructionID, _, err := decodePayload(userData)
	if err != nil {
		return fmt.Errorf("invalid pc20 user_data: %w", err)
	}
	if instructionID != 2 {
		return fmt.Errorf("pc20 user_data must carry instruction_id=2, got %d", instructionID)
	}
	return nil
}

// buildPC20ExportTransaction assembles the finalize id=5 transaction (mirror of the
// generic path in BuildOutboundTransaction — must reproduce the signed hash's inputs).
func (tb *TxBuilder) buildPC20ExportTransaction(
	ctx context.Context,
	req *common.UnsignedSigningReq,
	data *uetypes.OutboundCreatedEvent,
	relayerKeypair solana.PrivateKey,
	signature []byte,
	recoveryID byte,
	txID [32]byte,
	universalTxID [32]byte,
	pushAccount [20]byte,
	amount uint64,
	payload []byte,
) (*solana.Transaction, uint8, error) {
	sourceAsset, err := parsePC20SourceAsset(data.AssetAddr)
	if err != nil {
		return nil, 0, err
	}
	meta, err := parsePC20ExportPayload(payload)
	if err != nil {
		return nil, 0, err
	}
	recipient, err := parseSolanaAddress(data.Recipient)
	if err != nil {
		return nil, 0, err
	}

	hasPayload := len(meta.UserData) > 0
	destinationProgram := solana.SystemProgramID
	var execAccounts []GatewayAccountMeta
	if hasPayload {
		accounts, _, instructionID, targetProgram, decErr := decodePayload(meta.UserData)
		if decErr != nil {
			return nil, 0, fmt.Errorf("invalid pc20 user_data: %w", decErr)
		}
		if instructionID != 2 {
			return nil, 0, fmt.Errorf("pc20 user_data must carry instruction_id=2, got %d", instructionID)
		}
		execAccounts = accounts
		destinationProgram = solana.PublicKeyFromBytes(targetProgram[:])
	}

	configPDA, _, err := solana.FindProgramAddress([][]byte{configSeed}, tb.gatewayAddress)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to derive config PDA: %w", err)
	}
	vaultPDA, _, err := solana.FindProgramAddress([][]byte{vaultSeed}, tb.gatewayAddress)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to derive vault PDA: %w", err)
	}
	tssPDA, _, err := solana.FindProgramAddress([][]byte{tssSeed}, tb.gatewayAddress)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to derive TSS PDA: %w", err)
	}
	executedTxPDA, _, err := solana.FindProgramAddress([][]byte{executedSubTxSeed, txID[:]}, tb.gatewayAddress)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to derive executed_tx PDA: %w", err)
	}
	ceaAuthorityPDA, _, err := solana.FindProgramAddress([][]byte{ceaAuthoritySeed, pushAccount[:]}, tb.gatewayAddress)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to derive cea_authority PDA: %w", err)
	}
	pc20Mint, err := tb.derivePC20MintPDA(sourceAsset)
	if err != nil {
		return nil, 0, err
	}
	pc20State, err := tb.derivePC20StatePDA(pc20Mint)
	if err != nil {
		return nil, 0, err
	}

	ixData := buildPC20ExportIxData(sourceAsset, meta)
	instructionData := tb.buildWithdrawAndExecuteData(
		pc20FinalizeInstructionID, txID, universalTxID, amount, pushAccount,
		[]byte{}, ixData, parseGasFee(data.GasFee), data.SigningDeadline,
		signature, recoveryID, req.SigningHash,
	)

	accounts, err := tb.buildPC20ExportAccounts(
		relayerKeypair.PublicKey(),
		configPDA, vaultPDA, ceaAuthorityPDA, tssPDA, executedTxPDA,
		destinationProgram, recipient,
		pc20Mint, pc20State,
		hasPayload, execAccounts,
	)
	if err != nil {
		return nil, 0, err
	}

	gatewayInstruction := solana.NewInstruction(tb.gatewayAddress, accounts, instructionData)
	instructions := []solana.Instruction{
		tb.buildSetComputeUnitLimitInstruction(defaultComputeUnitLimit),
		gatewayInstruction,
	}

	recentBlockhash, err := tb.rpcClient.GetRecentBlockhash(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get recent blockhash: %w", err)
	}

	opts := []solana.TransactionOption{solana.TransactionPayer(relayerKeypair.PublicKey())}
	addressTables, err := tb.fetchAddressTables(ctx, pc20Mint, false)
	if err != nil {
		tb.logger.Warn().Err(err).Msg("failed to fetch ALTs, falling back to legacy tx")
	} else if len(addressTables) > 0 {
		opts = append(opts, solana.TransactionAddressTables(addressTables))
	}
	tx, err := solana.NewTransaction(instructions, recentBlockhash, opts...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create transaction: %w", err)
	}

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

	if txBytes, marshalErr := tx.MarshalBinary(); marshalErr == nil && len(txBytes) > solanaTxMaxBytes {
		tb.logger.Warn().
			Int("raw_bytes", len(txBytes)).
			Int("limit", solanaTxMaxBytes).
			Int("ix_data_bytes", len(ixData)).
			Msg("pc20 export transaction exceeds Solana raw tx limit")
	}

	return tx, pc20FinalizeInstructionID, nil
}

// buildPC20ExportAccounts builds the finalize_universal_tx account list for the id=5 route.
// Typed slots follow the FinalizeUniversalTx struct; SPL/rate-limit/ref slots are None.
// Remaining: [pc20_state(w), pc20_mint(w), recipient_ata(w)] direct,
// or [pc20_state(w), pc20_mint(w), …payload accounts] when user_data is present.
func (tb *TxBuilder) buildPC20ExportAccounts(
	caller solana.PublicKey,
	configPDA solana.PublicKey,
	vaultPDA solana.PublicKey,
	ceaAuthorityPDA solana.PublicKey,
	tssPDA solana.PublicKey,
	executedTxPDA solana.PublicKey,
	destinationProgram solana.PublicKey,
	recipient solana.PublicKey,
	pc20Mint solana.PublicKey,
	pc20State solana.PublicKey,
	hasPayload bool,
	execAccounts []GatewayAccountMeta,
) ([]*solana.AccountMeta, error) {
	none := func() *solana.AccountMeta {
		return &solana.AccountMeta{PublicKey: tb.gatewayAddress, IsWritable: false, IsSigner: false}
	}

	accounts := []*solana.AccountMeta{
		{PublicKey: caller, IsWritable: true, IsSigner: true},
		{PublicKey: configPDA, IsWritable: false, IsSigner: false},
		{PublicKey: vaultPDA, IsWritable: true, IsSigner: false},
		{PublicKey: ceaAuthorityPDA, IsWritable: true, IsSigner: false},
		{PublicKey: tssPDA, IsWritable: true, IsSigner: false},
		{PublicKey: executedTxPDA, IsWritable: true, IsSigner: false},
		{PublicKey: solana.SystemProgramID, IsWritable: false, IsSigner: false},
		{PublicKey: destinationProgram, IsWritable: false, IsSigner: false},
		{PublicKey: recipient, IsWritable: true, IsSigner: false}, // recipient
	}

	accounts = append(accounts, none()) // vault_ata
	if hasPayload {
		ceaATA, _, err := solana.FindProgramAddress(
			[][]byte{ceaAuthorityPDA.Bytes(), solana.TokenProgramID.Bytes(), pc20Mint.Bytes()},
			solana.SPLAssociatedTokenAccountProgramID,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to derive cea ATA: %w", err)
		}
		accounts = append(accounts, &solana.AccountMeta{PublicKey: ceaATA, IsWritable: true, IsSigner: false}) // cea_ata
	} else {
		accounts = append(accounts, none()) // cea_ata
	}
	accounts = append(accounts, none()) // mint
	accounts = append(accounts,
		&solana.AccountMeta{PublicKey: solana.TokenProgramID, IsWritable: false, IsSigner: false},
		&solana.AccountMeta{PublicKey: solana.SysVarRentPubkey, IsWritable: false, IsSigner: false},
		&solana.AccountMeta{PublicKey: solana.SPLAssociatedTokenAccountProgramID, IsWritable: false, IsSigner: false},
	)
	accounts = append(accounts, none()) // recipient_ata (typed)
	accounts = append(accounts, none(), none()) // rate_limit_config, token_rate_limit
	accounts = append(accounts, none(), none()) // stored_ix_data, store_refund_recipient

	accounts = append(accounts,
		&solana.AccountMeta{PublicKey: pc20State, IsWritable: true, IsSigner: false},
		&solana.AccountMeta{PublicKey: pc20Mint, IsWritable: true, IsSigner: false},
	)
	if hasPayload {
		for _, acc := range execAccounts {
			accounts = append(accounts, &solana.AccountMeta{
				PublicKey:  solana.PublicKeyFromBytes(acc.Pubkey[:]),
				IsWritable: acc.IsWritable,
				IsSigner:   false,
			})
		}
	} else {
		recipientATA, _, err := solana.FindProgramAddress(
			[][]byte{recipient.Bytes(), solana.TokenProgramID.Bytes(), pc20Mint.Bytes()},
			solana.SPLAssociatedTokenAccountProgramID,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to derive recipient ATA: %w", err)
		}
		accounts = append(accounts, &solana.AccountMeta{PublicKey: recipientATA, IsWritable: true, IsSigner: false})
	}

	return accounts, nil
}

// buildPC20RemintAccounts builds the revert_universal_tx / rescue_funds account list for
// the PC20 remint branch: token_vault + recipient_token_account None, token_mint + token_program
// present, remaining = [pc20_state, pc20_mint(w), recipient_ata(w), ATA_program, rent].
func (tb *TxBuilder) buildPC20RemintAccounts(
	configPDA solana.PublicKey,
	vaultPDA solana.PublicKey,
	feeVaultPDA solana.PublicKey,
	tssPDA solana.PublicKey,
	recipient solana.PublicKey,
	executedTxPDA solana.PublicKey,
	caller solana.PublicKey,
	mint solana.PublicKey,
) ([]*solana.AccountMeta, error) {
	pc20State, err := tb.derivePC20StatePDA(mint)
	if err != nil {
		return nil, err
	}
	recipientATA, _, err := solana.FindProgramAddress(
		[][]byte{recipient.Bytes(), solana.TokenProgramID.Bytes(), mint.Bytes()},
		solana.SPLAssociatedTokenAccountProgramID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to derive recipient ATA: %w", err)
	}

	none := &solana.AccountMeta{PublicKey: tb.gatewayAddress, IsWritable: false, IsSigner: false}
	accounts := []*solana.AccountMeta{
		{PublicKey: configPDA, IsWritable: false, IsSigner: false},
		{PublicKey: vaultPDA, IsWritable: true, IsSigner: false},
		{PublicKey: feeVaultPDA, IsWritable: true, IsSigner: false},
		{PublicKey: tssPDA, IsWritable: true, IsSigner: false},
		{PublicKey: recipient, IsWritable: true, IsSigner: false},
		{PublicKey: executedTxPDA, IsWritable: true, IsSigner: false},
		{PublicKey: caller, IsWritable: true, IsSigner: true},
		{PublicKey: solana.SystemProgramID, IsWritable: false, IsSigner: false},
		none, // token_vault
		none, // recipient_token_account
		{PublicKey: mint, IsWritable: false, IsSigner: false},
		{PublicKey: solana.TokenProgramID, IsWritable: false, IsSigner: false},
		// remaining accounts — exact shape required by is_pc20_remint_account_shape
		{PublicKey: pc20State, IsWritable: false, IsSigner: false},
		{PublicKey: mint, IsWritable: true, IsSigner: false},
		{PublicKey: recipientATA, IsWritable: true, IsSigner: false},
		{PublicKey: solana.SPLAssociatedTokenAccountProgramID, IsWritable: false, IsSigner: false},
		{PublicKey: solana.SysVarRentPubkey, IsWritable: false, IsSigner: false},
	}
	return accounts, nil
}
