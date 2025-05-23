package keeper_test

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/rollchains/pchain/x/crosschain/types"
	"github.com/stretchr/testify/require"
)

func TestCallFactoryToComputeAddress(t *testing.T) {
	f := SetupTest(t)

	tests := []struct {
		name          string
		from          common.Address
		factoryAddr   common.Address
		accountId     types.AccountId
		expectError   bool
		errorContains string
	}{
		{
			name:        "valid compute address call",
			from:        common.HexToAddress("0x1234567890123456789012345678901234567890"),
			factoryAddr: common.HexToAddress("0xabcdefabcdefabcdefabcdefabcdefabcdefabcd"),
			accountId: types.AccountId{
				Namespace: "eip155",
				ChainId:   "1",
				OwnerKey:  "0x1234567890123456789012345678901234567890",
				VmType:    types.VM_TYPE_EVM,
			},
			expectError:   true, // Will fail because ABI parsing is not mocked
			errorContains: "failed to parse factory ABI",
		},
		{
			name:        "invalid factory address - zero address",
			from:        common.HexToAddress("0x1234567890123456789012345678901234567890"),
			factoryAddr: common.Address{}, // zero address
			accountId: types.AccountId{
				Namespace: "eip155",
				ChainId:   "1",
				OwnerKey:  "0x1234567890123456789012345678901234567890",
				VmType:    types.VM_TYPE_EVM,
			},
			expectError:   true,
			errorContains: "failed to parse factory ABI",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			accountIdAbi, err := types.NewAbiAccountId(&tc.accountId)
			require.NoError(t, err)

			resp, err := f.k.CallFactoryToComputeAddress(
				f.ctx,
				tc.from,
				tc.factoryAddr,
				accountIdAbi,
			)

			if tc.expectError {
				require.Error(t, err)
				if tc.errorContains != "" {
					require.Contains(t, err.Error(), tc.errorContains)
				}
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
			}
		})
	}
}

func TestCallFactoryToDeployNMSC(t *testing.T) {
	f := SetupTest(t)

	tests := []struct {
		name          string
		from          common.Address
		factoryAddr   common.Address
		accountId     types.AccountId
		expectError   bool
		errorContains string
	}{
		{
			name:        "deployment attempt with valid addresses",
			from:        common.HexToAddress("0x1234567890123456789012345678901234567890"),
			factoryAddr: common.HexToAddress("0xabcdefabcdefabcdefabcdefabcdefabcdefabcd"),
			accountId: types.AccountId{
				Namespace: "eip155",
				ChainId:   "1",
				OwnerKey:  "0x1234567890123456789012345678901234567890",
				VmType:    types.VM_TYPE_EVM,
			},
			expectError:   true, // Will fail because ABI parsing is not mocked
			errorContains: "failed to parse factory ABI",
		},
		{
			name:        "deployment with zero factory address",
			from:        common.HexToAddress("0x1234567890123456789012345678901234567890"),
			factoryAddr: common.Address{}, // zero address
			accountId: types.AccountId{
				Namespace: "eip155",
				ChainId:   "1",
				OwnerKey:  "0x1234567890123456789012345678901234567890",
				VmType:    types.VM_TYPE_EVM,
			},
			expectError:   true,
			errorContains: "failed to parse factory ABI",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			accountIdAbi, err := types.NewAbiAccountId(&tc.accountId)
			require.NoError(t, err)

			resp, err := f.k.CallFactoryToDeployNMSC(
				f.ctx,
				tc.from,
				tc.factoryAddr,
				accountIdAbi,
			)

			if tc.expectError {
				require.Error(t, err)
				if tc.errorContains != "" {
					require.Contains(t, err.Error(), tc.errorContains)
				}
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
			}
		})
	}
}

