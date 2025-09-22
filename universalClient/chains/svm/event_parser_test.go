package svm

import (
	"encoding/binary"
	"testing"

	solana "github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"

	"github.com/pushchain/push-chain-node/universalClient/chains/common"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
)

func TestNewEventParser(t *testing.T) {
	logger := zerolog.New(nil).Level(zerolog.Disabled)
	gatewayAddr := solana.MustPublicKeyFromBase58("11111111111111111111111111111112")
	
	config := &uregistrytypes.ChainConfig{
		Chain:          "solana:mainnet",
		GatewayAddress: gatewayAddr.String(),
		GatewayMethods: []*uregistrytypes.GatewayMethods{
			{
				Name:       "addFunds",
				Identifier: "84ed4c39500ab38a",
			},
		},
	}
	
	parser := NewEventParser(gatewayAddr, config, logger)
	
	assert.NotNil(t, parser)
	assert.Equal(t, gatewayAddr, parser.gatewayAddr)
	assert.Equal(t, config, parser.config)
}

func TestParseGatewayEvent(t *testing.T) {
	logger := zerolog.New(nil).Level(zerolog.Disabled)
	gatewayAddr := solana.MustPublicKeyFromBase58("11111111111111111111111111111112")
	
	config := &uregistrytypes.ChainConfig{
		Chain:          "solana:mainnet",
		GatewayAddress: gatewayAddr.String(),
		GatewayMethods: []*uregistrytypes.GatewayMethods{
			{
				Name:       "addFunds",
				Identifier: "84ed4c39500ab38a",
			},
		},
	}
	
	parser := NewEventParser(gatewayAddr, config, logger)
	
	tests := []struct {
		name      string
		tx        *rpc.GetTransactionResult
		signature string
		slot      uint64
		wantEvent bool
		validate  func(*testing.T, *common.GatewayEvent)
	}{
		{
			name:      "returns nil for nil transaction",
			tx:        nil,
			signature: "test-sig",
			slot:      12345,
			wantEvent: false,
		},
		{
			name: "returns nil for transaction without meta",
			tx: &rpc.GetTransactionResult{
				Meta: nil,
			},
			signature: "test-sig",
			slot:      12345,
			wantEvent: false,
		},
		{
			name: "returns nil for non-gateway transaction",
			tx: &rpc.GetTransactionResult{
				Meta: &rpc.TransactionMeta{
					LogMessages: []string{
						"Program log: Some other program",
					},
				},
			},
			signature: "test-sig",
			slot:      12345,
			wantEvent: false,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := parser.ParseGatewayEvent(tt.tx, tt.signature, tt.slot)
			
			if tt.wantEvent {
				assert.NotNil(t, event)
				if tt.validate != nil {
					tt.validate(t, event)
				}
			} else {
				assert.Nil(t, event)
			}
		})
	}
}

