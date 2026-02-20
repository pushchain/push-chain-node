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
9. uregistry chain/token config submission

---

## What gets created

- `e2e-tests/repos/` — cloned external repos
  - push-chain-core-contracts
  - push-chain-swap-internal-amm-contracts
  - push-chain-gateway-contracts
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

---

## Configuration

Copy env template:

```bash
cp e2e-tests/.env.example e2e-tests/.env
```

Important variables in `.env`:

- `PUSH_RPC_URL` (default `http://localhost:8545`)
- `PRIVATE_KEY`
- `FUND_TO_ADDRESS`
- `POOL_CREATION_TOPUP_AMOUNT` (funding for deployer before pool creation)
- `CORE_CONTRACTS_BRANCH`
- `SWAP_AMM_BRANCH`
- `GATEWAY_BRANCH` (currently `e2e-push-node`)

Genesis account source:

- `GENESIS_ACCOUNTS_JSON` can point to a local file, but if missing `setup.sh` automatically
  reads `/tmp/push-accounts/genesis_accounts.json` from docker container `core-validator-1`.

Path settings are repository-relative and portable.

---

## One-command full run

```bash
./e2e-tests/setup.sh replace-addresses
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
./e2e-tests/setup.sh add-uregistry-configs
./e2e-tests/setup.sh replace-addresses
./e2e-tests/setup.sh all
```

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
./e2e-tests/setup.sh replace-addresses
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
