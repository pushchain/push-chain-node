# Changelog

All notable changes to this project will be documented in this file.

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