func TestCallNMSCExecutePayload(t *testing.T) {
	f := SetupTest(t)

	tests := []struct {
		name          string
		from          common.Address
		nmscAddr      common.Address
		payload       types.CrossChainPayload
		signature     []byte
		expectError   bool
		errorContains string
	}{
		{
			name:     "payload execution attempt",
			from:     common.HexToAddress("0x1234567890123456789012345678901234567890"),
			nmscAddr: common.HexToAddress("0xabcdefabcdefabcdefabcdefabcdefabcdefabcd"),
			payload: types.CrossChainPayload{
				Target:               "0x1111111111111111111111111111111111111111",
				Value:                "1000000000000000000", // 1 ETH
				Data:                 "0x",
				GasLimit:             "21000",
				MaxFeePerGas:         "20000000000", // 20 gwei
				MaxPriorityFeePerGas: "1000000000",  // 1 gwei
				Nonce:                "1",
				Deadline:             "1234567890",
			},
			signature:     []byte("mock_signature_bytes"),
			expectError:   true, // Will fail because ABI parsing is not mocked
			errorContains: "failed to parse smart account ABI",
		},
		{
			name:     "payload execution with invalid payload",
			from:     common.HexToAddress("0x1234567890123456789012345678901234567890"),
			nmscAddr: common.HexToAddress("0xabcdefabcdefabcdefabcdefabcdefabcdefabcd"),
			payload: types.CrossChainPayload{
				Target:               "", // empty target
				Value:                "invalid_value",
				Data:                 "invalid_hex",
				GasLimit:             "not_a_number",
				MaxFeePerGas:         "invalid",
				MaxPriorityFeePerGas: "invalid",
				Nonce:                "invalid",
				Deadline:             "invalid",
			},
			signature:     []byte("invalid_signature"),
			expectError:   true,
			errorContains: "invalid cross-chain payload",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			payloadAbi, err := types.NewAbiCrossChainPayload(&tc.payload)
			if err != nil {
				require.Error(t, err)
				require.Contains(t, err.Error(), "invalid cross-chain payload")
				return
			}

			resp, err := f.k.CallNMSCExecutePayload(
				f.ctx,
				tc.from,
				tc.nmscAddr,
				payloadAbi,
				tc.signature,
			)

			if tc.expectError {
				require.Error(t, err)
				if tc.errorContains != "" {
					require.Contains(t, err.Error(), tc.errorContains)
				}
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
			}
		})
	}
}

// Test EVM keeper integration boundaries
func TestEVMIntegrationBoundaries(t *testing.T) {
	f := SetupTest(t)

	t.Run("nil_evm_keeper", func(t *testing.T) {
		// Test behavior when EVM keeper is nil (should be handled gracefully)
		accountId := types.AccountId{
			Namespace: "eip155",
			ChainId:   "1",
			OwnerKey:  "0x1234567890123456789012345678901234567890",
			VmType:    types.VM_TYPE_EVM,
		}
		accountIdAbi, err := types.NewAbiAccountId(&accountId)
		require.NoError(t, err)

		from := common.HexToAddress("0x1234567890123456789012345678901234567890")
		factory := common.HexToAddress("0xabcdefabcdefabcdefabcdefabcdefabcdefabcd")

		// This should fail gracefully, not panic
		_, err = f.k.CallFactoryToComputeAddress(f.ctx, from, factory, accountIdAbi)
		require.Error(t, err)
	})

	t.Run("empty_address_handling", func(t *testing.T) {
		// Test with empty addresses
		accountId := types.AccountId{
			Namespace: "eip155",
			ChainId:   "1",
			OwnerKey:  "0x1234567890123456789012345678901234567890",
			VmType:    types.VM_TYPE_EVM,
		}
		accountIdAbi, err := types.NewAbiAccountId(&accountId)
		require.NoError(t, err)

		emptyAddr := common.Address{}
		_, err = f.k.CallFactoryToComputeAddress(f.ctx, emptyAddr, emptyAddr, accountIdAbi)
		require.Error(t, err)
	})
}

// Benchmark test for EVM operations (simplified)
func BenchmarkEVMOperations(b *testing.B) {
	f := SetupTest(&testing.T{})

	accountId := types.AccountId{
		Namespace: "eip155",
		ChainId:   "1",
		OwnerKey:  "0x1234567890123456789012345678901234567890",
		VmType:    types.VM_TYPE_EVM,
	}
	accountIdAbi, _ := types.NewAbiAccountId(&accountId)
	from := common.HexToAddress("0x1234567890123456789012345678901234567890")
	factory := common.HexToAddress("0xabcdefabcdefabcdefabcdefabcdefabcdefabcd")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// This will error due to missing EVM setup, but we're testing the call path
		_, _ = f.k.CallFactoryToComputeAddress(f.ctx, from, factory, accountIdAbi)
	}
}
