# e2e-tests setup

This folder provides a full, automated local E2E bootstrap for Push Chain.

It covers:

1. Local devnet — 4 `pchaind` + 4 `puniversald` processes (no Docker)
2. TSS key generation
3. Genesis key recovery + account funding
4. Core contracts deployment (auto-resume on receipt errors)
5. Swap AMM deployment (WPC + Uniswap V3 core + periphery)
6. WPC liquidity pool creation for all synthetic tokens
7. Core `.env` generation from deployed addresses
8. Token config updates
9. Gateway contracts deployment (auto-resume on receipt errors)
10. `configureUniversalCore` script
11. uregistry chain/token config submission
12. CounterPayable deployment + SDK constant sync
13. `push-chain-sdk` E2E test runners

---

## Quick testing setup

Three commands from a clean checkout. Make sure the prerequisites below are installed first.

**1. Set up `.env`**

```bash
cp e2e-tests/.env.example e2e-tests/.env
```

Edit `e2e-tests/.env` and set at minimum:

- `TESTING_ENV=LOCAL` — enables anvil + surfpool forks
- `PRIVATE_KEY=0x...` — EVM deployer key (used by forge/hardhat and mirrored into SDK `.env`)
- `SOLANA_PRIVATE_KEY=...` — only needed if you plan to run Solana SDK tests

`FUND_TO_ADDRESS`, `EVM_PRIVATE_KEY`, `EVM_RPC`, and `PUSH_PRIVATE_KEY` are auto-derived from `PRIVATE_KEY` / `PUSH_RPC_URL` if left blank.

**2. Bootstrap the local Push network**

```bash
TESTING_ENV=LOCAL bash e2e-tests/setup.sh all
```

