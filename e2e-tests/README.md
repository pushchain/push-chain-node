# e2e-tests setup

This folder provides a full, automated local E2E bootstrap for Push Chain.

It covers:

1. local-multi-validator devnet (Docker, validators + universal validators)
2. genesis key recovery + account funding
3. core contracts deployment
4. swap AMM deployment (WPC + V3 core + V3 periphery)
5. pool creation for pETH/WPC
6. core `.env` generation from deployed addresses
7. token config update (`eth_sepolia_eth.json`)
8. gateway contracts deployment
9. push-chain-sdk setup + E2E test runners
10. uregistry chain/token config submission

---

## What gets created

- `e2e-tests/repos/` — cloned external repos
  - push-chain-core-contracts
  - push-chain-swap-internal-amm-contracts
  - push-chain-gateway-contracts
  - push-chain-sdk
- `e2e-tests/logs/` — logs for each major deployment step
- `e2e-tests/deploy_addresses.json` — contract/token address source-of-truth

---

## Prerequisites

Required tools:

- `git`
- `jq`
- `node`, `npm`, `npx`
- `forge` (Foundry)
- `make`

Also ensure the Push Chain repo builds/runs locally.

Before running any e2e setup command, run:

```bash
make replace-addresses
```

---

## Configuration

Copy env template:

```bash
cp e2e-tests/.env.example e2e-tests/.env
```

Important variables in `.env`:

- `PUSH_RPC_URL` (default `http://localhost:8545`)
- `TESTING_ENV` (`LOCAL` enables anvil/surfpool + local RPC rewrites)
- `PRIVATE_KEY`
- `FUND_TO_ADDRESS`
- `POOL_CREATION_TOPUP_AMOUNT` (funding for deployer before pool creation)
- `CORE_CONTRACTS_BRANCH`
- `SWAP_AMM_BRANCH`
- `GATEWAY_BRANCH` (currently `e2e-push-node`)
- `PUSH_CHAIN_SDK_BRANCH` (default `outbound_changes`)
- `PUSH_CHAIN_SDK_E2E_DIR` (default `packages/core/__e2e__/evm/inbound`)

### TESTING_ENV=LOCAL behavior

Set this in `e2e-tests/.env` when running local fork-based E2E:

```bash
TESTING_ENV=LOCAL
```

When `TESTING_ENV=LOCAL`, `setup-environment` (and `all`) now does both:

1. starts local fork nodes (`anvil` for Sepolia/Arbitrum/Base/BSC and `surfpool` for Solana)
2. rewrites `public_rpc_url` in `config/testnet-donut/*/chain.json` to your configured local RPC URLs:
  - `ANVIL_SEPOLIA_HOST_RPC_URL` (default `http://localhost:9545`)
  - `ANVIL_ARBITRUM_HOST_RPC_URL` (default `http://localhost:9546`)
  - `ANVIL_BASE_HOST_RPC_URL` (default `http://localhost:9547`)
  - `ANVIL_BSC_HOST_RPC_URL` (default `http://localhost:9548`)
  - `SURFPOOL_SOLANA_HOST_RPC_URL` (default `http://localhost:8899`)
3. patches universal-validator container RPC endpoints (`pushuv_config.json`) to the corresponding local endpoints

Genesis account source:

- `GENESIS_ACCOUNTS_JSON` can point to a local file, but if missing `setup.sh` automatically
  reads `/tmp/push-accounts/genesis_accounts.json` from docker container `core-validator-1`.

Path settings are repository-relative and portable.

---

## One-command full run

```bash
make replace-addresses
./e2e-tests/setup.sh all
```

This runs the full sequence in order:

1. `devnet`
2. `recover-genesis-key`
3. `fund`
4. `setup-core`
5. `setup-swap`
6. `sync-addresses`
7. `create-pool`
8. `check-addresses`
9. `write-core-env`
10. `update-token-config`
11. `setup-gateway`
12. `add-uregistry-configs`

---

## Command reference

