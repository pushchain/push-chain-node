# UTxHashVerifier (Universal Tx Hash Verifier) Precompile

The UTxHashVerifier precompile provides transaction verification functionality for multiple chains, specifically Ethereum and Solana. It verifies that a transaction hash contains an event with a specific payload hash, ensuring cross-chain transaction integrity.

## Address

```
0x00000000000000000000000000000000000000CB
```

## Functions

### `verifyTxHash`

Verifies that a transaction contains a `FundsAdded` event with the specified payload hash.

```solidity
function verifyTxHash(
    string chainNamespace,
    string chainId,
    bytes owner,
    bytes32 payloadHash,
    bytes txHash
) external returns (bool);
```

#### Parameters

- `chainNamespace`: The chain namespace ("eip155" for Ethereum, "solana" for Solana)
- `chainId`: The specific chain ID ("11155111" for Sepolia, "devnet" for Solana devnet)
- `owner`: The owner's address/key as bytes
- `payloadHash`: The expected payload hash as bytes32
- `txHash`: The transaction hash as bytes (supports both Ethereum hex and Solana base58 formats)

#### Returns

- `bool`: `true` if verification succeeds, `false` otherwise

## Verification Process

### Architecture Overview

The UTxHashVerifier precompile delegates verification to the UTxVerifier (Universal Transaction Verification) module for gas efficiency. The verification process involves:

1. **Chain Detection**: Extracts chain information from chainNamespace parameter
2. **Owner Verification**: Ensures the transaction sender matches the provided owner
3. **Event Parsing**: Extracts payload hash from blockchain events
4. **Hash Comparison**: Compares provided hash with emitted hash
5. **Transaction Hash Processing**: Converts bytes to appropriate string format based on chain type

### Chain-Specific Implementation

#### Ethereum (EIP155)
- **RPC Endpoint**: `https://eth-sepolia.public.blastapi.io`
- **Gateway Contract**: `0x28E0F09bE2321c1420Dc60Ee146aACbD68B335Fe`
- **Event Parsing**: Uses `FundsAdded(address,bytes32,(uint256,uint8))` event signature
- **Hash Extraction**: Retrieves transaction hash from event topics[2]
- **txHash Format**: Bytes are converted to hex string format

#### Solana 
- **RPC Endpoint**: `https://api.devnet.solana.com`
- **Gateway Contract**: `3zrWaMknHTRQpZSxY4BvQxw9TStSXiHcmcp3NMPTFkke`
- **Event Parsing**: Decodes base64 "Program data:" logs using binary structure
- **Hash Extraction**: Parses FundsAddedEvent struct at specific byte offsets
- **txHash Format**: Bytes are converted to base58 string format

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
cast call 0x00000000000000000000000000000000000000CB \
  "verifyTxHash(string,string,bytes,bytes32,bytes)" \
  "eip155" \
  "11155111" \
  "0x498ad773E5dAd9388268c1477A7df26Bf03114A0" \
  "0xfdb80105de4e3eafa1f1d2c3b7a513752acb21e1ccd19d87ba8e413d670e1e27" \
  "0xc6e1abcfc0898fd9426904336a2a565908ab934b979ddd85a19044ab813a84d4"
```

### Solana Verification
```bash
cast call 0x00000000000000000000000000000000000000CB \
  "verifyTxHash(string,string,bytes,bytes32,bytes)" \
  "solana" \
  "EtWTRABZaYq6iMfeYKouRu166VU2xqa1" \
  "0x123f8bdd2850b76cd7d612ba9f5b4a1d05a66e39805048ccd12b7fbef3f69bbc" \
  "0xfdb80105de4e3eafa1f1d2c3b7a513752acb21e1ccd19d87ba8e413d670e1e27" \
  "0xfae0e6094f6af71e3816187c6d137cdee22bb0ae81921a2890c2c9ef466f4b56b99b1cd022d8f9631de1acb463ec8a67b8b2ac5cca717fba57c4e0245093160b"
```

## Key Implementation Features

### 1. Flattened Parameter Structure
- **Direct Parameters**: Uses individual parameters instead of tuple structure
- **Type Safety**: Proper type matching for Solidity contract integration
- **Simplified Encoding**: No complex struct creation required

### 2. Multi-Format Transaction Hash Support
- **Ethereum**: Expects transaction hash as hex-encoded bytes
- **Solana**: Expects transaction hash as base58-decoded bytes
- **Automatic Conversion**: Precompile converts bytes to appropriate string format based on chain

### 3. Efficient Architecture
- **Single RPC Call**: Fetches transaction data once and performs all verifications
- **Gas Optimization**: Delegates to UtxverifierKeeper moduledule to avoid expensive precompile operations
- **Error Handling**: Comprehensive error messages for debugging

### 4. Robust Event Parsing
- **Ethereum**: Uses event signature matching and topic extraction
- **Solana**: Binary parsing of base64-encoded event data with proper struct layout
- **Type Safety**: Validates data lengths and formats before parsing

## Transaction Hash Format Handling

### Ethereum
Your contract should convert hex string to bytes:
```solidity
string memory ethTxHash = "0xc6e1abcfc0898fd9426904336a2a565908ab934b979ddd85a19044ab813a84d4";
bytes memory txHashBytes = hex"c6e1abcfc0898fd9426904336a2a565908ab934b979ddd85a19044ab813a84d4";
```

### Solana
Your contract should convert base58 string to bytes (requires base58 library):
```solidity
string memory solanaTxHash = "61vNSsAFmLAifCKauxUM8bhCJHLkesxszkQNjL9iqRbZ3E9SWVA8r3biz3BjEgZHvykfMivAV5uu83po5Metxg8J";
bytes memory txHashBytes = base58.decode(solanaTxHash);
```

## Contract Integration

```solidity
struct UniversalAccountId {
    string chainNamespace;
    string chainId;
    bytes owner;
}

function verifyTransaction(
    UniversalAccountId memory id,
    bytes32 payloadHash,
    bytes memory txHashBytes
) external returns (bool) {
    (bool success, bytes memory result) = UTxHashVerifier_PRECOMPILE.staticcall(
        abi.encodeWithSignature(
            "verifyTxHash(string,string,bytes,bytes32,bytes)",
            id.chainNamespace,
            id.chainId,
            id.owner,
            payloadHash,
            txHashBytes
        )
    );
    
    require(success, "Precompile call failed");
    return abi.decode(result, (bool));
}
```

## Error Handling

The precompile returns `false` and logs detailed error information for:

- **Invalid Parameters**: Malformed chainNamespace, chainId, or owner format
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
- **UtxverifierKeeper moduledule**: Core verification logic and RPC utilities
- **EVM RPC**: Ethereum transaction and receipt fetching
- **SVM RPC**: Solana transaction and log parsing
- **Context Support**: Proper timeout and cancellation handling
- **Base58 Encoding**: For Solana transaction hash conversion

### Code Structure
```
precompiles/utxhashverifier/
├── abi.json          # Contract ABI definition
├── utxhashverifier.go           # Main precompile logic
├── query.go         # VerifyTxHash implementation
├── types.go         # Type definitions and interfaces
└── README.md        # This documentation
```

### Key Functions
- `VerifyTxHash`: Main verification entry point with flattened parameters
- `NewPrecompileWithUtv`: Factory with UTV keeper injection
- `RequiredGas`: Gas cost calculation (4000 gas units)

## Integration with UtxverifierKeeper moduledule

The UTxHashVerifier precompile integrates with the UtxVerifier (Universal Transaction Verification) module:

```go
// UtxverifierKeeper moduledule provides the core verification logic
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