Runs the full pipeline: starts anvil/surfpool forks, boots 4 validators + 2 universal validators, generates the TSS key, deploys core/swap/gateway contracts, submits uregistry configs, and syncs addresses into `deploy_addresses.json`. See [One-command full run](#one-command-full-run) for the detailed step list.

**3. Set up the SDK**

```bash
TESTING_ENV=LOCAL bash e2e-tests/setup.sh setup-sdk
```

Clones `push-chain-sdk`, writes `packages/core/.env` from your e2e `.env`, syncs the LOCALNET synthetic token addresses into the SDK's chain constants, resolves `UEA_PROXY_IMPLEMENTATION` from the local chain, and installs dependencies.

After this you can run SDK E2E tests — see [Running SDK E2E tests](#running-sdk-e2e-tests).

---

## What gets created

- `local-native/data/` — validator + universal-validator home directories
- `local-native/logs/` — per-process log files
- `e2e-tests/logs/` — logs for each deployment step
- `e2e-tests/deploy_addresses.json` — contract/token address source-of-truth

External repos are resolved from **sibling directories** (relative to `push-chain/`):

| Repo | Default path |
|---|---|
| `push-chain-core-contracts` | `../push-chain-core-contracts` |
| `push-chain-swap-internal-amm-contracts` | `../push-chain-swap-internal-amm-contracts` |
| `push-chain-gateway-contracts` | `../push-chain-gateway-contracts` |
| `push-chain-sdk` | `../push-chain-sdk` |

Override any of these with env vars (`CORE_CONTRACTS_DIR`, `SWAP_AMM_DIR`, `GATEWAY_DIR`, `PUSH_CHAIN_SDK_DIR`).

---

## Prerequisites

Required tools:

- `git`, `make`, `curl`, `jq`, `perl`, `python3`, `lsof`
- `node`, `npm`, `npx`, `yarn`
- `forge`, `cast` (Foundry)
- `anvil` + `surfpool` — only for `TESTING_ENV=LOCAL`
- `pchaind` and `puniversald` binaries in `build/` (built by `make build`)

Build the binaries first:

```bash
make replace-addresses
make build
```

---

## Configuration

Copy env template:

```bash
cp e2e-tests/.env.example e2e-tests/.env
```

Edit `e2e-tests/.env`. Key variables:

| Variable | Default | Description |
|---|---|---|
| `TESTING_ENV` | _(empty)_ | Set to `LOCAL` for local anvil/surfpool mode |
| `PUSH_RPC_URL` | `http://localhost:8545` | Push Chain EVM JSON-RPC |
| `PRIVATE_KEY` | — | EVM deployer private key (forge/hardhat) |
| `EVM_PRIVATE_KEY` | ← `PRIVATE_KEY` | SDK EVM signer key |
| `EVM_RPC` | ← `PUSH_RPC_URL` | SDK EVM RPC endpoint |
| `PUSH_PRIVATE_KEY` | ← `PRIVATE_KEY` | SDK Push Chain signer key |
| `SOLANA_PRIVATE_KEY` | — | SDK Solana signer key (also `SVM_PRIVATE_KEY` / `SOL_PRIVATE_KEY`) |
| `SOLANA_RPC_URL` | `https://api.devnet.solana.com` | SDK Solana RPC |
| `FUND_TO_ADDRESS` | _(auto-derived from `PRIVATE_KEY`)_ | Address to top up from genesis account |
| `GENESIS_MNEMONIC` | _(read from `genesis_accounts.json`)_ | Override genesis mnemonic directly |
| `POOL_CREATION_TOPUP_AMOUNT` | `50000000000000000000upc` | Deployer top-up before pool creation |
| `LOCAL_DEVNET_DIR` | `./local-native` | Path to local devnet management directory |
| `CORE_CONTRACTS_BRANCH` | `e2e-push-node` | |
| `SWAP_AMM_BRANCH` | `e2e-push-node` | |
| `GATEWAY_BRANCH` | `e2e-push-node` | |
| `PUSH_CHAIN_SDK_BRANCH` | `outbound_changes` | |
| `PUSH_CHAIN_SDK_E2E_DIR` | `packages/core/__e2e__/evm/inbound` | Test directory inside SDK |
| `PREFER_SIBLING_REPO_DIRS` | `true` | Prefer sibling dirs for core/gateway repos over cloning fresh |
| `E2E_TARGET_CHAINS` | — | Restrict SDK E2E chains (passed through to SDK `.env`) |
| `CORE_RESUME_MAX_ATTEMPTS` | `0` (unlimited) | Max `--resume` retry count for core forge script |
| `GATEWAY_RESUME_MAX_ATTEMPTS` | `0` (unlimited) | Max `--resume` retry count for gateway forge script |
| `CORE_CONFIGURE_RESUME_MAX_ATTEMPTS` | `0` (unlimited) | Max `--resume` retry count for `configureUniversalCore` |

### TESTING_ENV=LOCAL

When set in `.env`, the `setup-environment` step (also called by `all`) does:

1. Starts local fork nodes:
   - `anvil` for Ethereum Sepolia, Arbitrum Sepolia, Base Sepolia, BSC Testnet
   - `surfpool` for Solana
2. Rewrites `public_rpc_url` in `config/testnet-donut/*/chain.json` to local fork URLs
3. Patches `puniversald` chain RPC config (`local-native/data/universal-N/.puniversal/config/pushuv_config.json`) to use local fork endpoints

Default local fork URLs (override in `.env`):

| Variable | Default | Description |
|---|---|---|
| `ANVIL_SEPOLIA_HOST_RPC_URL` | `http://localhost:9545` | Anvil Sepolia host URL (forge/cast + chain config patch) |
| `ANVIL_ARBITRUM_HOST_RPC_URL` | `http://localhost:9546` | Anvil Arbitrum Sepolia host URL |
| `ANVIL_BASE_HOST_RPC_URL` | `http://localhost:9547` | Anvil Base Sepolia host URL |
| `ANVIL_BSC_HOST_RPC_URL` | `http://localhost:9548` | Anvil BSC Testnet host URL |
| `SURFPOOL_SOLANA_HOST_RPC_URL` | `http://localhost:8899` | Surfpool Solana devnet host URL |
| `LOCAL_SEPOLIA_UV_RPC_URL` | ← `ANVIL_SEPOLIA_HOST_RPC_URL` | RPC written into UV `pushuv_config.json` (can differ from host if using Docker networking) |
| `LOCAL_ARBITRUM_UV_RPC_URL` | ← `ANVIL_ARBITRUM_HOST_RPC_URL` | UV-side Arbitrum RPC |
| `LOCAL_BASE_UV_RPC_URL` | ← `ANVIL_BASE_HOST_RPC_URL` | UV-side Base RPC |
| `LOCAL_BSC_UV_RPC_URL` | ← `ANVIL_BSC_HOST_RPC_URL` | UV-side BSC RPC |
| `LOCAL_SOLANA_UV_RPC_URL` | ← `SURFPOOL_SOLANA_HOST_RPC_URL` | UV-side Solana RPC |

---

## One-command full run

```bash
make replace-addresses
make build
TESTING_ENV=LOCAL bash e2e-tests/setup.sh all
```

The `all` pipeline runs in order:

1. `setup-environment` — start anvil/surfpool + patch chain RPC configs (LOCAL) or sync testnet RPCs
2. Build binaries (`make replace-addresses` + `make build`)
3. Auto-derive `FUND_TO_ADDRESS` from `PRIVATE_KEY` (writes to `.env`)
4. Stop any running nodes cleanly
5. `devnet` — start 4 validators, register 4 universal validators, start 2 (edit `./devnet start-uv N` to start more)
6. `tss-keygen` — TSS key generation (via `./local-native/devnet tss-keygen`)
7. `setup-environment` (second run — patches UV `pushuv_config.json` with `event_start_from` after devnet data exists)
8. `recover-genesis-key` — import genesis mnemonic into local keyring
9. `fund` — top up deployer address from genesis account
10. `setup-core` — deploy core contracts (forge, auto-resume)
11. `setup-swap` — deploy WPC + Uniswap V3 (hardhat)
12. `sync-addresses` — copy addresses into swap `test-addresses.json`
13. `create-pool` — create WPC liquidity pools for all tokens
14. `check-addresses` — assert required contract addresses are recorded
15. `write-core-env` — generate core contracts `.env`
16. `configure-core` — run `configureUniversalCore.s.sol` (forge, auto-resume; internally re-generates core `.env`)
17. `update-token-config` — patch token config JSON files
18. `setup-gateway` — deploy gateway contracts (forge, auto-resume)
19. `add-uregistry-configs` — submit chain + token config txs
20. `deploy-counter-sdk` — deploy CounterPayable + sync SDK constants
21. Sync SDK LOCALNET synthetic token constants from `deploy_addresses.json`
22. `sync-vault-tss` — sync vault TSS addresses on all local Anvil EVM chains (LOCAL only)

> `setup-sdk` is **not** included in `all`. Run it separately before any `sdk-test-*` command (see [Running SDK E2E tests](#running-sdk-e2e-tests)).

---

## Running SDK E2E tests

The SDK repo is cloned/installed and patched to point at the local deployment only when `setup-sdk` runs. After `all` finishes:

```bash
# Clone push-chain-sdk, generate its .env, install deps, sync LOCALNET constants
TESTING_ENV=LOCAL bash e2e-tests/setup.sh setup-sdk

# Inbound test suite (TESTNET_DONUT → LOCALNET rewrite applied to spec files)
TESTING_ENV=LOCAL bash e2e-tests/setup.sh sdk-test-all

# Outbound test suite (requires TESTING_ENV=LOCAL; also funds TSS signer + vault TSS sync)
TESTING_ENV=LOCAL bash e2e-tests/setup.sh sdk-test-outbound-all

# Single inbound file
TESTING_ENV=LOCAL bash e2e-tests/setup.sh sdk-test-send-to-self
```

Route-2 outbound tests (`cea-to-eoa.spec.ts`) additionally require a bootstrapped CEA on the BSC testnet fork:

```bash
TESTING_ENV=LOCAL bash e2e-tests/setup.sh bootstrap-cea-sdk
TESTING_ENV=LOCAL bash e2e-tests/setup.sh sdk-test-cea-to-eoa
```

---

## Local devnet (`local-native/devnet`)

The `devnet` script manages 4 `pchaind` validators and 4 `puniversald` universal validators as local OS processes (no Docker).

```
local-native/
  devnet          # management script
  data/           # validator home dirs + PID file (gitignored)
  logs/           # per-process log files (gitignored)
```

### Devnet commands

```bash
./local-native/devnet start 4              # Start 4 core validators
./local-native/devnet setup-uvalidators    # Register UVs on-chain + create AuthZ grants
./local-native/devnet start-uv 2          # Start 2 universal validators (or 4 for full set)
./local-native/devnet stop                # Stop all processes (keep data)
./local-native/devnet down                # Stop and remove data
./local-native/devnet status              # Show running processes + block heights
./local-native/devnet logs [name]         # Tail logs (validator-1, universal-2, all, …)
./local-native/devnet tss-keygen          # Initiate TSS key generation
```

Port layout:

| Node | RPC | EVM JSON-RPC | WS |
|---|---|---|---|
| validator-1 | 26657 | 8545 | 8546 |
| validator-2 | 26658 | 8547 | 8548 |
| validator-3 | 26659 | 8549 | 8550 |
| validator-4 | 26660 | 8551 | 8552 |

| UV | Query | TSS P2P |
|---|---|---|
| universal-validator-1 | 8080 | 39000 |
| universal-validator-2 | 8081 | 39001 |
| universal-validator-3 | 8082 | 39002 |
| universal-validator-4 | 8083 | 39003 |

### Clean devnet restart

```bash
./local-native/devnet down
./local-native/devnet start 4
./local-native/devnet setup-uvalidators
./local-native/devnet start-uv 4
```

---

## setup.sh command reference

```bash
TESTING_ENV=LOCAL bash e2e-tests/setup.sh <command>
```

| Command | Description |
|---|---|
| `all` | Full setup pipeline |
| `setup-environment` | Start anvil/surfpool + patch chain RPC configs |
| `devnet` | Start local devnet + register universal validators |
| `print-genesis` | Print first genesis account + mnemonic |
| `recover-genesis-key` | Import genesis mnemonic into local keyring |
| `fund` | Fund `FUND_TO_ADDRESS` from genesis account |
| `setup-core` | Build + deploy core contracts (auto-resume) |
| `setup-swap` | Build + deploy WPC + Uniswap V3 |
| `sync-addresses` | Copy `deploy_addresses.json` into swap `test-addresses.json` |
| `create-pool` | Create WPC pools for all deployed core tokens |
| `fund-uea-prc20` | Transfer PRC20 tokens from deployer to the test UEA address |
| `configure-core` | Run `configureUniversalCore.s.sol` (auto-resume) |
| `check-addresses` | Assert required contract addresses are recorded |
| `write-core-env` | Generate core contracts `.env` |
| `update-token-config` | Patch token config JSON contract addresses |
| `setup-gateway` | Build + deploy gateway contracts (auto-resume) |
| `sync-vault-tss` | Sync vault `TSS_ADDRESS` to current TSS key on all local Anvil chains (LOCAL only) |
| `add-uregistry-configs` | Submit chain + token configs to uregistry |
| `deploy-counter-sdk` | Deploy CounterPayable + sync SDK `COUNTER_ADDRESS_PAYABLE` |
| `bootstrap-cea-sdk` | Ensure CEA is deployed for SDK signer on BSC testnet fork (Route 2 bootstrap) |
| `setup-sdk` | Clone/install SDK, generate SDK `.env`, sync LOCALNET constants |
| `sdk-test-all` | Run all configured inbound SDK E2E test files |
| `sdk-test-outbound-all` | Run all configured outbound SDK E2E test files (LOCAL only) |
| `sdk-test-pctx-last-transaction` | Run `pctx-last-transaction.spec.ts` |
| `sdk-test-send-to-self` | Run `send-to-self.spec.ts` |
| `sdk-test-progress-hook` | Run `progress-hook-per-tx.spec.ts` |
| `sdk-test-bridge-multicall` | Run `bridge-multicall.spec.ts` |
| `sdk-test-pushchain` | Run `pushchain.spec.ts` |
| `sdk-test-bridge-hooks` | Run `bridge-hooks.spec.ts` |
| `sdk-test-cea-to-eoa` | Run `cea-to-eoa.spec.ts` (outbound Route 3; requires `TESTING_ENV=LOCAL`) |
| `record-contract K A` | Manually record contract key + address |
| `record-token N S A` | Manually record token name, symbol, address |
| `help` | Show help |

---

## Address tracking model

`e2e-tests/deploy_addresses.json` is the canonical address registry.

### Required contracts

- `contracts.WPC`
- `contracts.Factory`
- `contracts.QuoterV2`
- `contracts.SwapRouter`
- `contracts.UEA_PROXY_IMPLEMENTATION` (resolved from on-chain precompile during `setup-sdk`)
- `contracts.COUNTER_ADDRESS_PAYABLE`

### Token entries

`tokens[]` records each synthetic ERC-20 deployed by core contracts (`name`, `symbol`, `address`, `decimals`).

These addresses are used to:

- sync swap repo `test-addresses.json`
- generate core contracts `.env`
- update `config/testnet-donut/tokens/*.json`
- submit token config txs to uregistry

Manual helpers:

```bash
./e2e-tests/setup.sh record-contract Factory 0x1234...
./e2e-tests/setup.sh record-token "Push ETH" pETH 0x1234...
```

---

## Adding a new token to the setup

To register a new synthetic token in the local bootstrap, edit `../push-chain-core-contracts/scripts/localSetup/setup.s.sol` and add the token there. The `all` pipeline will deploy it and automatically create a WPC ↔ token liquidity pool as part of `create-pool`.

Note: this only handles pools paired with WPC. If you need a pool between two non-WPC tokens, additional adjustments are required (extra pool-creation logic in the swap setup and matching entries in the token/uregistry configs).

---

## Auto-retry and resilience behavior

### Forge scripts (core, gateway, configureUniversalCore)

- Stale broadcast cache from previous runs is cleared automatically before each fresh deploy.
- If the initial `forge script --broadcast` fails (e.g., receipt timeout), retries with `--resume` until success.
- Caps (all default `0` = unlimited retries):
  - `CORE_RESUME_MAX_ATTEMPTS` — core contracts deploy
  - `GATEWAY_RESUME_MAX_ATTEMPTS` — gateway contracts deploy
  - `CORE_CONFIGURE_RESUME_MAX_ATTEMPTS` — `configureUniversalCore.s.sol`

### uregistry tx submission

- Retries automatically on `account sequence mismatch`.
- Validates tx result by checking the returned `code` field.

---

## Generated files of interest

| File | Description |
|---|---|
| `e2e-tests/deploy_addresses.json` | Contract/token address registry |
| `e2e-tests/logs/` | Per-step deployment logs |
| `local-native/data/` | Validator + UV home directories |
| `local-native/logs/` | Per-process stdout/stderr |
| `<SWAP_AMM_DIR>/test-addresses.json` | Swap repo address file (synced from deploy_addresses.json) |
| `<CORE_CONTRACTS_DIR>/.env` | Core contracts env (generated by `write-core-env`) |
| `config/testnet-donut/*/tokens/*.json` | Token config files (updated contract addresses) |

---

## Clean full re-run

```bash
# Stop + wipe devnet
./local-native/devnet down

# Reset state
rm -f e2e-tests/deploy_addresses.json

# Rebuild + run
make replace-addresses
make build
TESTING_ENV=LOCAL bash e2e-tests/setup.sh all
```

---

## Troubleshooting

### 1) `pchaind` or `puniversald` won't start

Check that `make build` completed successfully and `build/pchaind` / `build/puniversald` exist.

### 2) Validators stuck at height 0

P2P peer connections failing. The devnet script sets `allow_duplicate_ip = true` and `addr_book_strict = false` automatically for all-localhost setups. If reusing old data, run `./local-native/devnet down` to wipe and restart clean.

### 3) TSS keygen not completing

Check UV logs (`./local-native/devnet logs universal-1`). UVs need:
- All 4 validators bonded
- All 4 UVs registered with AuthZ grants
- External chain RPC endpoints configured (set by `setup-environment`)

### 4) Core/gateway forge script keeps stopping with receipt errors

Expected intermittently. The script auto-retries with `--resume` until all receipts confirm.

### 5) `account sequence mismatch` in uregistry tx

The script retries automatically.

### 6) Swap AMM deployment fails mid-run

Re-run the individual step:

```bash
TESTING_ENV=LOCAL bash e2e-tests/setup.sh setup-swap
```
