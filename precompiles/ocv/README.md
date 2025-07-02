# OCV (Owner and Chain Verification) Precompile

The OCV precompile provides transaction verification functionality for multiple chains, specifically Ethereum and Solana. It verifies that a transaction hash contains an event with a specific payload hash, ensuring cross-chain transaction integrity.

## Address

```
0x0000000000000000000000000000000000000901
```

## Functions

### `verifyTxHash`

Verifies that a transaction contains a `FundsAdded` event with the specified payload hash.

```solidity
function verifyTxHash(
    (string chainNamespace, string chainId, bytes owner) universalAccountId,
    bytes payloadHash,
    string txHash
) external returns (bool);
```

#### Parameters

- `universalAccountId`: A tuple containing:
  - `chainNamespace`: The chain namespace ("eip155" for Ethereum, "solana" for Solana)
  - `chainId`: The specific chain ID ("11155111" for Sepolia, "devnet" for Solana devnet)
  - `owner`: The owner's address/key as bytes
- `payloadHash`: The expected payload hash as bytes
- `txHash`: The transaction hash as a string (native format for each chain)

#### Returns

- `bool`: `true` if verification succeeds, `false` otherwise

## Verification Process

### Architecture Overview

The OCV precompile delegates verification to the UTV (Universal Transaction Verification) module for gas efficiency. The verification process involves:

1. **Chain Detection**: Extracts chain information from Universal Account ID
2. **Owner Verification**: Ensures the transaction sender matches the provided owner
3. **Event Parsing**: Extracts payload hash from blockchain events
4. **Hash Comparison**: Compares provided hash with emitted hash

### Chain-Specific Implementation

#### Ethereum (EIP155)
- **RPC Endpoint**: `https://eth-sepolia.public.blastapi.io`
- **Gateway Contract**: `0x28E0F09bE2321c1420Dc60Ee146aACbD68B335Fe`
- **Event Parsing**: Uses `FundsAdded(address,bytes32,(uint256,uint8))` event signature
- **Hash Extraction**: Retrieves transaction hash from event topics[2]

#### Solana 
- **RPC Endpoint**: `https://api.devnet.solana.com`
- **Gateway Contract**: `3zrWaMknHTRQpZSxY4BvQxw9TStSXiHcmcp3NMPTFkke`
- **Event Parsing**: Decodes base64 "Program data:" logs using binary structure
- **Hash Extraction**: Parses FundsAddedEvent struct at specific byte offsets

### Solana Event Structure

The Solana verification parses the `FundsAddedEvent` with the following structure:

```rust
#[event]
pub struct FundsAddedEvent {
    pub user: Pubkey,               // [8:40]   - 32 bytes
    pub sol_amount: u64,            // [40:48]  - 8 bytes  
    pub usd_equivalent: i128,       // [48:64]  - 16 bytes
    pub usd_exponent: i32,          // [64:68]  - 4 bytes
    pub transaction_hash: [u8; 32], // [68:100] - 32 bytes (target field)
}
```

The precompile extracts the `transaction_hash` field from bytes 68-100 of the decoded event data.

## Usage Examples

### Ethereum Verification
```bash
cast call 0x0000000000000000000000000000000000000901 \
  "verifyTxHash((string,string,bytes),bytes,string)" \
  "("eip155","11155111","0x498ad773E5dAd9388268c1477A7df26Bf03114A0")" \
  "0xfdb80105de4e3eafa1f1d2c3b7a513752acb21e1ccd19d87ba8e413d670e1e27" \
  "0xc6e1abcfc0898fd9426904336a2a565908ab934b979ddd85a19044ab813a84d4"
```

### Solana Verification
```bash
cast call 0x0000000000000000000000000000000000000901 \
  "verifyTxHash((string,string,bytes),bytes,string)" \
  "("solana","EtWTRABZaYq6iMfeYKouRu166VU2xqa1","0x123f8bdd2850b76cd7d612ba9f5b4a1d05a66e39805048ccd12b7fbef3f69bbc")" \
  "fdb80105de4e3eafa1f1d2c3b7a513752acb21e1ccd19d87ba8e413d670e1e27" \
  "61vNSsAFmLAifCKauxUM8bhCJHLkesxszkQNjL9iqRbZ3E9SWVA8r3biz3BjEgZHvykfMivAV5uu83po5Metxg8J"
```