func TestIsGatewayTransaction(t *testing.T) {
	gatewayAddr := solana.MustPublicKeyFromBase58("11111111111111111111111111111112")
	
	parser := &EventParser{
		gatewayAddr: gatewayAddr,
	}
	
	tests := []struct {
		name     string
		tx       *rpc.GetTransactionResult
		expected bool
	}{
		{
			name: "detects gateway transaction",
			tx: &rpc.GetTransactionResult{
				Meta: &rpc.TransactionMeta{
					LogMessages: []string{
						"Program " + gatewayAddr.String() + " invoke [1]",
						"Program log: Processing instruction",
					},
				},
			},
			expected: true,
		},
		{
			name: "returns false for non-gateway transaction",
			tx: &rpc.GetTransactionResult{
				Meta: &rpc.TransactionMeta{
					LogMessages: []string{
						"Program SomeOtherProgram111111111111111111111111 invoke [1]",
						"Program log: Processing instruction",
					},
				},
			},
			expected: false,
		},
		{
			name: "returns false for empty logs",
			tx: &rpc.GetTransactionResult{
				Meta: &rpc.TransactionMeta{
					LogMessages: []string{},
				},
			},
			expected: false,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parser.isGatewayTransaction(tt.tx)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractMethodInfo(t *testing.T) {
	config := &uregistrytypes.ChainConfig{
		GatewayMethods: []*uregistrytypes.GatewayMethods{
			{
				Name:       "addFunds",
				Identifier: "84ed4c39500ab38a",
			},
			{
				Name:       "withdrawFunds",
				Identifier: "abcd1234efgh5678",
			},
		},
	}
	
	parser := &EventParser{
		config: config,
	}
	
	tests := []struct {
		name           string
		tx             *rpc.GetTransactionResult
		expectedID     string
		expectedMethod string
	}{
		{
			name: "extracts add_funds method",
			tx: &rpc.GetTransactionResult{
				Meta: &rpc.TransactionMeta{
					LogMessages: []string{
						"Program log: add_funds called",
					},
				},
			},
			expectedID:     "84ed4c39500ab38a",
			expectedMethod: "addFunds",
		},
		{
			name: "extracts AddFunds method (capital case)",
			tx: &rpc.GetTransactionResult{
				Meta: &rpc.TransactionMeta{
					LogMessages: []string{
						"Program log: AddFunds instruction",
					},
				},
			},
			expectedID:     "84ed4c39500ab38a",
			expectedMethod: "addFunds",
		},
		{
			name: "returns empty for unknown method",
			tx: &rpc.GetTransactionResult{
				Meta: &rpc.TransactionMeta{
					LogMessages: []string{
						"Program log: unknown_method",
					},
				},
			},
			expectedID:     "",
			expectedMethod: "",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, method, _ := parser.extractMethodInfo(tt.tx)
			assert.Equal(t, tt.expectedID, id)
			assert.Equal(t, tt.expectedMethod, method)
		})
	}
}

func TestExtractTransactionDetails(t *testing.T) {
	logger := zerolog.New(nil).Level(zerolog.Disabled)
	gatewayAddr := solana.MustPublicKeyFromBase58("11111111111111111111111111111112")
	
	parser := &EventParser{
		gatewayAddr: gatewayAddr,
		logger:      logger,
	}
	
	tests := []struct {
		name           string
		tx             *rpc.GetTransactionResult
		expectedSender string
		expectedAmount string
		expectError    bool
	}{
		{
			name: "returns error for nil transaction",
			tx: &rpc.GetTransactionResult{
				Transaction: nil,
			},
			expectError: true,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sender, _, amount, _, err := parser.extractTransactionDetails(tt.tx)
			
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedSender, sender)
				assert.Equal(t, tt.expectedAmount, amount)
			}
		})
	}
}

func TestBytesEqual(t *testing.T) {
	tests := []struct {
		name     string
		a        []byte
		b        []byte
		expected bool
	}{
		{
			name:     "equal byte slices",
			a:        []byte{0x01, 0x02, 0x03},
			b:        []byte{0x01, 0x02, 0x03},
			expected: true,
		},
		{
			name:     "different byte slices",
			a:        []byte{0x01, 0x02, 0x03},
			b:        []byte{0x01, 0x02, 0x04},
			expected: false,
		},
		{
			name:     "different lengths",
			a:        []byte{0x01, 0x02},
			b:        []byte{0x01, 0x02, 0x03},
			expected: false,
		},
		{
			name:     "both empty",
			a:        []byte{},
			b:        []byte{},
			expected: true,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := bytesEqual(tt.a, tt.b)
			assert.Equal(t, tt.expected, result)
		})
	}
}



// Helper function to create a test transaction
func createTestTransaction() *solana.Transaction {
	// Create test accounts
	payer := solana.MustPrivateKeyFromBase58("5ysPKzei6U5b1KTRs7XjwUL8577j3WWRmceKcVFGLWRA2zKVrYgnStRdqrL4NfU1nK5Ag5hYM4JMhiXBM3BHKHTG")
	receiver := solana.MustPublicKeyFromBase58("22222222222222222222222222222223")
	
	// Create a simple instruction
	instruction := &solana.GenericInstruction{
		ProgID: solana.SystemProgramID,
		AccountValues: solana.AccountMetaSlice{
			&solana.AccountMeta{
				PublicKey:  payer.PublicKey(),
				IsWritable: true,
				IsSigner:   true,
			},
			&solana.AccountMeta{
				PublicKey:  receiver,
				IsWritable: true,
				IsSigner:   false,
			},
		},
		DataBytes: []byte{0x02, 0x00, 0x00, 0x00}, // Transfer instruction
	}
	
	// Build transaction
	tx, _ := solana.NewTransaction(
		[]solana.Instruction{instruction},
		solana.Hash{}, // Recent blockhash (dummy for test)
		solana.TransactionPayer(payer.PublicKey()),
	)
	
	return tx
}

// Helper to create amount bytes for testing
func createAmountBytes(amount uint64) []byte {
	bytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(bytes, amount)
	return bytes
}