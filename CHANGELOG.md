# Changelog

All notable changes to this project will be documented in this file.

## [v0.0.2] - 2025-09-02

## What's Changed

### 🚀 Features
- add manual binary release workflow with auto-changelog
- updated default admin of uexecutor module
- updated utxhashverifier precompile addr from 0x901 to 0xCB
- renamed ocv precompile to utxhashverifier
- renamed usv precompile to usigverifier
- modified utv module name to utxverifier module
- renamed ue module to uexecutor module
- updated module name from rollchains to pushchain/push-chain-node
- updated factory implementation bytecode
- added upgrade handler for gas estimation upgrade
- added gasLimit override feature in derived tx
- added generated mocks for evm keeper
- added upgrade handler for gas override feature
- added gas units in the gasless tx logs for showing derived tx in explorer
- updated verifiedTxs utv storage format
- added verifiedTxMetadata and usdValue in utv proto
- added upgrade handler for evm-derived-tx upgrade
- made verification svm working for solana txHash in bytes hex
- updated protobuf for msgExecutePayload from signature to payloadVerifier
- added a derived evm tx for mint pc msg
- imported account keeper in ue module
- modified evm calls to derived evm calls
- switched evm from strangelove/os to pushchain/evm fork
- modified UniversalAccount to UniversalAccountId in proto
- updated keeper and msg server methods acc. to new structures
- updated universalAccount and universalPayload protobuf
- Added enum for method type (removing method name)
- updated proxyAdmin owner address
- setup default admin for testnet-donut
- deployed new gateway on sepolia
- modified the method_config protobuf
- deployed new gateway on sepolia
- updated the factory impl bytecode
- finalised the verifyEd25519 gas to 4000
- modified npush to upc
- added proxyAdmin, and made factory a upgradeable transparent proxy
- implemented fallback rpc call to public rpc
- added chain config json for sepolia, solana devnet
- added generated protobuf files
- added proto changes for uea and universal account, payload
- added checks for called input fn is addFunds or not
- adjusted the method_config message validation
- standardised usd exponent/decimals and push conversion rate
- chain config scales for multiple methods
- modified chainConfig proto
- removed admin-params as factory is constant now
- added enabled param in chain-config for enabling/disabling a chain
- setup default admin
- adjust svm usdt decimals
- deployed factory contract on 0xEA
- added env support for rpc urls
- verifySVMInteraction works
- added parsing of funds from fundsAdded param
- added verification in msgDeploy and msgMint
- imported UtvKeeper in ue module
- standardized verify tx keeper methods
- added rpc package and evm rpc call functions
- imported ueKeeper in utvKeeper
- added queryVerifiedTxRequest in proto
- added updateChainConfig msg implementation
- added generate protobuf
- added UpdateChainConfig msg in proto file
- refactored file structure of proto files of ue module
- added validations in chain_config
- added cli commands for chain config methods
- added implementation of QueryChainConfig msg
- added implementation of AddChainConfig msg
- generated protobuf files
- added add-chain-config msg and query in proto files
- scaffolded utv module
- renamed solverfier precompile to usv precompile
- renamed crosschain module to universal-executor module
- removed verifier module on solverifier precompile
- added new account generation and verification decorator for gasless transactions
- added comments
- added gascost calculation in msgExecutePayload
- imported feemarket keeper in crosschain module
- added a base fee of 1 npush
- made crosschain module messages gasless
- modified verifyEd25519 precompile to a stateless precompile
- updated ed25519 precompile from 0x902 addr to 0x901
- added implementation of MsgExecutePayload
- completed the msgMintPush implementation
- imported bank keeper in crosschain module
- added MsgMintPush msg implementation
- added MsgDeployNMSC implementation and cli support
- added evm keeper inside the crosschain module
- added cli commands for admin_params msg
- added types for admin_params update msg
- added handler methods for admin-params msg and query msg
- updated proto files to add a admin-params msg
- switched from orm based state store to basic lightweight kvStore
- modified verifyEd25519 precompile to accept msg argument as bytes32
- added query messages for factory and precompile
- added setFactory and setVerifierPrecompile messages
- added admin of crosschain module in params
- generated protobuf files
- scaffolded crosschain module
- added solana sig verification precompile
- add end-to-end tests for block time, gas fees, governance proposals, and token transfers
- added ante handlers changes
- added changes of rpc, server
- added evm changes in app.go
- scaffolded chain using ignite v 0.27.2
- added push prefix and denom (#2)

### 🐛 Bug Fixes
- resolve GitHub Actions workflow warnings
- update .goreleaser.yaml repository configuration
- load .env on start up
- nginx script
- expose ports on nginx
- revert max_gas for block on testnet
- precompies
- renamed utv to utxverifier at some places
- fixed issue of computing uea address instead of gettig from factory
- update evm params, fix block params
- nginx setup
- scripts
- fixed broken link in readme
- remove identical mintPc test
- fixed MsgExecutePayload tests
- switched evm keeper with mock evm keeper
- readme
- readme
- enable make install in testnet script
- ignore build
- added the old upgradeHandlers in app.go
- remove comments
- fix one click upgrade handler
- fixed issue of verification
- fixed solana tx precompile
- fixed solana normalized tx hash
- sdk context & standardized code
- code after merge
- updated go version in dockerFile
- flattened args & bytes instead of string
- added an empty txLog for maintaining the logIndex
- fixed evm tx types in ante
- remove hardcoded function name
- Program ID verification
- fixed gasLimit comparison issue to gasCost
- modified solana getTransaction with fetching confirmed blocks param
- fixed deployFactory layout
- function name
- merge conflict
- names eth to EVM
- names
- function names
- event parsing
- fixed push tokens mint amount decimals

### ♻️ Refactoring
- added integration tests in uexecutor module directory and modified keeper and types imports
- removed integration tests from ue module directory
- updated a comment
- removed old migration of utxverifier module
- removed all previous upgrade handlers
- added state migration in utv module for verifiedInboundTx collection
- updated ocv precompile acc to new modularised verification methods
- updated utv types
- modularised svm verification methods
- modularised evm verification methods
- added svm helpers for verification
- added evm helpers for verification
- updated payload hash verification
- updated funds verification
- updated gateway interaction verification
- updated query server of utv module
- added protobuf
- updated msgExecutePayload param from payload_verifier to verification_data
- storing the bytes txHash in storage
- updated ue module methods acc. to new msgExecutePayload proto changes
- added protobuf generated
- updated block confirmations to 0
- updated abi and factory impl bytecode
- updated factory contract implementation bytecode
- moved mocks package out of keeper to resolve proto-gen issue
- exported keeper methods
- updated chainId to chain
- exported keeper methods
- updated baseFee
- renamed MsgMintPush to MsgMintPC
- removed maxPriorityFeePerGas from consideration
- updated sig_type in proto
- Updated lockerInteraction to gatewayInteraction
- modified locker_contract terminology to gateway_address
- updated .env sample with instructions for private RPC URLs
- updated new rpc function calls in verification
- modified evm, svm rpc functions
- added protobuf changes
- added validation changes
- added nomenclature changes in msg_server and query_server
- added nomenclature changes in msg_mint_push
- added nomenclature changes in msg_execute_payload
- renamed msg_deploy_nmsc to uea
- added chain_id to chain changes in chain_config
- added nomenclature changes in evm,abi operations
- added chainEnabled keeper
- added mock evmKeeper for unit test
- added comments
- modularise the msg server handler of ue module
- added verifyTx keeper fn in utv module
- added verifiedTx collection map and keys
- added queryRequest impl
- added generated protobuf
- added protobuf generated files of chain config msg
- added keys of chain config params
- removed legacy tx and query cli methods
- updated usv precompile addr from 0x901 to 0xca
- fixed Validate fn to ValidateBasic
- fixed Validate fn to ValidateBasic
- removed verifierPrecompile from adminParams
- replaced caipString with accountId in msgExecutePayload
- replaced caipString with accountId in msgMintPush
- modified validations of crosschain_payload
- added validations for accountId
- updated the deployNMSC msg implementation
- added accountId param in MsgDeployNMSC
- added param validation
- added config in types_test package
- added parameter validations in crosschain module
- removed temp variable from crosschain params
- added txHash param in MsgDeployNMSC
- modularize deployNmsc and mintPush message handlers
- modularise execute payload msg handler
- updating dummy mint amount
- updated MsgExecutePayload implementation
- added BaseDenom in types global package
- changed types in crosschain payload
- generated protobuf files for proto files
- enhance interchaintest with detailed comments and structured test cases for basic chain, CosmWasm integration, and token factory functionality
- added consensus params in config.yml
- replaced server commands to evm server commands
- added cmd changes (not working rn)
- added chainId and increased bonded amount acc. to evm
- removed scaffolded chain with ignite v28.7.0
- added a extra account in config mainly for funding the faucet
- modified binary name to pushchaind due to collision with pre-defined command in bash

### 📦 Other Changes
- Update manual-release.yml
- Merge branch 'main' of https://github.com/pushchain/push-chain-node
- Update CHANGELOG.md for 0.0.1
- Merge pull request #59 from pushchain/github_actions
- Merge pull request #52 from pushchain/start_up_env_load
- Merge pull request #50 from pushchain/expose-ports-on-nginx
- Merge pull request #46 from pushchain/arya/integration-test
- minor adjustments
- accomodated main changes to integration-test
- merged main changes
- Merge pull request #41 from pushchain/testnet-deployment-setup
- Merge pull request #47 from pushchain/feat/new-testnet-changes
- Refactor: testutils
- relax limits + change code to 429
- finalized integration tests
- removed unused functions
- Finalized Integration-tests
- Finalized DeployUEA tests
- Finalized MintPC tests
- Merge pull request #44 from pushchain/change-chain-registry

### 👥 Contributors
- @Aman Gupta
- @Arya Lanjewar
- @Ashis
- @Ashis Kumar Pradhan
- @Developer Experience team at Ignite
- @GitHub Action
- @Igx22
- @Md Zartaj Afser
- @Mohammed S
- @Nilesh Gupta
- @Zartaj0
- @aman035
- @pranshurastogi
- @strykerin

---

## [0.0.1] - 2025-09-02

## What's Changed

### 🚀 Features
- add manual binary release workflow with auto-changelog
- updated default admin of uexecutor module
- updated utxhashverifier precompile addr from 0x901 to 0xCB
- renamed ocv precompile to utxhashverifier
- renamed usv precompile to usigverifier
- modified utv module name to utxverifier module
- renamed ue module to uexecutor module
- updated module name from rollchains to pushchain/push-chain-node
- updated factory implementation bytecode
- added upgrade handler for gas estimation upgrade
- added gasLimit override feature in derived tx
- added generated mocks for evm keeper
- added upgrade handler for gas override feature
- added gas units in the gasless tx logs for showing derived tx in explorer
- updated verifiedTxs utv storage format
- added verifiedTxMetadata and usdValue in utv proto
- added upgrade handler for evm-derived-tx upgrade
- made verification svm working for solana txHash in bytes hex
- updated protobuf for msgExecutePayload from signature to payloadVerifier

### 🐛 Bug Fixes
- update .goreleaser.yaml repository configuration
- load .env on start up
- nginx script
- expose ports on nginx
- revert max_gas for block on testnet
- precompies
- renamed utv to utxverifier at some places
- fixed issue of computing uea address instead of gettig from factory
- update evm params, fix block params
- nginx setup
- scripts
- fixed broken link in readme
- remove identical mintPc test
- fixed MsgExecutePayload tests
- switched evm keeper with mock evm keeper
- readme
- readme
- enable make install in testnet script
- ignore build
- added the old upgradeHandlers in app.go
- remove comments
- fix one click upgrade handler
- fixed issue of verification
- fixed solana tx precompile
- fixed solana normalized tx hash
- sdk context & standardized code
- code after merge
- flattened args & bytes instead of string

### ♻️ Refactoring
- added integration tests in uexecutor module directory and modified keeper and types imports
- removed integration tests from ue module directory
- updated a comment
- removed old migration of utxverifier module
- removed all previous upgrade handlers
- added state migration in utv module for verifiedInboundTx collection
- updated ocv precompile acc to new modularised verification methods
- updated utv types
- modularised svm verification methods
- modularised evm verification methods
- added svm helpers for verification
- added evm helpers for verification
- updated payload hash verification
- updated funds verification
- updated gateway interaction verification
- updated query server of utv module
- added protobuf
- updated msgExecutePayload param from payload_verifier to verification_data
- storing the bytes txHash in storage
- updated ue module methods acc. to new msgExecutePayload proto changes
- added protobuf generated

### 📦 Other Changes
- Merge pull request #59 from pushchain/github_actions
- Merge pull request #52 from pushchain/start_up_env_load
- Merge pull request #50 from pushchain/expose-ports-on-nginx
- Merge pull request #46 from pushchain/arya/integration-test
- minor adjustments
- accomodated main changes to integration-test
- merged main changes
- Merge pull request #41 from pushchain/testnet-deployment-setup
- Merge pull request #47 from pushchain/feat/new-testnet-changes
- Refactor: testutils
- relax limits + change code to 429
- finalized integration tests
- removed unused functions
- Finalized Integration-tests
- Finalized DeployUEA tests
- Finalized MintPC tests
- Merge pull request #44 from pushchain/change-chain-registry
- Update chain-registry
- finalized integration-test for ue module
- integration-test setup finalized

### 👥 Contributors
- @Aman Gupta
- @Arya Lanjewar
- @Mohammed S
- @Nilesh Gupta
- @Zartaj0
- @aman035
- @pranshurastogi
- @strykerin

---

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Manual GitHub Actions workflow for binary releases
- Support for custom changelog entries per release
- Linux AMD64 binary builds for core validator

### Changed
- N/A

### Fixed
- N/A

---

<!-- Previous releases will be added here automatically by the workflow -->