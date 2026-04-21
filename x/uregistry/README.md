# `x/uregistry` — Chain & Token Registry

The configuration layer for Push Chain's crosschain protocol. Maintains the source of truth for which external chains and which tokens on those chains the protocol talks to. Every other Push module reads from `uregistry`; nobody else writes to it.

## What It Does

- **Stores chain configs** — for each supported external chain (CAIP-2 keyed): public RPC URL, gateway contract address, gateway/vault method identifiers, block confirmation thresholds, gas oracle fetch interval, VM type, and inbound/outbound enabled flags.
- **Stores token configs** — per (chain, token address): symbol, decimals, native PRC20 representation, liquidity cap, ERC20/SPL/etc. type.
- **Deploys reserved system contracts** — on fresh genesis, deploys `UNIVERSAL_GATEWAY_PC` and reserved proxy slots into the EVM at deterministic addresses (`0x...C1`, `0x...B0`, `0x...B1`, `0x...B2`).
- **Exposes lookup helpers** for the rest of the codebase, including `GetTokenConfigByPRC20` (reverse lookup from a PRC20 contract address to its source-chain token).

## State (KV layout)

| Prefix | Collection | Type | Purpose |
|---|---|---|---|
| `0` | `Params` | `Item[Params]` | Module parameters (admin address) |
| `1` | `ChainConfigs` | `Map[string, ChainConfig]` | Per-CAIP-2 chain configuration |
| `2` | `TokenConfigs` | `Map[string, TokenConfig]` | Token configuration, keyed by `chain:address` |

The `ChainConfig` schema (selected fields):

```protobuf
message ChainConfig {
  string                chain                    = 1;  // CAIP-2 (e.g. "eip155:11155111")
  string                public_rpc_url           = 2;
  VmType                vm_type                  = 3;  // EVM | SVM | MOVE_VM | WASM_VM | ...
  string                gateway_address          = 4;
  repeated GatewayMethods gateway_methods        = 5;
  repeated VaultMethods   vault_methods          = 6;
  BlockConfirmation     block_confirmation       = 7;  // fast & standard inbound counts
  uint64                gas_oracle_fetch_interval = 8;
  ChainEnabled          enabled                  = 9;  // is_inbound_enabled, is_outbound_enabled
}
```

## Messages (`MsgServer`)

| Message | Authority | Purpose |
|---|---|---|
| `MsgAddChainConfig` | admin (`params.Admin`) | Register a new external chain |
| `MsgUpdateChainConfig` | admin | Modify an existing chain config |
| `MsgAddTokenConfig` | admin | Whitelist a token on a chain |
| `MsgUpdateTokenConfig` | admin | Modify a token config |
| `MsgRemoveTokenConfig` | admin | Remove a token from the whitelist |
| `MsgUpdateParams` | gov | Rotate the admin or update other params |

There is no validator-vote path here — chain and token additions are intentionally admin-curated. The expected workflow is gov passes `MsgUpdateParams` to install an admin key, and the admin executes config changes day-to-day.

## Queries

- `Params`
- `ChainConfig` — by CAIP-2 ID
- `AllChainConfigs` — paginated list
- `TokenConfig` — by (chain, address)
- `AllTokenConfigs` — paginated list
- `TokenConfigsByChain` — filter by chain

## Inter-module Dependencies

The keeper holds:
- `evmKeeper` — for deploying system contracts on genesis

It exports no hooks. `x/uexecutor` and `x/utss` call its lookup helpers (`GetChainConfig`, `IsChainInboundEnabled`, `IsChainOutboundEnabled`, `GetTokenConfig`, `GetTokenConfigByPRC20`) but never write.

## EVM Integration

On fresh genesis (`Exported=false`), `InitGenesis` calls `deploySystemContracts` to install:

| Slot | Address |
|---|---|
| `UNIVERSAL_GATEWAY_PC` | `0x00000000000000000000000000000000000000C1` (proxy) |
| `RESERVED_0` | `0x00000000000000000000000000000000000000B0` |
| `RESERVED_1` | `0x00000000000000000000000000000000000000B1` |
| `RESERVED_2` | `0x00000000000000000000000000000000000000B2` |
| `UNIVERSAL_BATCH_CALL` | `0x00000000000000000000000000000000000000Bc` |

These are EIP-1967 transparent proxies — runtime-deployed bytecode is committed verbatim in `keeper.go`. Helper functions `ReserveUGPC` and `FixReservedBytecode` exist for in-place upgrade migrations to (re)install bytecode without redeploying through normal EVM calls.

## Genesis

```protobuf
GenesisState {
  Params                params
  repeated ChainConfigEntry chain_configs
  repeated TokenConfigEntry token_configs
  bool                  exported    // skip system-contract deploy if true
}
```

Default admin in `params.go`: `push1negskcfqu09j5zvpk7nhvacnwyy2mafffy7r6a`.

## Configuration Files

The on-disk JSON registry under `/config/{mainnet,testnet-donut}/<chain>/` is what operators use to seed `uregistry` at genesis or via admin txs. Each chain has a `chain.json` plus a `tokens/` directory of per-token JSONs. See `/config/testnet-donut/eth_sepolia/` for the canonical example.

## Layout

```
x/uregistry/
|-- keeper/
|   |-- keeper.go              State, lookups, system-contract deployment
|   |-- msg_server.go          AddChainConfig, AddTokenConfig, ...
|   +-- query_server.go        gRPC queries
|-- types/
|   |-- types.pb.go            ChainConfig, TokenConfig, GatewayMethods, VaultMethods, enums
|   |-- params.go              Admin field
|   |-- keys.go                Store prefixes
|   |-- chain_config.go, block_confirmation.go, gateway_methods.go, chain_enabled.go
|   +-- expected_keepers.go    EVMKeeper interface
|-- module.go
|-- autocli.go
+-- depinject.go
```
