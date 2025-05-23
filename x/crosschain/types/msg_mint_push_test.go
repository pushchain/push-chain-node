package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMsgMintPush_ValidateBasic(t *testing.T) {
	validAccountId := &AccountId{
		Namespace: "ethereum",
		ChainId:   "1",
		OwnerKey:  "0x30ea71869947818d27b718592ea44010b458903bd9bf0370f50eda79e87d9f69",
		VmType:    VM_TYPE_EVM,
	}

	tests := []struct {
		name          string
		signer        string
		accountId     *AccountId
		txHash        string
		expectError   bool
		errorContains string
	}{
		{
			name:        "valid mint push message",
			signer:      "cosmos1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
			accountId:   validAccountId,
			txHash:      "0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			expectError: false,
		},
		{
			name:          "empty signer",
			signer:        "",
			accountId:     validAccountId,
			txHash:        "0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			expectError:   true,
			errorContains: "invalid signer",
		},
		{
			name:          "nil account id",
			signer:        "cosmos1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
			accountId:     nil,
			txHash:        "0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			expectError:   true,
			errorContains: "accountId cannot be nil",
		},
		{
			name:          "empty tx hash",
			signer:        "cosmos1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
			accountId:     validAccountId,
			txHash:        "",
			expectError:   true,
			errorContains: "txHash cannot be empty",
		},
		{
			name:        "duplicate tx hash scenario",
			signer:      "cosmos1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
			accountId:   validAccountId,
			txHash:      "0x1111111111111111111111111111111111111111111111111111111111111111",
			expectError: false, // Should duplicate txHash be prevented? Worth testing
		},
		{
			name:   "different account same tx hash",
			signer: "cosmos1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
			accountId: &AccountId{
				Namespace: "ethereum",
				ChainId:   "1",
				OwnerKey:  "0x40ea71869947818d27b718592ea44010b458903bd9bf0370f50eda79e87d9f69", // Different key
				VmType:    VM_TYPE_EVM,
			},
			txHash:      "0x1111111111111111111111111111111111111111111111111111111111111111", // Same hash
			expectError: false,                                                                // Should same txHash for different accounts be allowed? Worth testing
		},
		{
			name:        "cross-chain tx hash formats - bitcoin style",
			signer:      "cosmos1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
			accountId:   validAccountId,
			txHash:      "1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa", // Bitcoin style
			expectError: false,                                // Should different hash formats be validated? Worth testing
		},
		{
			name:   "cross-chain tx hash formats - solana style",
			signer: "cosmos1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
			accountId: &AccountId{
				Namespace: "solana",
				ChainId:   "mainnet-beta",
				OwnerKey:  "0x30ea71869947818d27b718592ea44010b458903bd9bf0370f50eda79e87d9f69",
				VmType:    VM_TYPE_SVM,
			},
			txHash:      "5j7s8K3jNjDqCgRVfRHjhyJH4L6jG9vP2sT1xWqLzMmN8kQ4fD3yR7nX6pS2wL9mC4dF8qP1sT5",
			expectError: false, // Should Solana tx format be validated? Worth testing
		},
		{
			name:        "very large amount scenario",
			signer:      "cosmos1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
			accountId:   validAccountId,
			txHash:      "0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			expectError: false, // Note: Amount is not in the message, it's hardcoded in keeper
		},
		{
			name:        "rapid successive minting",
			signer:      "cosmos1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
			accountId:   validAccountId,
			txHash:      "0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567891",
			expectError: false, // Should rate limiting be implemented? Worth testing
		},
		{
			name:   "account id with mismatched vm type",
			signer: "cosmos1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
			accountId: &AccountId{
				Namespace: "ethereum", // Ethereum namespace
				ChainId:   "1",
				OwnerKey:  "0x30ea71869947818d27b718592ea44010b458903bd9bf0370f50eda79e87d9f69",
				VmType:    VM_TYPE_SVM, // But SVM type
			},
			txHash:      "0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			expectError: false, // Should namespace/vmType consistency be validated? Worth testing
		},
		{
			name:   "tx hash from non-existent chain",
			signer: "cosmos1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
			accountId: &AccountId{
				Namespace: "non-existent-chain",
				ChainId:   "999999",
				OwnerKey:  "0x30ea71869947818d27b718592ea44010b458903bd9bf0370f50eda79e87d9f69",
				VmType:    VM_TYPE_OTHER_VM,
			},
			txHash:      "0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			expectError: false, // Should chain existence be validated? Worth testing
		},
		{
			name:        "attempt to mint for other user",
			signer:      "cosmos1different_signer_address_here_12345678901234567890",
			accountId:   validAccountId, // Account owned by different key
			txHash:      "0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			expectError: false, // Should signer match accountId owner? Worth testing
		},
		{
			name:        "extremely old tx hash",
			signer:      "cosmos1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
			accountId:   validAccountId,
			txHash:      "0x0000000000000000000000000000000000000000000000000000000000000001", // Genesis-like
			expectError: false,                                                                // Should tx age be validated? Worth testing
		},
		{
			name:        "tx hash with invalid checksum",
			signer:      "cosmos1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
			accountId:   validAccountId,
			txHash:      "0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			expectError: false, // Should tx hash checksum be validated? Worth testing
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &MsgMintPush{
				Signer:    tt.signer,
				AccountId: tt.accountId,
				TxHash:    tt.txHash,
			}

			err := msg.ValidateBasic()

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					require.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}
