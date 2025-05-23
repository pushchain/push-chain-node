package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMsgDeployNMSC_ValidateBasic(t *testing.T) {
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
			name:        "valid deploy nmsc message",
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
			name:          "invalid signer format",
			signer:        "invalid-signer",
			accountId:     validAccountId,
			txHash:        "0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			expectError:   true,
			errorContains: "invalid signer",
		},
		{
			name:          "hex address as signer (should fail)",
			signer:        "0x527F3692F5C53CfA83F7689885995606F93b6164",
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
			name:   "empty namespace in account id",
			signer: "cosmos1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
			accountId: &AccountId{
				Namespace: "",
				ChainId:   "1",
				OwnerKey:  "0x30ea71869947818d27b718592ea44010b458903bd9bf0370f50eda79e87d9f69",
				VmType:    VM_TYPE_EVM,
			},
			txHash:        "0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			expectError:   true,
			errorContains: "namespace cannot be empty",
		},
		{
			name:   "empty chain id in account id",
			signer: "cosmos1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
			accountId: &AccountId{
				Namespace: "ethereum",
				ChainId:   "",
				OwnerKey:  "0x30ea71869947818d27b718592ea44010b458903bd9bf0370f50eda79e87d9f69",
				VmType:    VM_TYPE_EVM,
			},
			txHash:        "0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			expectError:   true,
			errorContains: "chainId cannot be empty",
		},
		{
			name:   "invalid hex in owner key",
			signer: "cosmos1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
			accountId: &AccountId{
				Namespace: "ethereum",
				ChainId:   "1",
				OwnerKey:  "0xZZZea71869947818d27b718592ea44010b458903bd9bf0370f50eda79e87d9f69",
				VmType:    VM_TYPE_EVM,
			},
			txHash:        "0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			expectError:   true,
			errorContains: "invalid hex",
		},
		{
			name:   "invalid vm type",
			signer: "cosmos1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
			accountId: &AccountId{
				Namespace: "ethereum",
				ChainId:   "1",
				OwnerKey:  "0x30ea71869947818d27b718592ea44010b458903bd9bf0370f50eda79e87d9f69",
				VmType:    VM_TYPE(999), // Invalid enum value
			},
			txHash:        "0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			expectError:   true,
			errorContains: "invalid vm_type",
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
			name:        "very short tx hash",
			signer:      "cosmos1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
			accountId:   validAccountId,
			txHash:      "0x123",
			expectError: false, // Should this be validated? Worth testing
		},
		{
			name:        "tx hash without 0x prefix",
			signer:      "cosmos1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
			accountId:   validAccountId,
			txHash:      "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			expectError: false, // Should this be validated? Worth testing
		},
		{
			name:        "extremely long tx hash",
			signer:      "cosmos1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
			accountId:   validAccountId,
			txHash:      "0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			expectError: false, // Should this be validated? Worth testing
		},
		{
			name:        "tx hash with invalid hex characters",
			signer:      "cosmos1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
			accountId:   validAccountId,
			txHash:      "0xabcdefZZZZ567890abcdef1234567890abcdef1234567890abcdef1234567890",
			expectError: false, // Should this be validated? Worth testing
		},
		{
			name:   "namespace with special characters",
			signer: "cosmos1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
			accountId: &AccountId{
				Namespace: "ethereum'; DROP TABLE;",
				ChainId:   "1",
				OwnerKey:  "0x30ea71869947818d27b718592ea44010b458903bd9bf0370f50eda79e87d9f69",
				VmType:    VM_TYPE_EVM,
			},
			txHash:      "0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			expectError: false, // Should special chars be allowed in namespace? Worth testing
		},
		{
			name:   "chain id with special characters",
			signer: "cosmos1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
			accountId: &AccountId{
				Namespace: "ethereum",
				ChainId:   "1; DROP TABLE;",
				OwnerKey:  "0x30ea71869947818d27b718592ea44010b458903bd9bf0370f50eda79e87d9f69",
				VmType:    VM_TYPE_EVM,
			},
			txHash:      "0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			expectError: false, // Should special chars be allowed in chainId? Worth testing
		},
		{
			name:   "owner key with control characters",
			signer: "cosmos1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
			accountId: &AccountId{
				Namespace: "ethereum",
				ChainId:   "1",
				OwnerKey:  "0x30ea71869947818d27b7\n18592ea44010b458903bd9bf0370f50eda79e87d9f69",
				VmType:    VM_TYPE_EVM,
			},
			txHash:        "0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			expectError:   true,
			errorContains: "invalid hex",
		},
		{
			name:        "tx hash with control characters",
			signer:      "cosmos1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
			accountId:   validAccountId,
			txHash:      "0xabcdef1234567890\nabcdef1234567890abcdef1234567890abcdef1234567890",
			expectError: false, // Should control chars be validated in txHash? Worth testing
		},
		{
			name:   "extremely long namespace",
			signer: "cosmos1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
			accountId: &AccountId{
				Namespace: "ethereum" + string(make([]byte, 1000)),
				ChainId:   "1",
				OwnerKey:  "0x30ea71869947818d27b718592ea44010b458903bd9bf0370f50eda79e87d9f69",
				VmType:    VM_TYPE_EVM,
			},
			txHash:      "0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			expectError: false, // Should long namespaces be limited? Worth testing
		},
		{
			name:   "solana account id",
			signer: "cosmos1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
			accountId: &AccountId{
				Namespace: "solana",
				ChainId:   "mainnet-beta",
				OwnerKey:  "0x30ea71869947818d27b718592ea44010b458903bd9bf0370f50eda79e87d9f69",
				VmType:    VM_TYPE_SVM,
			},
			txHash:      "5j7s8K3jNjDqCgRVfRHjhyJH4L6jG9vP2sT1xWqLzMmN8kQ4fD3yR7nX6pS2wL9",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &MsgDeployNMSC{
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
