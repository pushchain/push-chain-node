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

## What gets created

- `local-setup-e2e/data/` — validator + universal-validator home directories
- `local-setup-e2e/logs/` — per-process log files
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

- `git`, `make`
- `jq`
- `node`, `npm`, `npx`
- `forge`, `cast` (Foundry)
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
| `TESTING_ENV` | _(empty)_ | Set to `LOCAL` for local devnet |
| `PUSH_RPC_URL` | `http://localhost:8545` | Push Chain EVM JSON-RPC |
| `PRIVATE_KEY` | — | EVM deployer private key (forge/hardhat) |
| `EVM_PRIVATE_KEY` | ← `PRIVATE_KEY` | SDK EVM signer key |
| `PUSH_PRIVATE_KEY` | ← `PRIVATE_KEY` | SDK Push Chain signer key |
| `FUND_TO_ADDRESS` | — | Address to top up from genesis account |
| `POOL_CREATION_TOPUP_AMOUNT` | `50000000000000000000upc` | Deployer top-up before pool creation |
| `CORE_CONTRACTS_BRANCH` | `e2e-push-node` | |
| `SWAP_AMM_BRANCH` | `e2e-push-node` | |
| `GATEWAY_BRANCH` | `e2e-push-node` | |
| `PUSH_CHAIN_SDK_BRANCH` | `outbound_changes` | |
| `PUSH_CHAIN_SDK_E2E_DIR` | `packages/core/__e2e__/evm/inbound` | Test directory inside SDK |

### TESTING_ENV=LOCAL

When set in `.env`, the `setup-environment` step (also called by `all`) does:

1. Starts local fork nodes:
   - `anvil` for Ethereum Sepolia, Arbitrum Sepolia, Base Sepolia, BSC Testnet
   - `surfpool` for Solana
2. Rewrites `public_rpc_url` in `config/testnet-donut/*/chain.json` to local fork URLs
3. Patches `puniversald` chain RPC config (`local-setup-e2e/data/universal-N/.puniversal/config/pushuv_config.json`) to use local fork endpoints

Default local fork URLs (override in `.env`):

| Variable | Default |
|---|---|
| `ANVIL_SEPOLIA_HOST_RPC_URL` | `http://localhost:9545` |
| `ANVIL_ARBITRUM_HOST_RPC_URL` | `http://localhost:9546` |
| `ANVIL_BASE_HOST_RPC_URL` | `http://localhost:9547` |
| `ANVIL_BSC_HOST_RPC_URL` | `http://localhost:9548` |
| `SURFPOOL_SOLANA_HOST_RPC_URL` | `http://localhost:8899` |

---

## One-command full run

```bash
make replace-addresses
make build
TESTING_ENV=LOCAL bash e2e-tests/setup.sh all
```

The `all` pipeline runs in order:

1. `setup-environment` — start anvil/surfpool + patch chain RPC configs
2. Build binaries (`make replace-addresses` + `make build`)
3. `devnet` — start 4 validators + 4 universal validators (clean)
4. `tss-keygen` — TSS key generation (via `./local-setup-e2e/devnet tss-keygen`)
5. `recover-genesis-key` — import genesis mnemonic into local keyring
6. `fund` — top up deployer address from genesis account
7. `setup-core` — deploy core contracts (forge, auto-resume)
8. `setup-swap` — deploy WPC + Uniswap V3 (hardhat)
9. `sync-addresses` — copy addresses into swap `test-addresses.json`
10. `create-pool` — create WPC liquidity pools for all tokens
11. `write-core-env` — generate core contracts `.env`
12. `configure-core` — run `configureUniversalCore.s.sol` (forge, auto-resume)
13. `update-token-config` — patch token config JSON files
14. `setup-gateway` — deploy gateway contracts (forge, auto-resume)
15. `add-uregistry-configs` — submit chain + token config txs
16. `deploy-counter-sdk` — deploy CounterPayable + sync SDK constants

---

## Local devnet (`local-setup-e2e/devnet`)