## Key Implementation Features

### 1. Universal Account ID Structure
- **Standardized Format**: Uses tuple `(chainNamespace, chainId, owner)` for cross-chain compatibility
- **Chain Detection**: Automatically determines verification method from namespace
- **Address Formats**: Supports both Ethereum hex addresses and Solana public keys

### 2. Efficient Architecture
- **Single RPC Call**: Fetches transaction data once and performs all verifications
- **Gas Optimization**: Delegates to UTV module to avoid expensive precompile operations
- **Error Handling**: Comprehensive error messages for debugging

### 3. Robust Event Parsing
- **Ethereum**: Uses event signature matching and topic extraction
- **Solana**: Binary parsing of base64-encoded event data with proper struct layout
- **Type Safety**: Validates data lengths and formats before parsing

### 4. Transaction Hash Formats
- **Ethereum**: Standard hex format with 0x prefix
- **Solana**: Base58 encoded transaction signatures
- **Standardization**: Both chains use string format for consistency

## Error Handling

The precompile returns `false` and logs detailed error information for:

- **Invalid Universal Account ID**: Malformed chain namespace, ID, or owner format
- **Chain Configuration**: Unsupported chains or missing RPC endpoints
- **Transaction Verification**: Invalid transaction hashes or network errors
- **Owner Mismatch**: Transaction sender doesn't match provided owner
- **Event Parsing**: Missing or malformed FundsAdded events
- **Hash Mismatch**: Provided payload hash doesn't match emitted hash

## Configuration

### Supported Chains

| Chain Namespace | Chain ID | Network | RPC Endpoint |
|----------------|----------|---------|--------------|
| `eip155` | `11155111` | Ethereum Sepolia | `https://eth-sepolia.public.blastapi.io` |
| `solana` | `devnet` | Solana Devnet | `https://api.devnet.solana.com` |

### Gateway Contracts

| Chain | Contract Address |
|-------|------------------|
| Ethereum Sepolia | `0x28E0F09bE2321c1420Dc60Ee146aACbD68B335Fe` |
| Solana Devnet | `3zrWaMknHTRQpZSxY4BvQxw9TStSXiHcmcp3NMPTFkke` |

## Implementation Details

### Dependencies
- **UTV Module**: Core verification logic and RPC utilities
- **EVM RPC**: Ethereum transaction and receipt fetching
- **SVM RPC**: Solana transaction and log parsing
- **Context Support**: Proper timeout and cancellation handling

### Code Structure
```
precompiles/ocv/
├── abi.json          # Contract ABI definition
├── ocv.go           # Main precompile logic
├── query.go         # VerifyTxHash implementation
├── types.go         # Type definitions and interfaces
└── README.md        # This documentation
```

### Key Functions
- `VerifyTxHash`: Main verification entry point
- `NewPrecompileWithUtv`: Factory with UTV keeper injection
- `RequiredGas`: Gas cost calculation (4000 gas units)

## Integration with UTV Module

The OCV precompile integrates with the UTV (Universal Transaction Verification) module:

```go
// UTV module provides the core verification logic
func (k Keeper) VerifyTxHashWithPayload(
    ctx context.Context, 
    universalAccountId UniversalAccount, 
    payloadHash, txHash string
) (bool, error)
```

This delegation ensures:
- **Gas Efficiency**: Expensive operations run in native Go code
- **Code Reuse**: Shared verification logic across modules
- **Maintainability**: Centralized chain configuration and RPC handling

## Testing

The precompile can be tested using:

1. **Unit Tests**: Verify ABI encoding/decoding and input validation
2. **Integration Tests**: Test with real blockchain transactions
3. **Cast Commands**: Direct precompile calls for manual testing

Example test transaction hashes are provided in the usage examples above for both Ethereum and Solana networks. 