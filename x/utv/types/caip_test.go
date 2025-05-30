package types_test

import (
	"testing"

	"github.com/push-protocol/push-chain/x/utv/types"
	"github.com/stretchr/testify/require"
)

func TestParseCAIPAddress(t *testing.T) {
	testCases := []struct {
		name          string
		caipAddress   string
		expectedCAIP  *types.CAIPAddress
		expectedError bool
	}{
		{
			name:        "Valid Ethereum CAIP address",
			caipAddress: "eip155:1:0x1234567890abcdef1234567890abcdef12345678",
			expectedCAIP: &types.CAIPAddress{
				Namespace: "eip155",
				Reference: "1",
				Address:   "0x1234567890abcdef1234567890abcdef12345678",
			},
			expectedError: false,
		},
		{
			name:        "Valid Solana CAIP address",
			caipAddress: "solana:mainnet:5hAykmD4YGcQ7hNCeU6qMrjS1TDD5icwYt2k9LwqYtF3",
			expectedCAIP: &types.CAIPAddress{
				Namespace: "solana",
				Reference: "mainnet",
				Address:   "5hAykmD4YGcQ7hNPeU6qMrjS1TDD5icwYt2k9LwqYtF3",
			},
			expectedError: false,
		},
		{
			name:          "Invalid CAIP address - missing address part",
			caipAddress:   "eip155:1",
			expectedCAIP:  nil,
			expectedError: true,
		},
		{
			name:          "Invalid CAIP address - completely empty",
			caipAddress:   "",
			expectedCAIP:  nil,
			expectedError: true,
		},
		{
			name:          "Invalid CAIP address - wrong format",
			caipAddress:   "invalid-format",
			expectedCAIP:  nil,
			expectedError: true,
		},
		{
			name:        "CAIP address with extra colon parts",
			caipAddress: "eip155:1:0x1234:extra:parts",
			expectedCAIP: &types.CAIPAddress{
				Namespace: "eip155",
				Reference: "1",
				Address:   "0x1234:extra:parts",
			},
			expectedError: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			caip, err := types.ParseCAIPAddress(tc.caipAddress)

			if tc.expectedError {
				require.Error(t, err)
				require.Nil(t, caip)
			} else {
				require.NoError(t, err)
				require.NotNil(t, caip)
				require.Equal(t, tc.expectedCAIP.Namespace, caip.Namespace)
				require.Equal(t, tc.expectedCAIP.Reference, caip.Reference)
				require.Equal(t, tc.expectedCAIP.Address, caip.Address)
			}
		})
	}
}

func TestCAIPAddressGetChainIdentifier(t *testing.T) {
	testCases := []struct {
		name               string
		caip               types.CAIPAddress
		expectedIdentifier string
	}{
		{
			name: "Ethereum Mainnet",
			caip: types.CAIPAddress{
				Namespace: "eip155",
				Reference: "1",
				Address:   "0x1234567890abcdef1234567890abcdef12345678",
			},
			expectedIdentifier: "eip155:1",
		},
		{
			name: "Solana Mainnet",
			caip: types.CAIPAddress{
				Namespace: "solana",
				Reference: "mainnet",
				Address:   "5hAykmD4YGcQ7hNPeU6qMrjS1TDD5icwYt2k9LwqYtF3",
			},
			expectedIdentifier: "solana:mainnet",
		},
		{
			name: "Empty values",
			caip: types.CAIPAddress{
				Namespace: "",
				Reference: "",
				Address:   "",
			},
			expectedIdentifier: ":",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			identifier := tc.caip.GetChainIdentifier()
			require.Equal(t, tc.expectedIdentifier, identifier)
		})
	}
}

func TestCAIPAddressString(t *testing.T) {
	testCases := []struct {
		name           string
		caip           types.CAIPAddress
		expectedString string
	}{
		{
			name: "Ethereum Mainnet",
			caip: types.CAIPAddress{
				Namespace: "eip155",
				Reference: "1",
				Address:   "0x1234567890abcdef1234567890abcdef12345678",
			},
			expectedString: "eip155:1:0x1234567890abcdef1234567890abcdef12345678",
		},
		{
			name: "Solana Mainnet",
			caip: types.CAIPAddress{
				Namespace: "solana",
				Reference: "mainnet",
				Address:   "5hAykmD4YGcQ7hNPeU6qMrjS1TDD5icwYt2k9LwqYtF3",
			},
			expectedString: "solana:mainnet:5hAykmD4YGcQ7hNPeU6qMrjS1TDD5icwYt2k9LwqYtF3",
		},
		{
			name: "Empty values",
			caip: types.CAIPAddress{
				Namespace: "",
				Reference: "",
				Address:   "",
			},
			expectedString: "::",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			str := tc.caip.String()
			require.Equal(t, tc.expectedString, str)
		})
	}
}