The `devnet` script manages 4 `pchaind` validators and 4 `puniversald` universal validators as local OS processes (no Docker).

```
local-setup-e2e/
  devnet          # management script
  data/           # validator home dirs + PID file (gitignored)
  logs/           # per-process log files (gitignored)
```

### Devnet commands

```bash
./local-setup-e2e/devnet start [--build]   # Start all 4 validators + 4 UVs
                                            # --build for clean start (wipes data)
./local-setup-e2e/devnet stop              # Stop all processes (keep data)
./local-setup-e2e/devnet down              # Stop and remove data
./local-setup-e2e/devnet status            # Show running processes + block heights
./local-setup-e2e/devnet logs [name]       # Tail logs (validator-1, universal-2, all, …)
./local-setup-e2e/devnet tss-keygen        # Initiate TSS key generation
./local-setup-e2e/devnet setup-uvalidators # Register UVs + create AuthZ grants
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
./local-setup-e2e/devnet down
./local-setup-e2e/devnet start --build
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
| `configure-core` | Run `configureUniversalCore.s.sol` (auto-resume) |
| `check-addresses` | Assert required contract addresses are recorded |
| `write-core-env` | Generate core contracts `.env` |
| `update-token-config` | Patch token config JSON contract addresses |
| `setup-gateway` | Build + deploy gateway contracts (auto-resume) |
| `add-uregistry-configs` | Submit chain + token configs to uregistry |
| `deploy-counter-sdk` | Deploy CounterPayable + sync SDK `COUNTER_ADDRESS_PAYABLE` |
| `bootstrap-cea-sdk` | Ensure CEA is deployed for SDK signer (Route 2 bootstrap) |
| `setup-sdk` | Install SDK dependencies + generate SDK `.env` |
| `sdk-test-all` | Run all configured SDK E2E test files |
| `sdk-test-pctx-last-transaction` | Run `pctx-last-transaction.spec.ts` |
| `sdk-test-send-to-self` | Run `send-to-self.spec.ts` |
| `sdk-test-progress-hook` | Run `progress-hook-per-tx.spec.ts` |
| `sdk-test-bridge-multicall` | Run `bridge-multicall.spec.ts` |
| `sdk-test-pushchain` | Run `pushchain.spec.ts` |
| `sdk-test-bridge-hooks` | Run `bridge-hooks.spec.ts` |
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
- `contracts.UEA_PROXY_IMPLEMENTATION`
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

## Auto-retry and resilience behavior

### Forge scripts (core, gateway, configureUniversalCore)

- Stale broadcast cache from previous runs is cleared automatically before each fresh deploy.
- If the initial `forge script --broadcast` fails (e.g., receipt timeout), retries with `--resume` until success.
- Optional cap: `CORE_RESUME_MAX_ATTEMPTS=5` (default `0` = unlimited).

### uregistry tx submission

- Retries automatically on `account sequence mismatch`.
- Validates tx result by checking the returned `code` field.

---

## Generated files of interest

| File | Description |
|---|---|
| `e2e-tests/deploy_addresses.json` | Contract/token address registry |
| `e2e-tests/logs/` | Per-step deployment logs |
| `local-setup-e2e/data/` | Validator + UV home directories |
| `local-setup-e2e/logs/` | Per-process stdout/stderr |
| `<SWAP_AMM_DIR>/test-addresses.json` | Swap repo address file (synced from deploy_addresses.json) |
| `<CORE_CONTRACTS_DIR>/.env` | Core contracts env (generated by `write-core-env`) |
| `config/testnet-donut/*/tokens/*.json` | Token config files (updated contract addresses) |

---

## Clean full re-run

```bash
# Stop + wipe devnet
./local-setup-e2e/devnet down

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

P2P peer connections failing. The devnet script sets `allow_duplicate_ip = true` and `addr_book_strict = false` automatically for all-localhost setups. If reusing old data, run `./local-setup-e2e/devnet down` to wipe and restart clean.

### 3) TSS keygen not completing

Check UV logs (`./local-setup-e2e/devnet logs universal-1`). UVs need:
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
