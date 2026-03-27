package types_test

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/pushchain/push-chain-node/x/uexecutor/types"
	"github.com/stretchr/testify/require"
)

func TestInbound_ValidateBasic(t *testing.T) {
	validInbound := types.Inbound{
		SourceChain: "eip155:11155111",
		TxHash:      "0x123abc",
		Sender:      "0x000000000000000000000000000000000000dead",
		Recipient:   "0x000000000000000000000000000000000000beef",
		Amount:      "1000",
		AssetAddr:   "0x000000000000000000000000000000000000cafe",
		LogIndex:    "1",
		TxType:      types.TxType_FUNDS,
	}

	tests := []struct {
		name        string
		inbound     types.Inbound
		expectError bool
		errContains string
	}{
		{
			name:        "valid inbound",
			inbound:     validInbound,
			expectError: false,
		},
		{
			name: "empty source chain",
			inbound: func() types.Inbound {
				ib := validInbound
				ib.SourceChain = ""
				return ib
			}(),
			expectError: true,
			errContains: "source chain cannot be empty",
		},
		{
			name: "invalid source chain format",
			inbound: func() types.Inbound {
				ib := validInbound
				ib.SourceChain = "eip155" // missing ":"
				return ib
			}(),
			expectError: true,
			errContains: "CAIP-2 format",
		},
		{
			name: "empty tx_hash",
			inbound: func() types.Inbound {
				ib := validInbound
				ib.TxHash = ""
				return ib
			}(),
			expectError: true,
			errContains: "tx_hash cannot be empty",
		},
		{
			name: "empty sender",
			inbound: func() types.Inbound {
				ib := validInbound
				ib.Sender = ""
				return ib
			}(),
			expectError: true,
			errContains: "sender cannot be empty",
		},
		{
			name: "empty log_index",
			inbound: func() types.Inbound {
				ib := validInbound
				ib.LogIndex = ""
				return ib
			}(),
			expectError: true,
			errContains: "log_index cannot be empty",
		},
		{
			name: "unspecified tx_type",
			inbound: func() types.Inbound {
				ib := validInbound
				ib.TxType = types.TxType_UNSPECIFIED_TX
				return ib
			}(),
			expectError: true,
			errContains: "invalid tx_type",
		},
		{
			name: "invalid tx_type out of range",
			inbound: func() types.Inbound {
				ib := validInbound
				ib.TxType = 99
				return ib
			}(),
			expectError: true,
			errContains: "invalid tx_type",
		},
		{
			name: "passes with extra payload on non-payload type (ignored at execution time)",
			inbound: func() types.Inbound {
				ib := validInbound
				ib.UniversalPayload = &types.UniversalPayload{Data: "0x1234"}
				return ib
			}(),
			expectError: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.inbound.ValidateBasic()
			if tc.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestInbound_ValidateForExecution(t *testing.T) {
	validPayload := &types.UniversalPayload{
		To:                   "0x000000000000000000000000000000000000beef",
		Value:                "1000",
		Data:                 "0x",
		GasLimit:             "21000",
		MaxFeePerGas:         "1000000000",
		MaxPriorityFeePerGas: "200000000",
		Nonce:                "1",
		Deadline:             "9999999999",
		VType:                types.VerificationType(1),
	}

	validFundsInbound := types.Inbound{
		SourceChain: "eip155:11155111",
		TxHash:      "0x123abc",
		Sender:      "0x000000000000000000000000000000000000dead",
		Recipient:   "0x000000000000000000000000000000000000beef",
		Amount:      "1000",
		AssetAddr:   "0x000000000000000000000000000000000000cafe",
		LogIndex:    "1",
		TxType:      types.TxType_FUNDS,
	}

	validPayloadInbound := types.Inbound{
		SourceChain:      "eip155:11155111",
		TxHash:           "0x123abc",
		Sender:           "0x000000000000000000000000000000000000dead",
		Amount:           "1000",
		AssetAddr:        "0x000000000000000000000000000000000000cafe",
		LogIndex:         "1",
		TxType:           types.TxType_FUNDS_AND_PAYLOAD,
		UniversalPayload: validPayload,
	}

	tests := []struct {
		name        string
		inbound     types.Inbound
		expectError bool
		errContains string
	}{
		{
			name:        "valid funds inbound",
			inbound:     validFundsInbound,
			expectError: false,
		},
		{
			name:        "valid payload inbound",
			inbound:     validPayloadInbound,
			expectError: false,
		},
		{
			name: "empty amount",
			inbound: func() types.Inbound {
				ib := validFundsInbound
				ib.Amount = ""
				return ib
			}(),
			expectError: true,
			errContains: "amount cannot be empty",
		},
		{
			name: "negative amount",
			inbound: func() types.Inbound {
				ib := validFundsInbound
				ib.Amount = "-100"
				return ib
			}(),
			expectError: true,
			errContains: "amount must be a valid non-negative uint256",
		},
		{
			name: "zero amount rejected for FUNDS type",
			inbound: func() types.Inbound {
				ib := validFundsInbound
				ib.Amount = "0"
				ib.TxType = types.TxType_FUNDS
				return ib
			}(),
			expectError: true,
			errContains: "amount must be positive for this tx type",
		},
		{
			name: "zero amount rejected for GAS type",
			inbound: func() types.Inbound {
				ib := validFundsInbound
				ib.Amount = "0"
				ib.TxType = types.TxType_GAS
				return ib
			}(),
			expectError: true,
			errContains: "amount must be positive for this tx type",
		},
		{
			name: "zero amount allowed for FUNDS_AND_PAYLOAD type",
			inbound: func() types.Inbound {
				ib := validFundsInbound
				ib.Amount = "0"
				ib.TxType = types.TxType_FUNDS_AND_PAYLOAD
				ib.Recipient = ""
				ib.UniversalPayload = validPayload
				return ib
			}(),
			expectError: false,
		},
		{
			name: "zero amount allowed for GAS_AND_PAYLOAD type",
			inbound: func() types.Inbound {
				ib := validFundsInbound
				ib.Amount = "0"
				ib.TxType = types.TxType_GAS_AND_PAYLOAD
				ib.Recipient = ""
				ib.UniversalPayload = validPayload
				return ib
			}(),
			expectError: false,
		},
		{
			name: "empty asset_addr",
			inbound: func() types.Inbound {
				ib := validFundsInbound
				ib.AssetAddr = ""
				return ib
			}(),
			expectError: true,
			errContains: "asset_addr cannot be empty",
		},
		{
			name: "empty recipient for FUNDS type",
			inbound: func() types.Inbound {
				ib := validFundsInbound
				ib.Recipient = ""
				return ib
			}(),
			expectError: true,
			errContains: "recipient cannot be empty",
		},
		{
			name: "invalid recipient address for FUNDS type",
			inbound: func() types.Inbound {
				ib := validFundsInbound
				ib.Recipient = "0xzzzzzzzz"
				return ib
			}(),
			expectError: true,
			errContains: "invalid recipient address",
		},
		{
			name: "missing payload for FUNDS_AND_PAYLOAD type",
			inbound: func() types.Inbound {
				ib := validPayloadInbound
				ib.UniversalPayload = nil
				return ib
			}(),
			expectError: true,
			errContains: "payload is required",
		},
		// isCEA validation tests
		{
			name: "isCEA rejected for GAS type",
			inbound: func() types.Inbound {
				ib := validFundsInbound
				ib.TxType = types.TxType_GAS
				ib.IsCEA = true
				return ib
			}(),
			expectError: true,
			errContains: "isCEA is only supported for FUNDS, FUNDS_AND_PAYLOAD, and GAS_AND_PAYLOAD",
		},
		{
			name: "isCEA allowed for FUNDS type",
			inbound: func() types.Inbound {
				ib := validFundsInbound
				ib.IsCEA = true
				ib.TxType = types.TxType_FUNDS
				return ib
			}(),
			expectError: false,
		},
		{
			name: "isCEA allowed for FUNDS_AND_PAYLOAD type",
			inbound: func() types.Inbound {
				ib := validFundsInbound
				ib.IsCEA = true
				ib.TxType = types.TxType_FUNDS_AND_PAYLOAD
				ib.Recipient = "0x000000000000000000000000000000000000beef"
				ib.UniversalPayload = validPayload
				return ib
			}(),
			expectError: false,
		},
		{
			name: "isCEA allowed for GAS_AND_PAYLOAD type",
			inbound: func() types.Inbound {
				ib := validFundsInbound
				ib.IsCEA = true
				ib.TxType = types.TxType_GAS_AND_PAYLOAD
				ib.Recipient = "0x000000000000000000000000000000000000beef"
				ib.UniversalPayload = validPayload
				return ib
			}(),
			expectError: false,
		},
		{
			name: "isCEA=true requires recipient for GAS_AND_PAYLOAD",
			inbound: func() types.Inbound {
				ib := validFundsInbound
				ib.IsCEA = true
				ib.TxType = types.TxType_GAS_AND_PAYLOAD
				ib.Recipient = ""
				ib.UniversalPayload = validPayload
				return ib
			}(),
			expectError: true,
			errContains: "recipient cannot be empty when isCEA is true",
		},
		{
			name: "isCEA=true with invalid recipient for GAS_AND_PAYLOAD",
			inbound: func() types.Inbound {
				ib := validFundsInbound
				ib.IsCEA = true
				ib.TxType = types.TxType_GAS_AND_PAYLOAD
				ib.Recipient = "not-a-hex-address"
				ib.UniversalPayload = validPayload
				return ib
			}(),
			expectError: true,
			errContains: "invalid recipient address when isCEA is true",
		},
		{
			name: "isCEA with empty recipient on FUNDS_AND_PAYLOAD",
			inbound: func() types.Inbound {
				ib := validPayloadInbound
				ib.IsCEA = true
				ib.Recipient = ""
				return ib
			}(),
			expectError: true,
			errContains: "recipient cannot be empty when isCEA is true",
		},
		{
			name: "isCEA with valid recipient on FUNDS_AND_PAYLOAD",
			inbound: func() types.Inbound {
				ib := validPayloadInbound
				ib.IsCEA = true
				ib.Recipient = "0x000000000000000000000000000000000000beef"
				return ib
			}(),
			expectError: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.inbound.ValidateForExecution()
			if tc.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestInbound_NormalizeForTxType(t *testing.T) {
	t.Run("payload type without isCEA: zeros recipient, wipes UV-sent universal_payload, no raw_payload → nil payload", func(t *testing.T) {
		ib := types.Inbound{
			TxType:       types.TxType_FUNDS_AND_PAYLOAD,
			SourceChain:  "eip155:11155111",
			Recipient:    "0x000000000000000000000000000000000000beef",
			UniversalPayload: &types.UniversalPayload{
				Data: "0x1234",
				To:   "0x000000000000000000000000000000000000abcd",
			},
		}
		err := ib.NormalizeForTxType()
		require.NoError(t, err)
		require.Equal(t, types.EvmZeroAddress, ib.Recipient)
		require.Nil(t, ib.UniversalPayload, "UV-sent universal_payload should be wiped; no raw_payload to decode")
	})

	t.Run("payload type with isCEA keeps recipient, wipes universal_payload", func(t *testing.T) {
		ib := types.Inbound{
			TxType:       types.TxType_FUNDS_AND_PAYLOAD,
			SourceChain:  "eip155:11155111",
			IsCEA:        true,
			Recipient:    "0x000000000000000000000000000000000000beef",
			UniversalPayload: &types.UniversalPayload{
				Data: "0x1234",
				To:   "0x000000000000000000000000000000000000abcd",
			},
		}
		err := ib.NormalizeForTxType()
		require.NoError(t, err)
		require.Equal(t, "0x000000000000000000000000000000000000beef", ib.Recipient)
		require.Nil(t, ib.UniversalPayload, "UV-sent universal_payload should be wiped")
	})

	t.Run("payload type with invalid raw_payload returns decode error", func(t *testing.T) {
		ib := types.Inbound{
			TxType:      types.TxType_FUNDS_AND_PAYLOAD,
			SourceChain: "eip155:11155111",
			RawPayload:  "0xdeadbeef", // too short to be valid ABI
		}
		err := ib.NormalizeForTxType()
		require.Error(t, err)
		require.Contains(t, err.Error(), "decode")
	})

	t.Run("non-payload type strips payload and raw_payload", func(t *testing.T) {
		ib := types.Inbound{
			TxType:           types.TxType_FUNDS,
			SourceChain:      "eip155:11155111",
			Recipient:        "0x000000000000000000000000000000000000beef",
			UniversalPayload: &types.UniversalPayload{Data: "0x1234"},
			VerificationData: "some_data",
			RawPayload:       "0xdeadbeef",
		}
		err := ib.NormalizeForTxType()
		require.NoError(t, err)
		require.Nil(t, ib.UniversalPayload)
		require.Empty(t, ib.VerificationData)
		require.Empty(t, ib.RawPayload)
		require.Equal(t, "0x000000000000000000000000000000000000beef", ib.Recipient)
	})
}

// TestInbound_VoteInboundValidationPipeline simulates the full validation pipeline
// as it runs inside VoteInbound: ValidateBasic → NormalizeForTxType → ValidateForExecution.
// This ensures that:
//   - ValidateBasic does NOT reject extra/irrelevant fields (UV is a dumb relay)
//   - NormalizeForTxType cleans up irrelevant fields before persisting
//   - ValidateForExecution catches real issues that should produce a failed PCTx + revert
func TestInbound_VoteInboundValidationPipeline(t *testing.T) {
	validPayload := &types.UniversalPayload{
		To:                   "0x000000000000000000000000000000000000beef",
		Value:                "1000",
		Data:                 "0x",
		GasLimit:             "21000",
		MaxFeePerGas:         "1000000000",
		MaxPriorityFeePerGas: "200000000",
		Nonce:                "1",
		Deadline:             "9999999999",
		VType:                types.VerificationType(1),
	}
	// ABI-encoded version of validPayload for use as raw_payload
	validRawPayload, err := abiEncodeUniversalPayload(
		common.HexToAddress("0x000000000000000000000000000000000000beef"),
		big.NewInt(1000), []byte{}, big.NewInt(21000),
		big.NewInt(1000000000), big.NewInt(200000000), big.NewInt(1), big.NewInt(9999999999), 1,
	)
	require.NoError(t, err)
	_ = validPayload // still used in some tests that verify UV-sent payload is wiped

	// runPipeline simulates VoteInbound: ValidateBasic → NormalizeForTxType (includes decode) → ValidateForExecution
	runPipeline := func(ib types.Inbound) (basicErr, execErr error, normalized types.Inbound) {
		basicErr = ib.ValidateBasic()
		if basicErr != nil {
			return basicErr, nil, ib
		}
		if normalizeErr := ib.NormalizeForTxType(); normalizeErr != nil {
			return nil, normalizeErr, ib
		}
		execErr = ib.ValidateForExecution()
		return nil, execErr, ib
	}

	t.Run("FUNDS with extra payload: passes basic, normalization strips payload, passes execution", func(t *testing.T) {
		ib := types.Inbound{
			SourceChain:      "eip155:11155111",
			TxHash:           "0xabc",
			Sender:           "0x000000000000000000000000000000000000dead",
			Recipient:        "0x000000000000000000000000000000000000beef",
			Amount:           "1000",
			AssetAddr:        "0x000000000000000000000000000000000000cafe",
			LogIndex:         "1",
			TxType:           types.TxType_FUNDS,
			UniversalPayload: validPayload,
			VerificationData: "0xsig",
		}
		basicErr, execErr, norm := runPipeline(ib)
		require.NoError(t, basicErr, "ValidateBasic should not reject extra payload on FUNDS")
		require.NoError(t, execErr, "ValidateForExecution should pass after normalization")
		require.Nil(t, norm.UniversalPayload, "payload should be stripped after normalization")
		require.Empty(t, norm.VerificationData, "verification_data should be stripped")
		require.Equal(t, "0x000000000000000000000000000000000000beef", norm.Recipient, "recipient should be preserved")
	})

	t.Run("FUNDS_AND_PAYLOAD non-isCEA with extra recipient: passes basic, normalization zeros recipient", func(t *testing.T) {
		ib := types.Inbound{
			SourceChain: "eip155:11155111",
			TxHash:      "0xabc",
			Sender:      "0x000000000000000000000000000000000000dead",
			Recipient:   "0x000000000000000000000000000000000000beef",
			Amount:      "1000",
			AssetAddr:   "0x000000000000000000000000000000000000cafe",
			LogIndex:    "1",
			TxType:      types.TxType_FUNDS_AND_PAYLOAD,
			RawPayload:  validRawPayload,
		}
		basicErr, execErr, norm := runPipeline(ib)
		require.NoError(t, basicErr)
		require.NoError(t, execErr)
		require.Equal(t, types.EvmZeroAddress, norm.Recipient, "recipient should be zero address for non-isCEA payload type")
		require.NotNil(t, norm.UniversalPayload, "payload should be decoded from raw_payload")
	})

	t.Run("FUNDS_AND_PAYLOAD isCEA with recipient: passes everything, recipient preserved", func(t *testing.T) {
		ib := types.Inbound{
			SourceChain: "eip155:11155111",
			TxHash:      "0xabc",
			Sender:      "0x000000000000000000000000000000000000dead",
			Recipient:   "0x000000000000000000000000000000000000beef",
			Amount:      "1000",
			AssetAddr:   "0x000000000000000000000000000000000000cafe",
			LogIndex:    "1",
			TxType:      types.TxType_FUNDS_AND_PAYLOAD,
			IsCEA:       true,
			RawPayload:  validRawPayload,
		}
		basicErr, execErr, norm := runPipeline(ib)
		require.NoError(t, basicErr)
		require.NoError(t, execErr)
		require.Equal(t, "0x000000000000000000000000000000000000beef", norm.Recipient, "isCEA recipient should be preserved")
	})

	t.Run("FUNDS with invalid amount: passes basic, fails execution", func(t *testing.T) {
		ib := types.Inbound{
			SourceChain: "eip155:11155111",
			TxHash:      "0xabc",
			Sender:      "0x000000000000000000000000000000000000dead",
			Recipient:   "0x000000000000000000000000000000000000beef",
			Amount:      "-100",
			AssetAddr:   "0x000000000000000000000000000000000000cafe",
			LogIndex:    "1",
			TxType:      types.TxType_FUNDS,
		}
		basicErr, execErr, _ := runPipeline(ib)
		require.NoError(t, basicErr, "ValidateBasic should not check amount")
		require.Error(t, execErr, "ValidateForExecution should catch invalid amount")
		require.Contains(t, execErr.Error(), "amount")
	})

	t.Run("FUNDS with empty recipient: passes basic, fails execution", func(t *testing.T) {
		ib := types.Inbound{
			SourceChain: "eip155:11155111",
			TxHash:      "0xabc",
			Sender:      "0x000000000000000000000000000000000000dead",
			Recipient:   "",
			Amount:      "1000",
			AssetAddr:   "0x000000000000000000000000000000000000cafe",
			LogIndex:    "1",
			TxType:      types.TxType_FUNDS,
		}
		basicErr, execErr, _ := runPipeline(ib)
		require.NoError(t, basicErr, "ValidateBasic should not check recipient")
		require.Error(t, execErr, "ValidateForExecution should catch missing recipient")
		require.Contains(t, execErr.Error(), "recipient cannot be empty")
	})

	t.Run("FUNDS with invalid recipient: passes basic, fails execution", func(t *testing.T) {
		ib := types.Inbound{
			SourceChain: "eip155:11155111",
			TxHash:      "0xabc",
			Sender:      "0x000000000000000000000000000000000000dead",
			Recipient:   "not-a-hex-addr",
			Amount:      "1000",
			AssetAddr:   "0x000000000000000000000000000000000000cafe",
			LogIndex:    "1",
			TxType:      types.TxType_FUNDS,
		}
		basicErr, execErr, _ := runPipeline(ib)
		require.NoError(t, basicErr, "ValidateBasic should not check recipient format")
		require.Error(t, execErr)
		require.Contains(t, execErr.Error(), "invalid recipient address")
	})

	t.Run("FUNDS_AND_PAYLOAD with missing payload: passes basic, fails execution", func(t *testing.T) {
		ib := types.Inbound{
			SourceChain: "eip155:11155111",
			TxHash:      "0xabc",
			Sender:      "0x000000000000000000000000000000000000dead",
			Amount:      "1000",
			AssetAddr:   "0x000000000000000000000000000000000000cafe",
			LogIndex:    "1",
			TxType:      types.TxType_FUNDS_AND_PAYLOAD,
		}
		basicErr, execErr, _ := runPipeline(ib)
		require.NoError(t, basicErr, "ValidateBasic should not check payload presence")
		require.Error(t, execErr, "ValidateForExecution should catch missing payload")
		require.Contains(t, execErr.Error(), "payload is required")
	})

	t.Run("FUNDS with empty asset_addr: passes basic, fails execution", func(t *testing.T) {
		ib := types.Inbound{
			SourceChain: "eip155:11155111",
			TxHash:      "0xabc",
			Sender:      "0x000000000000000000000000000000000000dead",
			Recipient:   "0x000000000000000000000000000000000000beef",
			Amount:      "1000",
			AssetAddr:   "",
			LogIndex:    "1",
			TxType:      types.TxType_FUNDS,
		}
		basicErr, execErr, _ := runPipeline(ib)
		require.NoError(t, basicErr, "ValidateBasic should not check asset_addr")
		require.Error(t, execErr)
		require.Contains(t, execErr.Error(), "asset_addr cannot be empty")
	})

	t.Run("isCEA on GAS type: passes basic, fails execution", func(t *testing.T) {
		ib := types.Inbound{
			SourceChain: "eip155:11155111",
			TxHash:      "0xabc",
			Sender:      "0x000000000000000000000000000000000000dead",
			Recipient:   "0x000000000000000000000000000000000000beef",
			Amount:      "1000",
			AssetAddr:   "0x000000000000000000000000000000000000cafe",
			LogIndex:    "1",
			TxType:      types.TxType_GAS,
			IsCEA:       true,
		}
		basicErr, execErr, _ := runPipeline(ib)
		require.NoError(t, basicErr, "ValidateBasic should not check isCEA compatibility")
		require.Error(t, execErr)
		require.Contains(t, execErr.Error(), "isCEA is only supported")
	})

	t.Run("missing source_chain: fails at basic level (vote rejected)", func(t *testing.T) {
		ib := types.Inbound{
			SourceChain: "",
			TxHash:      "0xabc",
			Sender:      "0x000000000000000000000000000000000000dead",
			Amount:      "1000",
			LogIndex:    "1",
			TxType:      types.TxType_FUNDS,
		}
		basicErr, _, _ := runPipeline(ib)
		require.Error(t, basicErr, "ValidateBasic should reject missing source_chain")
	})

	t.Run("missing tx_hash: fails at basic level (vote rejected)", func(t *testing.T) {
		ib := types.Inbound{
			SourceChain: "eip155:11155111",
			TxHash:      "",
			Sender:      "0x000000000000000000000000000000000000dead",
			Amount:      "1000",
			LogIndex:    "1",
			TxType:      types.TxType_FUNDS,
		}
		basicErr, _, _ := runPipeline(ib)
		require.Error(t, basicErr, "ValidateBasic should reject missing tx_hash")
	})

	t.Run("GAS_AND_PAYLOAD zero amount: passes full pipeline (payload-only execution)", func(t *testing.T) {
		ib := types.Inbound{
			SourceChain: "eip155:11155111",
			TxHash:      "0xabc",
			Sender:      "0x000000000000000000000000000000000000dead",
			Amount:      "0",
			AssetAddr:   "0x000000000000000000000000000000000000cafe",
			LogIndex:    "1",
			TxType:      types.TxType_GAS_AND_PAYLOAD,
			RawPayload:  validRawPayload,
		}
		basicErr, execErr, norm := runPipeline(ib)
		require.NoError(t, basicErr)
		require.NoError(t, execErr)
		require.Equal(t, types.EvmZeroAddress, norm.Recipient, "non-isCEA payload type should have zero address recipient")
	})

	t.Run("GAS zero amount: passes basic, fails execution (GAS needs positive amount)", func(t *testing.T) {
		ib := types.Inbound{
			SourceChain: "eip155:11155111",
			TxHash:      "0xabc",
			Sender:      "0x000000000000000000000000000000000000dead",
			Recipient:   "0x000000000000000000000000000000000000beef",
			Amount:      "0",
			AssetAddr:   "0x000000000000000000000000000000000000cafe",
			LogIndex:    "1",
			TxType:      types.TxType_GAS,
		}
		basicErr, execErr, _ := runPipeline(ib)
		require.NoError(t, basicErr)
		require.Error(t, execErr)
		require.Contains(t, execErr.Error(), "amount must be positive")
	})

	// --- raw_payload tests (new UV format) ---

	t.Run("FUNDS_AND_PAYLOAD with raw_payload: decoded into universal_payload, passes execution", func(t *testing.T) {
		rawPayload, err := abiEncodeUniversalPayload(
			common.HexToAddress("0x000000000000000000000000000000000000beef"),
			big.NewInt(1000), []byte{0xde, 0xad}, big.NewInt(21000),
			big.NewInt(1e9), big.NewInt(2e8), big.NewInt(1), big.NewInt(9999999999), 1,
		)
		require.NoError(t, err)

		ib := types.Inbound{
			SourceChain: "eip155:11155111",
			TxHash:      "0xraw01",
			Sender:      "0x000000000000000000000000000000000000dead",
			Amount:      "1000",
			AssetAddr:   "0x000000000000000000000000000000000000cafe",
			LogIndex:    "1",
			TxType:      types.TxType_FUNDS_AND_PAYLOAD,
			RawPayload:  rawPayload,
		}
		basicErr, execErr, norm := runPipeline(ib)
		require.NoError(t, basicErr)
		require.NoError(t, execErr)
		require.NotNil(t, norm.UniversalPayload, "raw_payload should be decoded into universal_payload")
		require.Equal(t, "1000", norm.UniversalPayload.Value)
		require.Equal(t, "0xdead", norm.UniversalPayload.Data)
		require.Empty(t, norm.RawPayload, "raw_payload should be cleared after decode")
	})

	t.Run("FUNDS_AND_PAYLOAD with invalid raw_payload: decode fails", func(t *testing.T) {
		ib := types.Inbound{
			SourceChain: "eip155:11155111",
			TxHash:      "0xraw02",
			Sender:      "0x000000000000000000000000000000000000dead",
			Amount:      "1000",
			AssetAddr:   "0x000000000000000000000000000000000000cafe",
			LogIndex:    "1",
			TxType:      types.TxType_FUNDS_AND_PAYLOAD,
			RawPayload:  "0xdeadbeef", // too short to be valid ABI
		}
		basicErr, execErr, _ := runPipeline(ib)
		require.NoError(t, basicErr)
		require.Error(t, execErr, "invalid raw_payload should fail at decode step")
		require.Contains(t, execErr.Error(), "decode")
	})

	t.Run("FUNDS_AND_PAYLOAD with both raw_payload and universal_payload: raw takes precedence", func(t *testing.T) {
		rawPayload, err := abiEncodeUniversalPayload(
			common.HexToAddress("0x000000000000000000000000000000000000abcd"),
			big.NewInt(9999), []byte{}, big.NewInt(21000),
			big.NewInt(1e9), big.NewInt(2e8), big.NewInt(0), big.NewInt(0), 0,
		)
		require.NoError(t, err)

		ib := types.Inbound{
			SourceChain:      "eip155:11155111",
			TxHash:           "0xraw03",
			Sender:           "0x000000000000000000000000000000000000dead",
			Amount:           "1000",
			AssetAddr:        "0x000000000000000000000000000000000000cafe",
			LogIndex:         "1",
			TxType:           types.TxType_FUNDS_AND_PAYLOAD,
			UniversalPayload: validPayload,                   // old format (will be cleared by normalize)
			RawPayload:       rawPayload,                     // new format (takes precedence)
		}
		basicErr, execErr, norm := runPipeline(ib)
		require.NoError(t, basicErr)
		require.NoError(t, execErr)
		require.NotNil(t, norm.UniversalPayload)
		require.Equal(t, "9999", norm.UniversalPayload.Value, "raw_payload value should take precedence")
	})

	t.Run("FUNDS_AND_PAYLOAD with only universal_payload (no raw_payload): UV payload is wiped, fails execution", func(t *testing.T) {
		ib := types.Inbound{
			SourceChain:      "eip155:11155111",
			TxHash:           "0xraw04",
			Sender:           "0x000000000000000000000000000000000000dead",
			Amount:           "1000",
			AssetAddr:        "0x000000000000000000000000000000000000cafe",
			LogIndex:         "1",
			TxType:           types.TxType_FUNDS_AND_PAYLOAD,
			UniversalPayload: validPayload, // UV sends decoded payload directly — will be wiped
		}
		basicErr, execErr, _ := runPipeline(ib)
		require.NoError(t, basicErr)
		require.Error(t, execErr, "UV-sent universal_payload is wiped; raw_payload is required")
		require.Contains(t, execErr.Error(), "payload is required")
	})

	t.Run("FUNDS with raw_payload: normalization strips it (non-payload type)", func(t *testing.T) {
		ib := types.Inbound{
			SourceChain: "eip155:11155111",
			TxHash:      "0xraw05",
			Sender:      "0x000000000000000000000000000000000000dead",
			Recipient:   "0x000000000000000000000000000000000000beef",
			Amount:      "1000",
			AssetAddr:   "0x000000000000000000000000000000000000cafe",
			LogIndex:    "1",
			TxType:      types.TxType_FUNDS,
			RawPayload:  "0xdeadbeef",
		}
		basicErr, execErr, norm := runPipeline(ib)
		require.NoError(t, basicErr)
		require.NoError(t, execErr)
		require.Empty(t, norm.RawPayload, "raw_payload should be cleared for non-payload type")
	})
}