```bash
./e2e-tests/setup.sh devnet
./e2e-tests/setup.sh print-genesis
./e2e-tests/setup.sh recover-genesis-key
./e2e-tests/setup.sh fund
./e2e-tests/setup.sh setup-core
./e2e-tests/setup.sh setup-swap
./e2e-tests/setup.sh sync-addresses
./e2e-tests/setup.sh create-pool
./e2e-tests/setup.sh check-addresses
./e2e-tests/setup.sh write-core-env
./e2e-tests/setup.sh update-token-config
./e2e-tests/setup.sh setup-gateway
./e2e-tests/setup.sh setup-sdk
./e2e-tests/setup.sh sdk-test-all
./e2e-tests/setup.sh sdk-test-pctx-last-transaction
./e2e-tests/setup.sh sdk-test-send-to-self
./e2e-tests/setup.sh sdk-test-progress-hook
./e2e-tests/setup.sh sdk-test-bridge-multicall
./e2e-tests/setup.sh sdk-test-pushchain
./e2e-tests/setup.sh add-uregistry-configs
make replace-addresses
./e2e-tests/setup.sh all
```

### push-chain-sdk setup + tests

Clone and install dependencies in one command:

```bash
./e2e-tests/setup.sh setup-sdk
```

This executes:

- `yarn install`
- `npm install`
- `npm i --save-dev @types/bs58`

It also fetches `UEA_PROXY_IMPLEMENTATION` with:

- `cast call 0x00000000000000000000000000000000000000ea "UEA_PROXY_IMPLEMENTATION()(address)"`

Then it updates both:

- `e2e-tests/deploy_addresses.json` as `contracts.UEA_PROXY_IMPLEMENTATION`
- `push-chain-sdk/packages/core/src/lib/constants/chain.ts` at `[PUSH_NETWORK.LOCALNET]`

SDK tests are discovered from:

- `push-chain-sdk/packages/core/__e2e__/evm/inbound`

Run all configured SDK E2E files:

```bash
./e2e-tests/setup.sh sdk-test-all
```

Run single files:

```bash
./e2e-tests/setup.sh sdk-test-pctx-last-transaction
./e2e-tests/setup.sh sdk-test-send-to-self
./e2e-tests/setup.sh sdk-test-progress-hook
./e2e-tests/setup.sh sdk-test-bridge-multicall
./e2e-tests/setup.sh sdk-test-pushchain
```

Before each SDK test run, the script automatically rewrites these values in configured files:

- `PUSH_NETWORK.TESTNET_DONUT` → `PUSH_NETWORK.LOCALNET`
- `PUSH_NETWORK.TESTNET` → `PUSH_NETWORK.LOCALNET`

---

## Address tracking model

`deploy_addresses.json` is the canonical address registry used by later steps.

### Required contracts

- `contracts.WPC`
- `contracts.Factory`
- `contracts.QuoterV2`
- `contracts.SwapRouter`

### Token entries

- `tokens[]` from core deployment logs (`name`, `symbol`, `address`, `source`)

These addresses are used to:

- sync swap repo `test-addresses.json`
- generate core contracts `.env`
- update `config/testnet-donut/tokens/eth_sepolia_eth.json`

Manual helpers:

```bash
./e2e-tests/setup.sh record-contract Factory 0x1234567890123456789012345678901234567890
./e2e-tests/setup.sh record-token "Push ETH" pETH 0x1234567890123456789012345678901234567890
```

---

## Auto-retry and resilience behavior

### Core contracts

- Runs `forge script scripts/localSetup/setup.s.sol ...`
- If receipt fetch fails, auto-retries with `--resume` in a loop until success
- Optional cap via:

```bash
CORE_RESUME_MAX_ATTEMPTS=0   # 0 means unlimited (default)
```

### Gateway contracts

- Runs gateway `forge script ... setup.s.sol`
- If initial execution fails, retries with `--resume`

### uregistry tx submission

- Submits chain config then token config
- Retries automatically on account sequence mismatch
- Validates tx result by checking returned `code`

---

## Generated files of interest

- `e2e-tests/deploy_addresses.json`
- `e2e-tests/repos/push-chain-swap-internal-amm-contracts/test-addresses.json`
- `e2e-tests/repos/push-chain-core-contracts/.env`
- `config/testnet-donut/tokens/eth_sepolia_eth.json` (updated contract address)

---

## Clean re-run

For a fresh run:

```bash
rm -rf e2e-tests/repos
./local-multi-validator/devnet down || true
make replace-addresses
./e2e-tests/setup.sh all
```

---

## Troubleshooting

### 1) Core script keeps stopping with receipt errors

This is expected intermittently on local RPC. The script auto-runs `--resume` until completion.

### 2) Missing branch in a dependency repo

The script attempts to resolve/fallback to available remote branches.

### 3) `account sequence mismatch` in uregistry tx

The script retries automatically for this error.

### 4) WPC deployment artifact not found

`setup-swap` compiles before deployment. If interrupted mid-run, re-run:

```bash
./e2e-tests/setup.sh setup-swap
```
