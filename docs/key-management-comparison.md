# Key Management Comparison: pchaind vs Zetaclient vs Universal Validator Proposal

## Table of Contents
1. [Overview](#overview)
2. [pchaind Key Management](#pchaind-key-management)
3. [How Keys are Tied to Validators in pchaind](#how-keys-are-tied-to-validators-in-pchaind)
4. [Zetaclient Key Management](#zetaclient-key-management)
5. [Architecture Comparison](#architecture-comparison)
6. [Universal Validator Proposal](#universal-validator-proposal)

## Overview

This document provides a comprehensive analysis of key management approaches in pchaind (Push Chain validator node) and Zetaclient (Zeta observer client), along with a proposal for Universal Validator implementation. The analysis focuses on security models, architectural patterns, and practical implementation considerations.

## pchaind Key Management

### Purpose and Architecture
pchaind is a Cosmos SDK-based blockchain validator node that participates in consensus. It follows the standard Cosmos validator architecture with a clear separation between consensus operations and transaction signing.

### Key Types

#### 1. Consensus Key (Validator Key)
- **Type**: Ed25519 key pair
- **Location**: `~/.pchain/config/priv_validator_key.json`
- **Purpose**: Signs blocks during consensus participation
- **Creation**: Automatically generated during `pchaind init`
- **Usage**: Used by Tendermint/CometBFT consensus engine
- **Security**: Should remain on the validator node (hot key)

#### 2. Operator Key (Account Key)
- **Type**: Secp256k1 key pair
- **Location**: Cosmos keyring (test, file, or OS backend)
- **Purpose**: Signs validator-related transactions (create validator, edit validator, delegation operations)
- **Creation**: Manually created by operator using `pchaind keys add`
- **Usage**: Used for all on-chain transactions
- **Security**: Can be kept offline (cold storage) after initial validator setup

### Key Management System

#### Keyring Integration
- Uses standard Cosmos SDK keyring with EVM extensions
- Supports multiple backends:
  - **test**: Unencrypted, for development only
  - **file**: Encrypted file storage with password protection
  - **os**: Uses operating system's native keyring (macOS Keychain, Linux Secret Service, Windows Credential Manager)
- Integrates `cosmosevmkeyring` for Ethereum compatibility

#### Key Commands
- `keys add`: Create new keys
- `keys list`: List all keys in keyring
- `keys show`: Display key information
- `keys delete`: Remove keys
- `keys export/import`: Backup and restore keys

### Security Model
- **Simple and Direct**: Validator directly owns and controls all keys
- **Cold/Hot Separation**: Operator keys can be stored offline after validator creation
- **Single Password**: One password for keyring access (in file backend)
- **No Delegation**: Validator signs with its own keys directly

## How Keys are Tied to Validators in pchaind

Understanding how keys are associated with validators is crucial for comprehending the security model of pchaind. The process involves a permanent binding between two distinct key types.

### The Two-Key System

In pchaind, validators operate with two separate keys that serve different purposes:

1. **Consensus Key (Ed25519)**
   - This is the validator's identity in the consensus layer
   - Used to sign blocks and consensus messages
   - Must remain online (hot) for the validator to participate
   - Generated automatically during node initialization

2. **Operator Key (Secp256k1)**
   - This is the validator's identity for on-chain transactions
   - Controls the validator account and its stake
   - Used to sign validator-related transactions
   - Can be kept offline (cold) after validator setup

### The Binding Process

The association between these keys happens during validator creation through a specific transaction. Here's how it works:

#### Step 1: Node Initialization
When you run `pchaind init`, the node automatically generates a consensus key pair:
- Private key is stored in `~/.pchain/config/priv_validator_key.json`
- Public key can be retrieved using `pchaind tendermint show-validator`
- This key has a unique address derived from the public key

#### Step 2: Operator Key Creation
The operator manually creates an account key:
- Run `pchaind keys add <key-name>` to create a new key
- This key is stored in the Cosmos keyring
- The key controls funds and validator operations
- Has its own address for receiving tokens and rewards

#### Step 3: Validator Creation Transaction
The binding happens when the operator submits a `create-validator` transaction:
- The transaction is signed by the operator key
- It includes the consensus public key in the transaction
- Once accepted on-chain, this creates a permanent association

The transaction structure looks like:
- **From**: Operator address (who signs the transaction)
- **Pubkey**: Consensus public key (identifies the node)
- **Amount**: Initial self-delegation
- **Commission**: Validator commission parameters
- **Details**: Validator metadata

#### Step 4: On-Chain Record
Once the transaction is processed:
- A validator object is created on-chain
- It permanently links the operator address to the consensus pubkey
- The consensus key can now participate in block production
- The operator key controls all validator operations

### Important Characteristics

#### Permanent Binding
- Once created, the consensus key cannot be changed for a validator
- The operator can update validator details but not the consensus key
- To change consensus key, you must create a new validator

#### Security Implications
- **Consensus Key Risk**: If compromised, attacker can sign blocks incorrectly, leading to slashing
- **Operator Key Risk**: If compromised, attacker can control stake, withdraw rewards, or unbond
- **Separation Benefits**: Operator key can be cold, limiting exposure

#### Operational Considerations
- **Key Backup**: Both keys must be backed up, but separately
- **Key Rotation**: Only operator key can be changed (through key migration)
- **Slashing Protection**: Consensus key compromise leads to byzantine behavior slashing
- **Recovery**: Lost consensus key means creating new validator; lost operator key means loss of control

### Verification Methods

You can verify the key association in several ways:

1. **Query by Validator Address**
   - Use the operator address to find validator info
   - Returns consensus pubkey and validator details

2. **Query by Consensus Key**
   - Search validators by consensus public key
   - Finds the associated operator address

3. **Check Local Node**
   - `pchaind tendermint show-validator` shows local consensus key
   - Compare with on-chain validator records

### Why This Design?

This two-key system is standard in Cosmos SDK chains because:

1. **Security Separation**: Different risk profiles for different operations
2. **Operational Flexibility**: Operator can be offline while validator runs
3. **Clear Responsibilities**: Consensus for protocol, operator for business
4. **Industry Standard**: Proven pattern across many Cosmos chains

The binding mechanism ensures that:
- Each validator has a unique identity in consensus
- Stake ownership is clearly defined
- Validator operations are controlled by the stake owner
- The system can slash misbehaving validators accurately

## Zetaclient Key Management

### Purpose and Architecture
Zetaclient is an observer client that monitors multiple blockchains and coordinates cross-chain operations. It uses a sophisticated key management system to handle multiple chains and security requirements.

### Key Types

#### 1. Hot Key (Delegated Signer)
- **Type**: Secp256k1 key pair
- **Location**: Cosmos keyring
- **Purpose**: Signs cross-chain observations and votes
- **Security**: Uses AuthZ for delegated authority from operator
- **Creation**: Created during zetaclient setup

#### 2. TSS Key (Threshold Signature Scheme)
- **Type**: ECDSA key shares
- **Location**: Encrypted file storage
- **Purpose**: Participates in distributed signing for cross-chain messages
- **Security**: No single party has complete key; requires threshold participation
- **Creation**: Generated through distributed key generation protocol

#### 3. Relayer Keys (Chain-Specific)
- **Type**: Chain-specific formats (Solana Ed25519, EVM Secp256k1, etc.)
- **Location**: `~/.zetacored/relayer-keys/`
- **Purpose**: Sign transactions on external chains
- **Security**: Encrypted with AES-256-GCM using chain-specific passwords
- **Creation**: Imported by operator for each supported chain

### Key Management System

#### Multi-Level Security
1. **Operator Account** (Cold)
   - Main validator account with funds and voting power
   - Grants limited permissions to hot key via AuthZ
   - Can remain offline after delegation

2. **Hot Key** (Warm)
   - Operates with delegated permissions
   - Limited to specific message types
   - Cannot access operator's funds directly

3. **Chain Keys** (Hot)
   - Encrypted storage for each blockchain
   - Separate passwords per chain
   - Loaded into memory during operation

#### AuthZ (Authorization) System
- Operator grants specific permissions to hot key
- Permissions limited to:
  - Voting on observed transactions
  - Updating observer parameters
  - TSS-related operations
- Cannot perform validator operations or token transfers

#### Password Management
- **Multiple Passwords Required**:
  1. Hot key password (keyring access)
  2. TSS key password (TSS operations)
  3. Chain-specific passwords (one per supported chain)
- Passwords prompted at startup
- Stored in memory during runtime

### Advanced Features

#### TSS (Threshold Signature Scheme)
- Enables distributed signing without revealing complete private keys
- Requires coordination between multiple validators
- Provides security against single point of failure
- Used for high-value cross-chain operations

#### Relayer Key System
- Modular design supporting multiple chains
- Each chain has its own encrypted key file
- Keys are chain-specific (different cryptographic schemes)
- Supports dynamic loading/unloading of chain support

## Architecture Comparison

### Fundamental Differences

| Aspect | pchaind | Zetaclient |
|--------|---------|------------|
| **Primary Role** | Blockchain Validator | Cross-chain Observer |
| **Architecture** | Monolithic validator node | Modular observer client |
| **Key Ownership** | Direct ownership | Delegated authority |
| **Security Model** | Simple hot/cold separation | Multi-layer with AuthZ + TSS |
| **Chain Support** | Single chain (Push Chain) | Multiple chains simultaneously |
| **Key Types** | 2 (consensus, operator) | 3+ (hot, TSS, multiple relayer) |
| **Password Complexity** | Single password | Multiple passwords |

### Use Case Alignment

#### pchaind Use Case
- Single chain validation
- Direct block production
- Simple validator operations
- Standard Cosmos SDK patterns

#### Zetaclient Use Case
- Multi-chain observation
- Cross-chain message coordination
- Distributed security requirements
- Complex operational needs

### Security Considerations

#### pchaind Security
- **Strengths**:
  - Simple to understand and audit
  - Standard Cosmos security model
  - Clear key separation
  - Minimal attack surface

- **Limitations**:
  - Hot consensus key always online
  - No built-in delegation mechanism
  - Single chain focus

#### Zetaclient Security
- **Strengths**:
  - Sophisticated delegation model
  - Distributed security via TSS
  - Granular permission control
  - Multi-chain isolation

- **Limitations**:
  - Complex to set up and maintain
  - Multiple passwords to manage
  - Larger attack surface
  - More points of failure

## Universal Validator Proposal

Based on the analysis and the fact that Universal Validator is architecturally similar to Zetaclient (multi-chain observer), we propose adopting a Zetaclient-style architecture with appropriate modifications.

### Recommended Architecture

#### Core Components

1. **Keyring System**
   - Use Cosmos SDK keyring for hot key management
   - Support file and test backends initially
   - Implement OS backend for production

2. **AuthZ Integration**
   - Implement delegated authority pattern
   - Operator account delegates to hot key
   - Define specific permissions for universal validation

3. **Chain-Specific Keys**
   - Encrypted storage for each supported chain
   - Use AES-256-GCM encryption (same as Zetaclient)
   - Separate passwords per chain for security isolation

4. **Optional TSS Support**
   - Design system to accommodate TSS in future
   - Not required for initial implementation
   - Can be added when multi-validator coordination needed

### Implementation Approach

#### Phase 1: Basic Key Management
1. **Hot Key System**
   - Create hot key in Cosmos keyring
   - Implement basic password protection
   - Store operator address for reference

2. **Chain Key Storage**
   - Design encrypted storage format
   - Implement key import/export commands
   - Support initial chains (EVM, Solana)

3. **AuthZ Setup**
   - Implement delegation commands
   - Define permission sets for universal validation
   - Create setup documentation

#### Phase 2: Enhanced Security
1. **Multi-Password System**
   - Separate passwords for different key types
   - Secure password prompting at startup
   - Memory-only password storage

2. **Key Rotation Support**
   - Implement key update mechanisms
   - Support graceful key transitions
   - Maintain operation continuity

3. **Monitoring and Alerts**
   - Key usage tracking
   - Unauthorized access detection
   - Security event logging

### Security Best Practices

1. **Key Isolation**
   - Never store operator keys on validator
   - Use unique passwords for each key type
   - Implement key usage auditing

2. **Permission Minimization**
   - Grant only necessary permissions via AuthZ
   - Regularly review and update permissions
   - Implement permission expiry where possible

3. **Operational Security**
   - Use hardware security modules (HSM) for production
   - Implement key backup and recovery procedures
   - Regular security audits

### Migration Path

For validators transitioning to Universal Validator:

1. **Preparation**
   - Create new hot key for Universal Validator
   - Set up AuthZ delegation from operator account
   - Import chain-specific keys

2. **Testing**
   - Run in testnet first
   - Verify all key operations
   - Test key rotation procedures

3. **Production Deployment**
   - Gradual rollout with monitoring
   - Maintain fallback procedures
   - Document all operational procedures

### Advantages of Proposed Approach

1. **Security**
   - Operator keys remain offline
   - Chain keys are isolated
   - Granular permission control

2. **Flexibility**
   - Easy to add new chains
   - Modular key management
   - Future-proof for TSS

3. **Operational Excellence**
   - Clear security boundaries
   - Standardized procedures
   - Comprehensive auditing

### Considerations and Trade-offs

1. **Complexity**
   - More complex than pchaind approach
   - Requires careful setup and documentation
   - Multiple passwords to manage

2. **Maintenance**
   - Regular permission reviews needed
   - Key rotation procedures required
   - More components to monitor

3. **Benefits Outweigh Complexity**
   - Multi-chain requirements demand sophisticated approach
   - Security benefits justify additional complexity
   - Industry best practices for multi-chain systems

## Validation from Testnet Setup

The testnet setup commands provide real-world validation of our analysis. Here's how the actual deployment process confirms our understanding of pchaind key management:

### Node Initialization Process

The testnet setup demonstrates the standard validator creation workflow:

1. **Infrastructure Setup**
   - Two nodes are created: donut-node1 (genesis) and donut-node2 (validator)
   - Each node gets a dedicated IP address
   - Firewall rules open necessary ports (26656 for P2P, 26657 for RPC, 8545 for EVM RPC)

2. **Software Installation**
   - Standard dependencies including Go, Python, and build tools
   - Python tomlkit for configuration management
   - pchaind binary deployment to ~/app directory

### Key Generation and Management

The setup commands reveal the actual key management flow:

#### Genesis Node (donut-node1)
```bash
~/app/make_first_node.sh
```
This script likely:
- Runs `pchaind init` to generate consensus keys
- Creates the genesis file
- Sets up initial validator if needed

#### Validator Node (donut-node2)

1. **Consensus Key Generation**
   - The resetConfigs.sh script initializes the node
   - This generates `priv_validator_key.json` automatically
   - The consensus public key is retrieved: `pchaind tendermint show-validator`

2. **Operator Key Creation**
   ```bash
   export KEYRING="test"
   export NODE_OWNER_WALLET_NAME=acc21
   pchaind keys add $NODE_OWNER_WALLET_NAME --keyring-backend "$KEYRING"
   ```
   - Creates operator key in test keyring (unencrypted for testnet)
   - Generates wallet address: push1qg8q9xvh7dq7eh6zawkaxszm0uu53yrgunjwdl

3. **Validator Registration**
   The create-validator transaction binds the keys:
   ```bash
   pchaind tx staking create-validator register-validator.json \
     --from $NODE_OWNER_WALLET_NAME \
     --keyring-backend test
   ```

### Key Observations from Setup

1. **Two-Phase Process Confirmed**
   - Phase 1: Node setup generates consensus key automatically
   - Phase 2: Operator manually creates account key and validator

2. **Key Separation Validated**
   - Consensus key: In config directory, used by node
   - Operator key: In keyring, used for transactions

3. **JSON Configuration Structure**
   The register-validator.json shows the binding:
   ```json
   {
     "pubkey": $VALIDATOR_PUBKEY,  // Consensus public key
     "amount": "20000000000000000000000upc",
     "moniker": "donut-node2",
     // ... other validator parameters
   }
   ```

4. **Verification Methods**
   - Transaction hash tracking: 577F36720EE6E3CA6EFB9516B94BF6098C4968461A450650AAC39088F0A71F76
   - Query validator by moniker: `pchaind query staking validators`

### Security Practices in Testnet

Even in testnet, we see security considerations:

1. **Network Isolation**
   - Nodes use private network (cosmos-network)
   - Specific firewall rules for each service

2. **Key Storage**
   - Consensus keys have restricted permissions (chmod 600)
   - Keyring backend set to "test" for development

3. **External Access Control**
   - External IPs for P2P communication
   - Persistent peers configuration for node discovery

### Operational Insights

The setup reveals operational patterns:

1. **Node Identity**
   - Node ID from consensus key: `pchaind tendermint show-node-id`
   - Used for P2P connections between validators

2. **Funding Requirements**
   - Operator account must be funded before creating validator
   - Shows the practical need for operator key to hold tokens

3. **Configuration Management**
   - Python scripts for TOML editing
   - External address configuration for peer discovery
   - State reset procedures for clean initialization

### Validation Summary

The testnet setup commands confirm our analysis:

1. **Key Architecture**: Two-key system (consensus + operator) is standard practice
2. **Binding Process**: Create-validator transaction permanently links the keys
3. **Security Model**: Even in testnet, separation of concerns is maintained
4. **Operational Flow**: Initialize node → Create operator key → Fund account → Create validator

This real-world deployment validates that pchaind follows standard Cosmos SDK patterns for validator key management, exactly as analyzed in the theoretical sections above.

## Zetaclient Command Reference

Understanding Zetaclient's command structure provides insight into how a sophisticated multi-chain key management system operates. Here are the key commands:

### Core Commands

#### Version Command
```bash
zetaclientd version
```
- Displays the current version of Zetaclient
- Useful for debugging and ensuring compatibility

#### Initialize Configuration
```bash
zetaclientd init-config
# or
zetaclientd init
```
- Initializes the Zetaclient configuration file
- Sets up default directory structure
- Creates initial configuration templates

#### Start Observer
```bash
zetaclientd start
```
- Starts the Zetaclient observer
- Prompts for multiple passwords:
  - Hot key password (keyring access)
  - TSS key password (if TSS is enabled)
  - Chain-specific passwords (one per supported chain)
- Loads all keys into memory for operation

### TSS (Threshold Signature Scheme) Commands

#### Encrypt TSS Key-Share
```bash
zetaclientd tss encrypt [file-path] [secret-key]
```
- Encrypts an existing TSS key-share file
- Required arguments:
  - `file-path`: Path to the TSS key-share file
  - `secret-key`: Encryption key to use
- Used to secure TSS key shares after generation

#### Generate TSS Pre-Parameters
```bash
zetaclientd tss gen-pre-params [path]
```
- Generates pre-parameters for TSS key generation
- Required argument:
  - `path`: Directory to store pre-parameters
- Used before distributed key generation ceremony

### Relayer Key Commands

#### Import Relayer Key
```bash
zetaclientd relayer import-key \
  --network=<network-id> \
  --private-key=<private-key> \
  --password=<password> \
  --key-path=<path>
```
- Imports a private key for a specific blockchain
- Options:
  - `--network`: Network ID (e.g., 7 for Solana)
  - `--private-key`: The private key to import
  - `--password`: Encryption password for the key
  - `--key-path`: Path to store encrypted key (default: `~/.zetacored/relayer-keys/`)

#### Show Relayer Address
```bash
zetaclientd relayer show-address \
  --network=<network-id> \
  --password=<password> \
  --key-path=<path>
```
- Displays the address for a relayer key
- Options:
  - `--network`: Network ID to show address for
  - `--password`: Password to decrypt the key
  - `--key-path`: Path to relayer keys directory

### Global Options

All commands support:
```bash
--home <path>  # Set home directory (default: ~/.zetacored)
```

### Key Management Workflow

1. **Initial Setup**
   ```bash
   # Initialize configuration
   zetaclientd init-config
   
   # Import relayer keys for each chain
   zetaclientd relayer import-key --network=1 --private-key=<eth-key> --password=<pass>
   zetaclientd relayer import-key --network=7 --private-key=<sol-key> --password=<pass>
   ```

2. **TSS Setup (if required)**
   ```bash
   # Generate pre-parameters
   zetaclientd tss gen-pre-params /path/to/preps
   
   # After key generation, encrypt the share
   zetaclientd tss encrypt /path/to/share <encryption-key>
   ```

3. **Daily Operations**
   ```bash
   # Start the observer
   zetaclientd start
   # Enter passwords when prompted
   ```

### Security Features

1. **Password Prompting**
   - Passwords are never passed as command-line arguments for `start`
   - Interactive prompting prevents password exposure in shell history

2. **Encrypted Storage**
   - All keys are encrypted at rest
   - Different encryption keys for different chains

3. **Memory-Only Passwords**
   - Passwords are kept in memory during runtime
   - Not written to disk or logs

### Command Design Principles

Zetaclient's commands follow several important patterns:

1. **Separation of Concerns**
   - TSS commands handle distributed signing
   - Relayer commands manage chain-specific keys
   - Core commands control the observer lifecycle

2. **Security by Default**
   - Mandatory encryption for all keys
   - Interactive password prompting
   - No plaintext key storage

3. **Multi-Chain Architecture**
   - Network IDs identify different blockchains
   - Separate key management per chain
   - Modular addition of new chains

## Universal Validator Commands

The Universal Validator (`puniversald`) is currently implemented as a client daemon that monitors multiple chains and manages registry configurations. Based on the existing implementation and the analysis of key management needs, here's the current command structure along with proposed enhancements for key management:

### Current Commands (Implemented)

#### Version
```bash
puniversald version
```
- Displays version information including:
  - Name, App Name, Version
  - Commit hash
  - Build tags

#### Initialize Configuration
```bash
puniversald init [flags]
```
- Creates initial configuration file
- Saves to `~/.pushuv/config/pushuv_config.json`
- Current flags:
  - `--log-level`: Log level (0=debug, 1=info, ..., 5=panic)
  - `--log-format`: Log format (json or console)
  - `--log-sampler`: Enable log sampling

#### Start
```bash
puniversald start
```
- Starts the Universal Validator client
- Loads configuration from `~/.pushuv/config/pushuv_config.json`
- Initializes logger, database, and registry client
- Begins monitoring configured chains

#### Query Commands
```bash
puniversald query uregistry <subcommand>
```
Subcommands for querying registry data:
- `all-chain-configs`: Query all chain configurations
- `all-token-configs`: Query all token configurations
- `token-configs-by-chain --chain <chain-id>`: Query tokens for specific chain
- `token-config --chain <chain-id> --address <token-address>`: Query specific token

All query commands support `--output` flag (yaml|json)

### Proposed Key Management Commands

Based on the Zetaclient architecture analysis and Universal Validator's multi-chain monitoring role, the following key management commands should be implemented:

#### Hot Key Management
```bash
# Create new hot key
puniversald keys add <key-name> [flags]
  --keyring-backend {test|file|os}  # Keyring backend (default: file)
  --recover                          # Recover from mnemonic

# List all keys
puniversald keys list

# Show key details
puniversald keys show <key-name> [flags]
  --address    # Show address only
  --pubkey     # Show public key

# Delete key
puniversald keys delete <key-name>

# Export key
puniversald keys export <key-name>

# Import key
puniversald keys import <key-name> <keyfile>
```

#### Chain Key Management
```bash
# Import chain-specific key
puniversald chain-keys import [flags]
  --chain {eip155:*|solana:*}       # Chain ID in CAIP-2 format
  --private-key <key>                # Private key to import
  --key-file <path>                  # Or import from file
  --password                         # Encryption password (prompted if not provided)

# Show chain address
puniversald chain-keys show [flags]
  --chain {eip155:*|solana:*}       # Chain ID in CAIP-2 format
  --password                         # Decryption password (prompted)

# List all chain keys
puniversald chain-keys list

# Remove chain key
puniversald chain-keys delete [flags]
  --chain {eip155:*|solana:*}       # Chain ID in CAIP-2 format
  --password                         # Confirmation password

# Rotate chain key
puniversald chain-keys rotate [flags]
  --chain {eip155:*|solana:*}       # Chain ID in CAIP-2 format
  --new-key <key>                    # New private key
  --password                         # Encryption password
```

### AuthZ Delegation Commands

#### Setup Delegation
```bash
# Create delegation from operator to hot key
puniversald authz grant <hot-key-address> [flags]
  --from <operator-key-name>         # Operator key name
  --permissions <permission-file>    # JSON file with permissions
  --expiration <duration>            # Optional expiration (e.g., "365d")
  --chain-id <chain-id>              # Target chain ID
  --node <rpc-url>                   # RPC endpoint

# Example permission file (permissions.json):
{
  "msg_types": [
    "/push.utv.v1.MsgVerifyTransaction",
    "/push.uregistry.v1.MsgUpdateRegistry",
    "/push.observer.v1.MsgSubmitObservation"
  ],
  "max_tokens": null,
  "allow_list": []
}
```

#### Manage Delegations
```bash
# List active delegations
puniversald authz list [flags]
  --granter <address>    # Filter by granter
  --grantee <address>    # Filter by grantee

# Revoke delegation
puniversald authz revoke <grantee-address> <msg-type> [flags]
  --from <operator-key-name>
  --chain-id <chain-id>
  --node <rpc-url>

# Update delegation
puniversald authz update <grantee-address> [flags]
  --from <operator-key-name>
  --permissions <permission-file>
  --expiration <duration>
```

### Security Commands

#### Key Encryption
```bash
# Re-encrypt chain keys with new password
puniversald security reencrypt [flags]
  --chain {eip155:*|solana:*|all}       # Target chain(s)
  --old-password                         # Current password
  --new-password                         # New password

# Verify key integrity
puniversald security verify [flags]
  --chain {eip155:*|solana:*|all}       # Target chain(s)
  --password                             # Decryption password

# Backup keys
puniversald security backup [flags]
  --output <backup-dir>                  # Backup directory
  --include-hot-key                      # Include hot key in backup
  --include-chain-keys                   # Include chain keys
  --encrypt                              # Encrypt backup
```

### Migration Commands

#### From Zetaclient
```bash
# Import Zetaclient configuration
puniversald migrate from-zetaclient [flags]
  --zetaclient-home <path>              # Zetaclient home directory
  --import-keys                          # Import relayer keys
  --import-config                        # Import configuration

# Validate migration
puniversald migrate validate [flags]
  --check-keys                           # Verify all keys are accessible
  --check-authz                          # Verify AuthZ setup
```

### Operational Commands

#### Status and Monitoring
```bash
# Show validator status (proposed)
puniversald status

# Show key status (proposed)
puniversald keys status [flags]
  --verbose                              # Show detailed information

# Test chain connectivity (proposed)
puniversald chain test [flags]
  --chain {eip155:*|solana:*|all}       # Target chain(s)
```

### Configuration Management

#### Enhanced Config Commands
```bash
# Show current configuration (extends existing)
puniversald config show

# Validate configuration (proposed)
puniversald config validate

# Update configuration value (proposed)
puniversald config set <key> <value>

# Add chain to monitoring (proposed)
puniversald config add-chain [flags]
  --chain <chain-id>                     # Chain ID in CAIP-2 format
  --rpc-url <url>                        # RPC endpoint for chain
  --enabled                              # Enable immediately
```

### Key Storage Architecture

Based on the current implementation and future needs:

```
~/.pushuv/
├── config/
│   └── pushuv_config.json         # Main configuration
├── keys/
│   ├── keyring/                   # Cosmos SDK keyring for hot keys
│   └── chain-keys/                # Encrypted chain-specific keys
│       ├── eip155:1.key          # Ethereum mainnet key
│       ├── eip155:11155111.key   # Sepolia testnet key
│       └── solana:mainnet.key    # Solana mainnet key
└── data/
    └── pushuv.db                  # Local database
```

### Example Workflows

#### Initial Setup
```bash
# 1. Initialize Universal Validator
puniversald init --log-level 1 --log-format json

# 2. Create hot key (proposed)
puniversald keys add universal-hot-key --keyring-backend file

# 3. Import chain keys (proposed)
puniversald chain-keys import --chain eip155:1 --key-file eth-mainnet.json
puniversald chain-keys import --chain eip155:11155111 --key-file eth-sepolia.json
puniversald chain-keys import --chain solana:mainnet --key-file sol-mainnet.json

# 4. Setup AuthZ delegation (proposed, from operator account)
puniversald authz grant <hot-key-address> \
  --from operator-key \
  --permissions permissions.json \
  --chain-id push_42101-1

# 5. Start validator
puniversald start
```

#### Monitoring and Queries
```bash
# Check chain configurations
puniversald query uregistry all-chain-configs --output json

# Check token configurations for a specific chain
puniversald query uregistry token-configs-by-chain --chain eip155:11155111

# Query specific token
puniversald query uregistry token-config \
  --chain eip155:11155111 \
  --address 0x1234567890123456789012345678901234567890
```

#### Key Rotation (Proposed)
```bash
# 1. Import new key
puniversald chain-keys import --chain eip155:1 --private-key <new-key>

# 2. Verify new key
puniversald chain-keys show --chain eip155:1

# 3. Remove old key (after confirming new key works)
puniversald chain-keys delete --chain eip155:1 --password
```

#### Security Audit (Proposed)
```bash
# 1. Verify all keys
puniversald security verify --chain all

# 2. Check AuthZ delegations
puniversald authz list --granter <operator-address>

# 3. Backup keys
puniversald security backup --output ./backup --encrypt

# 4. Test chain connectivity
puniversald chain test --chain all
```

### Implementation Priorities

1. **Phase 1: Key Management Foundation**
   - Implement basic keyring integration for hot keys
   - Add chain key import/export functionality
   - Create encrypted storage for chain keys
   - Integrate key loading into start command

2. **Phase 2: AuthZ Integration**
   - Add AuthZ delegation commands
   - Implement permission-based operations
   - Create hot key authentication flow
   - Add delegation validation

3. **Phase 3: Security Enhancements**
   - Multi-password system for different key types
   - Key rotation workflows
   - Backup and recovery commands
   - Security audit tools

4. **Phase 4: Advanced Features**
   - Migration tools from other validators
   - Performance monitoring integration
   - Automated key rotation
   - Multi-validator coordination support

### Key Architecture and Transaction Capabilities

Understanding which keys can perform transactions on which chains is fundamental to Universal Validator's security model. The system maintains complete isolation between P-chain operations and external chain operations.

#### Key Types and Their Transaction Scope

##### 1. Keyring Keys (Cosmos SDK Keys)
These keys operate **exclusively on the P-chain** (Push Chain):

```
┌─────────────────────────────────────┐
│     Keyring Keys (P-chain only)     │
├─────────────────────────────────────┤
│ • Stored in Cosmos SDK keyring      │
│ • Format: Secp256k1                 │
│ • Address: push1xxxxx...           │
│                                     │
│ Can do on P-chain:                  │
│ ✓ Send PUSH tokens                  │
│ ✓ Stake/delegate                    │
│ ✓ Vote on governance                │
│ ✓ Create validators                 │
│ ✓ Submit observations               │
│ ✓ Update registry                   │
│                                     │
│ Cannot do:                          │
│ ✗ Send ETH on Ethereum              │
│ ✗ Send SOL on Solana                │
│ ✗ Interact with other chains        │
└─────────────────────────────────────┘
```

##### 2. Chain Keys (External Chain Keys)
These keys operate **exclusively on their respective external chains**:

```
┌─────────────────────────────────────┐
│    Chain Keys (External chains)     │
├─────────────────────────────────────┤
│ EVM Key (eip155:1)                  │
│ • Format: Secp256k1                 │
│ • Address: 0x1234...                │
│ • Can: Send ETH, call contracts     │
│                                     │
│ Solana Key (solana:mainnet)         │
│ • Format: Ed25519                   │
│ • Address: 9WzDXwBbmkg...           │
│ • Can: Send SOL, call programs      │
│                                     │
│ Each key works ONLY on its chain!   │
└─────────────────────────────────────┘
```

#### How Keys Work Together

The Universal Validator orchestrates these isolated keys to perform cross-chain operations:

```
┌──────────────────────┐     ┌──────────────────────┐
│   P-chain (Push)     │     │   External Chains    │
├──────────────────────┤     ├──────────────────────┤
│                      │     │                      │
│  Operator Key (Cold) │     │  Ethereum Key        │
│  push1abc...         │     │  0x123...            │
│      ↓               │     │                      │
│  Delegates via AuthZ │     │  Solana Key          │
│      ↓               │     │  9WzD...             │
│  Hot Key (Online)    │     │                      │
│  push1xyz...         │     │  Bitcoin Key         │
│                      │     │  bc1q...             │
└──────────────────────┘     └──────────────────────┘
         │                            │
         │                            │
         ↓                            ↓
   Can submit proofs            Can read/monitor
   about external chains        and sign transactions
   to P-chain                   on respective chains
```

#### Cross-Chain Operation Workflow

Here's how Universal Validator uses different keys for a complete cross-chain operation:

```bash
# 1. Ethereum key observes an event on Ethereum
#    Uses: 0x123... key to read Ethereum state

# 2. Hot key submits observation to P-chain
puniversald tx observer submit-observation \
  --from hot-key \              # Uses push1xyz... key
  --chain eip155:1 \
  --event "Transfer(0xabc, 0xdef, 1000)"

# 3. After P-chain consensus, execute on destination chain
#    If destination is Solana: Uses 9WzD... key
#    If destination is BSC: Uses 0x456... key
```

#### Key Isolation in Practice

The implementation ensures complete key isolation:

```python
# Simplified internal implementation
class UniversalValidator:
    def __init__(self):
        # P-chain key from keyring
        self.hot_key = load_from_keyring("hot-key")  # push1xyz...
        
        # Chain-specific keys from encrypted storage
        self.chain_keys = {
            "eip155:1": load_encrypted("~/.pushuv/keys/eip155:1.key"),      # 0x123...
            "eip155:11155111": load_encrypted("~/.pushuv/keys/eip155:11155111.key"),  # 0x456...
            "solana:mainnet": load_encrypted("~/.pushuv/keys/solana:mainnet.key"),    # 9WzD...
        }
    
    def submit_to_pchain(self, observation):
        # ONLY hot_key can sign P-chain transactions
        tx = create_observation_tx(observation)
        signed_tx = self.hot_key.sign(tx)
        return broadcast_to_pchain(signed_tx)
    
    def execute_on_ethereum(self, action):
        # ONLY Ethereum key can sign Ethereum transactions
        eth_key = self.chain_keys["eip155:1"]
        tx = create_eth_transaction(action)
        signed_tx = eth_key.sign(tx)
        return broadcast_to_ethereum(signed_tx)
```

#### Security Benefits of Key Isolation

This architecture provides multiple security layers:

1. **Compromised P-chain key**
   - Cannot steal ETH, SOL, or other external assets
   - Cannot execute unauthorized transactions on external chains
   - Damage limited to P-chain operations only

2. **Compromised Ethereum key**
   - Cannot affect P-chain stake or governance
   - Cannot impact other chains (Solana, BSC, etc.)
   - Damage limited to Ethereum operations only

3. **Operator key remains cold**
   - Never exposed to online systems
   - Maintains ultimate control via AuthZ
   - Can revoke permissions if hot key compromised

#### Practical Key Management Commands

Based on the proposed architecture:

```bash
# Query operations (no key needed)
puniversald query uregistry all-chain-configs

# P-chain key management
puniversald keys add hot-key --keyring-backend file
# Creates: push1abc... (works only on P-chain)

# External chain key management
puniversald chain-keys import --chain eip155:1 --private-key <eth-key>
# Creates: 0x123... (works only on Ethereum)

puniversald chain-keys import --chain solana:mainnet --private-key <sol-key>
# Creates: 9WzD... (works only on Solana)

# View key addresses
puniversald keys show hot-key                    # Shows: push1abc...
puniversald chain-keys show --chain eip155:1    # Shows: 0x123...
```

#### Key Storage Structure

The physical separation of keys on disk reinforces the logical separation:

```
~/.pushuv/
├── keys/
│   ├── keyring/                   # Cosmos SDK keyring
│   │   └── hot-key               # P-chain key (push1...)
│   └── chain-keys/               # External chain keys
│       ├── eip155:1.key         # Ethereum mainnet (0x...)
│       ├── eip155:56.key        # BSC mainnet (0x...)
│       └── solana:mainnet.key   # Solana mainnet (base58...)
```

This complete isolation ensures that:
- **No single key compromise can affect multiple chains**
- **Each chain's security model is respected**
- **Cross-chain operations require explicit coordination**
- **Audit trails clearly show which key performed which action**

### Integration with Existing Architecture

The proposed key management system integrates seamlessly with the current Universal Validator architecture:

1. **Registry Client**: Uses hot key for authenticated queries
2. **Chain Clients**: Each uses its specific encrypted key
3. **Config System**: Extended to include key paths and security settings
4. **Logger**: Enhanced to audit key operations
5. **Database**: Stores key metadata and usage history

This approach maintains backward compatibility while adding the sophisticated key management capabilities needed for secure multi-chain operations.

## Conclusion

Universal Validator should adopt a Zetaclient-style key management architecture due to its similar role as a multi-chain observer. This approach provides:

- Strong security through delegation and isolation
- Flexibility for multi-chain operations
- Future-proofing for advanced features like TSS
- Alignment with industry best practices

The implementation should be phased, starting with basic functionality and progressively adding advanced features. This ensures a stable foundation while maintaining the flexibility to evolve with changing requirements.