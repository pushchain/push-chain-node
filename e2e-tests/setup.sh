#!/usr/bin/env bash

set -euo pipefail
IFS=$'\n\t'

SCRIPT_DIR="$(cd -P "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PUSH_CHAIN_DIR_DEFAULT="$(cd -P "$SCRIPT_DIR/.." && pwd)"
ENV_FILE="$SCRIPT_DIR/.env"

if [[ -f "$ENV_FILE" ]]; then
  set -a
  source "$ENV_FILE"
  set +a
fi

: "${PUSH_CHAIN_DIR:=$PUSH_CHAIN_DIR_DEFAULT}"
: "${PUSH_RPC_URL:=http://localhost:8545}"
: "${CHAIN_ID:=localchain_9000-1}"
: "${TESTING_ENV:=}"
: "${KEYRING_BACKEND:=test}"
: "${GENESIS_KEY_NAME:=genesis-acc-1}"
: "${GENESIS_KEY_HOME:=./e2e-tests/.pchain}"
: "${GENESIS_ACCOUNTS_JSON:=./e2e-tests/genesis_accounts.json}"
: "${FUND_AMOUNT:=1000000000000000000upc}"
: "${POOL_CREATION_TOPUP_AMOUNT:=50000000000000000000upc}"
: "${GAS_PRICES:=100000000000upc}"
: "${LOCAL_DEVNET_DIR:=./local-multi-validator}"
: "${LEGACY_LOCAL_NATIVE_DIR:=./local-native}"

: "${CORE_CONTRACTS_REPO:=https://github.com/pushchain/push-chain-core-contracts.git}"
: "${CORE_CONTRACTS_BRANCH:=e2e-push-node}"
: "${SWAP_AMM_REPO:=https://github.com/pushchain/push-chain-swap-internal-amm-contracts.git}"
: "${SWAP_AMM_BRANCH:=e2e-push-node}"
: "${GATEWAY_REPO:=https://github.com/pushchain/push-chain-gateway-contracts.git}"
: "${GATEWAY_BRANCH:=e2e-push-node}"
: "${PUSH_CHAIN_SDK_REPO:=https://github.com/pushchain/push-chain-sdk.git}"
: "${PUSH_CHAIN_SDK_BRANCH:=outbound_changes}"

: "${E2E_PARENT_DIR:=../}"
: "${CORE_CONTRACTS_DIR:=$E2E_PARENT_DIR/push-chain-core-contracts}"
: "${SWAP_AMM_DIR:=$E2E_PARENT_DIR/push-chain-swap-internal-amm-contracts}"
: "${GATEWAY_DIR:=$E2E_PARENT_DIR/push-chain-gateway-contracts}"
: "${PUSH_CHAIN_SDK_DIR:=$E2E_PARENT_DIR/push-chain-sdk}"
: "${PUSH_CHAIN_SDK_E2E_DIR:=packages/core/__e2e__/evm/inbound}"
: "${PUSH_CHAIN_SDK_CHAIN_CONSTANTS_PATH:=packages/core/src/lib/constants/chain.ts}"
: "${PUSH_CHAIN_SDK_ACCOUNT_TS_PATH:=packages/core/src/lib/universal/account/account.ts}"
: "${PUSH_CHAIN_SDK_CORE_ENV_PATH:=packages/core/.env}"
: "${DEPLOY_ADDRESSES_FILE:=$SCRIPT_DIR/deploy_addresses.json}"
: "${LOG_DIR:=$SCRIPT_DIR/logs}"
: "${TEST_ADDRESSES_PATH:=$SWAP_AMM_DIR/test-addresses.json}"
: "${TOKENS_CONFIG_DIR:=./config/testnet-donut}"
: "${TOKEN_CONFIG_PATH:=./config/testnet-donut/eth_sepolia/tokens/eth.json}"
: "${CHAIN_CONFIG_PATH:=./config/testnet-donut/eth_sepolia/chain.json}"

abs_from_root() {
  local path="$1"
  if [[ "$path" = /* ]]; then
    printf "%s" "$path"
  else
    printf "%s/%s" "$PUSH_CHAIN_DIR" "${path#./}"
  fi
}

GENESIS_KEY_HOME="$(abs_from_root "$GENESIS_KEY_HOME")"
GENESIS_ACCOUNTS_JSON="$(abs_from_root "$GENESIS_ACCOUNTS_JSON")"
LOCAL_DEVNET_DIR="$(abs_from_root "$LOCAL_DEVNET_DIR")"
LEGACY_LOCAL_NATIVE_DIR="$(abs_from_root "$LEGACY_LOCAL_NATIVE_DIR")"
E2E_PARENT_DIR="$(abs_from_root "$E2E_PARENT_DIR")"
CORE_CONTRACTS_DIR="$(abs_from_root "$CORE_CONTRACTS_DIR")"
SWAP_AMM_DIR="$(abs_from_root "$SWAP_AMM_DIR")"
GATEWAY_DIR="$(abs_from_root "$GATEWAY_DIR")"
PUSH_CHAIN_SDK_DIR="$(abs_from_root "$PUSH_CHAIN_SDK_DIR")"
DEPLOY_ADDRESSES_FILE="$(abs_from_root "$DEPLOY_ADDRESSES_FILE")"
TEST_ADDRESSES_PATH="$(abs_from_root "$TEST_ADDRESSES_PATH")"
LOG_DIR="$(abs_from_root "$LOG_DIR")"
TOKENS_CONFIG_DIR="$(abs_from_root "$TOKENS_CONFIG_DIR")"
TOKEN_CONFIG_PATH="$(abs_from_root "$TOKEN_CONFIG_PATH")"
CHAIN_CONFIG_PATH="$(abs_from_root "$CHAIN_CONFIG_PATH")"

mkdir -p "$LOG_DIR"

green='\033[0;32m'
yellow='\033[0;33m'
red='\033[0;31m'
cyan='\033[0;36m'
nc='\033[0m'

log_info() { printf "%b\n" "${cyan}==>${nc} $*"; }
log_ok() { printf "%b\n" "${green}✓${nc} $*"; }
log_warn() { printf "%b\n" "${yellow}!${nc} $*"; }
log_err() { printf "%b\n" "${red}x${nc} $*"; }

ensure_testing_env_var_in_env_file() {
  mkdir -p "$(dirname "$ENV_FILE")"

  if [[ ! -f "$ENV_FILE" ]]; then
    printf "TESTING_ENV=\n" >"$ENV_FILE"
    return
  fi

  if ! grep -Eq '^TESTING_ENV=' "$ENV_FILE"; then
    printf "\nTESTING_ENV=\n" >>"$ENV_FILE"
  fi
}

is_local_testing_env() {
  [[ "${TESTING_ENV:-}" == "LOCAL" ]]
}

get_genesis_accounts_json() {
  if [[ -f "$GENESIS_ACCOUNTS_JSON" ]]; then
    cat "$GENESIS_ACCOUNTS_JSON"
    return 0
  fi

  if command -v docker >/dev/null 2>&1; then
    if docker ps --format '{{.Names}}' | grep -qx 'core-validator-1'; then
      if docker exec core-validator-1 test -f /tmp/push-accounts/genesis_accounts.json >/dev/null 2>&1; then
        docker exec core-validator-1 cat /tmp/push-accounts/genesis_accounts.json
        return 0
      fi
    fi
  fi

  return 1
}

require_cmd() {
  local c
  for c in "$@"; do
    command -v "$c" >/dev/null 2>&1 || {
      log_err "Missing command: $c"
      exit 1
    }
  done
}

list_remote_branches() {
  local repo_url="$1"
  git ls-remote --heads "$repo_url" | awk '{print $2}' | sed 's#refs/heads/##'
}

select_best_matching_branch() {
  local requested="$1"
  shift
  local branches=("$@")
  local best=""
  local best_score=0
  local branch token score

  # Tokenize requested branch by non-alphanumeric delimiters.
  local tokens=()
  while IFS= read -r token; do
    [[ -n "$token" ]] && tokens+=("$token")
  done < <(echo "$requested" | tr -cs '[:alnum:]' '\n' | tr '[:upper:]' '[:lower:]')

  for branch in "${branches[@]}"; do
    score=0
    local b_lc
    b_lc="$(echo "$branch" | tr '[:upper:]' '[:lower:]')"
    for token in "${tokens[@]}"; do
      if [[ "$b_lc" == *"$token"* ]]; then
        score=$((score + 1))
      fi
    done
    if (( score > best_score )); then
      best_score=$score
      best="$branch"
    fi
  done

  if (( best_score >= 2 )); then
    printf "%s" "$best"
  fi
}

resolve_branch() {
  local repo_url="$1"
  local requested="$2"
  local branches=()
  local b

  while IFS= read -r b; do
    [[ -n "$b" ]] && branches+=("$b")
  done < <(list_remote_branches "$repo_url")

  local branch
  for branch in "${branches[@]}"; do
    if [[ "$branch" == "$requested" ]]; then
      printf "%s" "$requested"
      return
    fi
  done

  local best
  best="$(select_best_matching_branch "$requested" "${branches[@]}")"
  if [[ -n "$best" ]]; then
    printf "%b\n" "${yellow}!${nc} Branch '$requested' not found. Auto-selected '$best'." >&2
    printf "%s" "$best"
    return
  fi

  for branch in main master; do
    for b in "${branches[@]}"; do
      if [[ "$b" == "$branch" ]]; then
        printf "%b\n" "${yellow}!${nc} Branch '$requested' not found. Falling back to '$branch'." >&2
        printf "%s" "$branch"
        return
      fi
    done
  done

  if [[ ${#branches[@]} -gt 0 ]]; then
    printf "%b\n" "${yellow}!${nc} Branch '$requested' not found. Falling back to '${branches[0]}'." >&2
    printf "%s" "${branches[0]}"
    return
  fi

  log_err "No remote branches found for $repo_url"
  exit 1
}

ensure_deploy_file() {
  mkdir -p "$(dirname "$DEPLOY_ADDRESSES_FILE")"

  if [[ ! -s "$DEPLOY_ADDRESSES_FILE" ]]; then
    cat >"$DEPLOY_ADDRESSES_FILE" <<'JSON'
{
  "generatedAt": "",
  "contracts": {},
  "tokens": []
}
JSON
    return
  fi

  if ! jq -e . "$DEPLOY_ADDRESSES_FILE" >/dev/null 2>&1; then
    log_warn "Deploy file is empty/invalid JSON, reinitializing: $DEPLOY_ADDRESSES_FILE"
    cat >"$DEPLOY_ADDRESSES_FILE" <<'JSON'
{
  "generatedAt": "",
  "contracts": {},
  "tokens": []
}
JSON
    return
  fi

  local tmp
  tmp="$(mktemp)"
  jq '
    .generatedAt = (.generatedAt // "")
    | .contracts = (.contracts // {})
    | .tokens = (.tokens // [])
  ' "$DEPLOY_ADDRESSES_FILE" >"$tmp"
  mv "$tmp" "$DEPLOY_ADDRESSES_FILE"
}

set_generated_at() {
  local tmp
  tmp="$(mktemp)"
  jq --arg now "$(date -u +%Y-%m-%dT%H:%M:%SZ)" '.generatedAt = $now' "$DEPLOY_ADDRESSES_FILE" >"$tmp"
  mv "$tmp" "$DEPLOY_ADDRESSES_FILE"
}

record_contract() {
  local key="$1"
  local address="$2"
  local tmp
  tmp="$(mktemp)"
  jq --arg key "$key" --arg val "$address" '.contracts[$key] = $val' "$DEPLOY_ADDRESSES_FILE" >"$tmp"
  mv "$tmp" "$DEPLOY_ADDRESSES_FILE"
  set_generated_at
  log_ok "Recorded contract $key=$address"
}

record_token() {
  local name="$1"
  local symbol="$2"
  local address="$3"
  local source="$4"
  local tmp
  tmp="$(mktemp)"
  jq \
    --arg name "$name" \
    --arg symbol "$symbol" \
    --arg address "$address" \
    --arg source "$source" \
    '
      .tokens = (
        ([.tokens[]? | select((.address | ascii_downcase) != ($address | ascii_downcase))])
        + [{name:$name, symbol:$symbol, address:$address, source:$source}]
      )
    ' "$DEPLOY_ADDRESSES_FILE" >"$tmp"
  mv "$tmp" "$DEPLOY_ADDRESSES_FILE"
  set_generated_at
  log_ok "Recorded token $symbol=$address ($name)"
}

validate_eth_address() {
  [[ "$1" =~ ^0x[a-fA-F0-9]{40}$ ]]
}

clone_or_update_repo() {
  local repo_url="$1"
  local branch="$2"
  local dest="$3"
  local resolved_branch

  resolved_branch="$(resolve_branch "$repo_url" "$branch")"

  if [[ -d "$dest" && ! -d "$dest/.git" ]]; then
    log_warn "Removing non-git directory at $dest"
    rm -rf "$dest"
  fi

  if [[ -d "$dest/.git" ]]; then
    local current_branch has_changes
    current_branch="$(git -C "$dest" rev-parse --abbrev-ref HEAD 2>/dev/null || true)"
    has_changes="$(git -C "$dest" status --porcelain 2>/dev/null)"

    if [[ -n "$has_changes" && "$current_branch" == "$resolved_branch" ]]; then
      log_warn "Repo $(basename "$dest") has local changes on branch '$current_branch'. Skipping update to preserve local changes."
      return 0
    fi

    log_info "Updating repo $(basename "$dest")"
    local current_origin
    current_origin="$(git -C "$dest" remote get-url origin 2>/dev/null || true)"
    if [[ -z "$current_origin" || "$current_origin" != "$repo_url" ]]; then
      log_warn "Setting origin for $(basename "$dest") to $repo_url"
      if git -C "$dest" remote get-url origin >/dev/null 2>&1; then
        git -C "$dest" remote set-url origin "$repo_url"
      else
        git -C "$dest" remote add origin "$repo_url"
      fi
    fi

    git -C "$dest" fetch origin
    git -C "$dest" checkout -B "$resolved_branch" "origin/$resolved_branch"
    git -C "$dest" reset --hard "origin/$resolved_branch"
  else
    log_info "Cloning $(basename "$dest")"
    git clone --branch "$resolved_branch" "$repo_url" "$dest"
  fi
}

sdk_test_files() {
  local base_dir="$PUSH_CHAIN_SDK_DIR/$PUSH_CHAIN_SDK_E2E_DIR"
  local file alt
  local requested_files=(
    "pctx-last-transaction.spec.ts"
    "send-to-self.spec.ts"
    "progress-hook-per-tx.spec.ts"
    "bridge-multicall.spec.ts"
    "pushchain.spec.ts"
    "bridge-hooks.spec.ts"
  )

  for file in "${requested_files[@]}"; do
    if [[ -f "$base_dir/$file" ]]; then
      printf "%s\n" "$base_dir/$file"
      continue
    fi

    if [[ "$file" == *.tx ]]; then
      alt="${file%.tx}.ts"
      if [[ -f "$base_dir/$alt" ]]; then
        printf "%b\n" "${yellow}!${nc} Test file '$file' not found. Using '$alt'." >&2
        printf "%s\n" "$base_dir/$alt"
        continue
      fi
    fi

    log_err "SDK test file not found: $base_dir/$file"
    exit 1
  done
}

sdk_prepare_test_files_for_localnet() {
  require_cmd perl

  if [[ ! -d "$PUSH_CHAIN_SDK_DIR/.git" && ! -d "$PUSH_CHAIN_SDK_DIR" ]]; then
    log_err "SDK repo not found at $PUSH_CHAIN_SDK_DIR"
    log_err "Run: $0 setup-sdk"
    exit 1
  fi

  if [[ ! -d "$PUSH_CHAIN_SDK_DIR/$PUSH_CHAIN_SDK_E2E_DIR" ]]; then
    log_err "SDK E2E directory not found: $PUSH_CHAIN_SDK_DIR/$PUSH_CHAIN_SDK_E2E_DIR"
    exit 1
  fi

  while IFS= read -r test_file; do
    [[ -n "$test_file" ]] || continue
    perl -0pi -e 's/\bPUSH_NETWORK\.TESTNET_DONUT\b/PUSH_NETWORK.LOCALNET/g; s/\bPUSH_NETWORK\.TESTNET\b/PUSH_NETWORK.LOCALNET/g; s/\bCHAIN\.PUSH_TESTNET_DONUT\b/CHAIN.PUSH_LOCALNET/g' "$test_file"
    log_ok "Prepared LOCALNET network replacement in $(basename "$test_file")"
  done < <(sdk_test_files)
}

step_setup_push_chain_sdk() {
  require_cmd git yarn npm cast jq perl

  local chain_constants_file="$PUSH_CHAIN_SDK_DIR/$PUSH_CHAIN_SDK_CHAIN_CONSTANTS_PATH"
  local sdk_account_file="$PUSH_CHAIN_SDK_DIR/$PUSH_CHAIN_SDK_ACCOUNT_TS_PATH"
  local uea_impl_raw uea_impl synced_localnet_uea

  clone_or_update_repo "$PUSH_CHAIN_SDK_REPO" "$PUSH_CHAIN_SDK_BRANCH" "$PUSH_CHAIN_SDK_DIR"

  local sdk_env_path="$PUSH_CHAIN_SDK_DIR/$PUSH_CHAIN_SDK_CORE_ENV_PATH"
  local sdk_evm_private_key sdk_evm_rpc sdk_solana_rpc sdk_solana_private_key sdk_push_private_key

  sdk_evm_private_key="${EVM_PRIVATE_KEY:-${PRIVATE_KEY:-}}"
  sdk_evm_rpc="${EVM_RPC:-${PUSH_RPC_URL:-}}"
  sdk_solana_rpc="${SOLANA_RPC_URL:-https://api.devnet.solana.com}"
  sdk_solana_private_key="${SOLANA_PRIVATE_KEY:-${SVM_PRIVATE_KEY:-${SOL_PRIVATE_KEY:-}}}"
  sdk_push_private_key="${PUSH_PRIVATE_KEY:-${PRIVATE_KEY:-}}"

  mkdir -p "$(dirname "$sdk_env_path")"
  {
    echo "# Auto-generated by e2e-tests/setup.sh setup-sdk"
    echo "# Source: e2e-tests/.env"
    echo "EVM_PRIVATE_KEY=$sdk_evm_private_key"
    echo "EVM_RPC=$sdk_evm_rpc"
    echo "SOLANA_RPC_URL=$sdk_solana_rpc"
    echo "SOLANA_PRIVATE_KEY=$sdk_solana_private_key"
    echo "PUSH_PRIVATE_KEY=$sdk_push_private_key"
  } >"$sdk_env_path"

  [[ -n "$sdk_evm_private_key" ]] || log_warn "SDK env EVM_PRIVATE_KEY is empty (set EVM_PRIVATE_KEY or PRIVATE_KEY in e2e-tests/.env)"
  [[ -n "$sdk_evm_rpc" ]] || log_warn "SDK env EVM_RPC is empty (set EVM_RPC or PUSH_RPC_URL in e2e-tests/.env)"
  [[ -n "$sdk_solana_private_key" ]] || log_warn "SDK env SOLANA_PRIVATE_KEY is empty (set SOLANA_PRIVATE_KEY in e2e-tests/.env)"
  [[ -n "$sdk_push_private_key" ]] || log_warn "SDK env PUSH_PRIVATE_KEY is empty (set PUSH_PRIVATE_KEY or PRIVATE_KEY in e2e-tests/.env)"
  log_ok "Generated push-chain-sdk env file: $sdk_env_path"

  if [[ ! -f "$chain_constants_file" ]]; then
    log_err "SDK chain constants file not found: $chain_constants_file"
    exit 1
  fi

  log_info "Fetching UEA_PROXY_IMPLEMENTATION from local chain"
  uea_impl_raw="$(cast call 0x00000000000000000000000000000000000000ea 'UEA_PROXY_IMPLEMENTATION()(address)' --rpc-url "$PUSH_RPC_URL" 2>/dev/null || true)"
  uea_impl="$(echo "$uea_impl_raw" | grep -Eo '0x[a-fA-F0-9]{40}' | head -1 || true)"

  if ! validate_eth_address "$uea_impl"; then
    log_err "Could not resolve valid UEA_PROXY_IMPLEMENTATION address from cast output: $uea_impl_raw"
    exit 1
  fi

  ensure_deploy_file
  record_contract "UEA_PROXY_IMPLEMENTATION" "$uea_impl"

  UEA_PROXY_IMPL="$uea_impl" perl -0pi -e 's#(\[PUSH_NETWORK\.LOCALNET\]:\s*)'\''[^'\'']*'\''#$1'\''$ENV{UEA_PROXY_IMPL}'\''#g' "$chain_constants_file"

  synced_localnet_uea="$(grep -E '\[PUSH_NETWORK\.LOCALNET\]:' "$chain_constants_file" | head -1 | sed -E "s/.*'([^']+)'.*/\1/")"
  if [[ "$synced_localnet_uea" != "$uea_impl" ]]; then
    log_err "Failed to update PUSH_NETWORK.LOCALNET UEA proxy in $chain_constants_file"
    exit 1
  fi

  log_ok "Synced PUSH_NETWORK.LOCALNET UEA proxy to $uea_impl"

  if [[ ! -f "$sdk_account_file" ]]; then
    log_err "SDK account file not found: $sdk_account_file"
    exit 1
  fi

  perl -0pi -e '
    s{(function\s+convertExecutorToOriginAccount\b.*?\{)(.*?)(\n\})}{
      my ($head, $body, $tail) = ($1, $2, $3);
      $body =~ s/\bCHAIN\.PUSH_TESTNET_DONUT\b/CHAIN.PUSH_LOCALNET/g;
      "$head$body$tail";
    }gse;
  ' "$sdk_account_file"
  log_ok "Replaced CHAIN.PUSH_TESTNET_DONUT with CHAIN.PUSH_LOCALNET only in convertExecutorToOriginAccount() in $sdk_account_file"

  log_info "Installing push-chain-sdk dependencies"
  (
    cd "$PUSH_CHAIN_SDK_DIR"
    yarn install
    npm install
    npm i --save-dev @types/bs58
  )

  log_ok "push-chain-sdk setup complete"
}

step_run_sdk_test_file() {
  local test_basename="$1"
  local test_file=""

  sdk_prepare_test_files_for_localnet

  while IFS= read -r candidate; do
    [[ -n "$candidate" ]] || continue
    if [[ "$(basename "$candidate")" == "$test_basename" ]]; then
      test_file="$candidate"
      break
    fi
  done < <(sdk_test_files)

  if [[ -z "$test_file" ]]; then
    log_err "Requested SDK test file not in configured list: $test_basename"
    exit 1
  fi

  log_info "Running SDK test: $test_basename"
  (
    cd "$PUSH_CHAIN_SDK_DIR"
    npx nx test core --runInBand --testPathPattern="$(basename "$test_file")"
  )

  log_ok "Completed SDK test: $test_basename"
}

step_run_sdk_tests_all() {
  local test_file

  sdk_prepare_test_files_for_localnet

  while IFS= read -r test_file; do
    [[ -n "$test_file" ]] || continue
    log_info "Running SDK test: $(basename "$test_file")"
    (
      cd "$PUSH_CHAIN_SDK_DIR"
      npx nx test core --runInBand --testPathPattern="$(basename "$test_file")"
    )
  done < <(sdk_test_files)

  log_ok "Completed all configured SDK E2E tests"
}

step_devnet() {
  require_cmd bash
  log_info "Starting local-multi-validator devnet"
  (
    cd "$LOCAL_DEVNET_DIR"
    ./devnet start --build
    ./devnet setup-uvalidators
  )
  log_ok "Devnet is up"
}

step_setup_environment() {
  if ! is_local_testing_env; then
    log_info "TESTING_ENV is not LOCAL, skipping setup-environment"
    return 0
  fi

  require_cmd anvil cast docker jq surfpool curl

  local sepolia_host_rpc="${ANVIL_SEPOLIA_HOST_RPC_URL:-http://localhost:9545}"
  local arbitrum_host_rpc="${ANVIL_ARBITRUM_HOST_RPC_URL:-http://localhost:9546}"
  local base_host_rpc="${ANVIL_BASE_HOST_RPC_URL:-http://localhost:9547}"
  local bsc_host_rpc="${ANVIL_BSC_HOST_RPC_URL:-http://localhost:9548}"

  local uv_sepolia_rpc_url="${LOCAL_SEPOLIA_UV_RPC_URL:-http://host.docker.internal:9545}"
  local uv_arbitrum_rpc_url="${LOCAL_ARBITRUM_UV_RPC_URL:-http://host.docker.internal:9546}"
  local uv_base_rpc_url="${LOCAL_BASE_UV_RPC_URL:-http://host.docker.internal:9547}"
  local uv_bsc_rpc_url="${LOCAL_BSC_UV_RPC_URL:-http://host.docker.internal:9548}"
  local solana_host_rpc="${SURFPOOL_SOLANA_HOST_RPC_URL:-http://localhost:8899}"
  local uv_solana_rpc_url="${LOCAL_SOLANA_UV_RPC_URL:-http://host.docker.internal:8899}"

  patch_chain_config_public_rpc() {
    local file_path="$1"
    local rpc_url="$2"
    local label="$3"
    local tmp

    if [[ ! -f "$file_path" ]]; then
      log_warn "Chain config file not found for $label: $file_path"
      return 0
    fi

    tmp="$(mktemp)"
    jq --arg rpc "$rpc_url" '.public_rpc_url = $rpc' "$file_path" >"$tmp"
    mv "$tmp" "$file_path"
    log_ok "Patched $label chain config public_rpc_url => $rpc_url"
  }

  patch_local_testnet_donut_chain_configs() {
    patch_chain_config_public_rpc "$TOKENS_CONFIG_DIR/eth_sepolia/chain.json" "$sepolia_host_rpc" "eth_sepolia"
    patch_chain_config_public_rpc "$TOKENS_CONFIG_DIR/arb_sepolia/chain.json" "$arbitrum_host_rpc" "arb_sepolia"
    patch_chain_config_public_rpc "$TOKENS_CONFIG_DIR/base_sepolia/chain.json" "$base_host_rpc" "base_sepolia"
    patch_chain_config_public_rpc "$TOKENS_CONFIG_DIR/bsc_testnet/chain.json" "$bsc_host_rpc" "bsc_testnet"
    patch_chain_config_public_rpc "$TOKENS_CONFIG_DIR/solana_devnet/chain.json" "$solana_host_rpc" "solana_devnet"
  }

  start_anvil_fork() {
    local label="$1"
    local port="$2"
    local chain_id="$3"
    local fork_url="$4"
    local anvil_pattern="anvil --port $port"

    if pgrep -f "$anvil_pattern" >/dev/null 2>&1; then
      log_info "Stopping existing anvil $label on port $port"
      pkill -f "$anvil_pattern" >/dev/null 2>&1 || true
      sleep 1
    fi

    log_info "Starting anvil $label on port $port (chain-id: $chain_id)"
    nohup anvil --host 0.0.0.0 --port "$port" --chain-id "$chain_id" --fork-url "$fork_url" --block-time 1 \
      >"$LOG_DIR/anvil_${label}.log" 2>&1 &
  }

  wait_for_block_number() {
    local label="$1"
    local rpc_url="$2"
    local latest=""
    local i
    for i in {1..30}; do
      latest="$(cast block-number --rpc-url "$rpc_url" 2>/dev/null || true)"
      latest="$(echo "$latest" | tr -d '[:space:]')"
      if [[ "$latest" =~ ^[0-9]+$ ]]; then
        printf "%s" "$latest"
        return 0
      fi
      sleep 1
    done

    log_err "Could not read latest block number from $label anvil at $rpc_url"
    return 1
  }

  start_surfpool() {
    local surfpool_pattern="surfpool start --port 8899 --network devnet"

    if pgrep -f "$surfpool_pattern" >/dev/null 2>&1; then
      log_info "Stopping existing surfpool on port 8899"
      pkill -f "$surfpool_pattern" >/dev/null 2>&1 || true
      sleep 1
    fi

    log_info "Starting surfpool for local Solana testing on port 8899"
    nohup surfpool start --port 8899 --network devnet >"$LOG_DIR/surfpool.log" 2>&1 &
  }

  wait_for_solana_slot() {
    local rpc_url="$1"
    local slot=""
    local response
    local i
    for i in {1..30}; do
      response="$(curl -sS -X POST "$rpc_url" -H 'Content-Type: application/json' --data '{"jsonrpc":"2.0","id":1,"method":"getSlot","params":[{"commitment":"processed"}]}' || true)"
      slot="$(echo "$response" | jq -r '.result // empty' 2>/dev/null || true)"
      slot="$(echo "$slot" | tr -d '[:space:]')"
      if [[ "$slot" =~ ^[0-9]+$ ]]; then
        printf "%s" "$slot"
        return 0
      fi
      sleep 1
    done

    log_err "Could not read latest Solana slot from surfpool at $rpc_url"
    return 1
  }

  start_anvil_fork "sepolia" "9545" "11155111" "https://ethereum-sepolia-rpc.publicnode.com"
  start_anvil_fork "arbitrum" "9546" "421614" "https://arbitrum-sepolia.gateway.tenderly.co"
  start_anvil_fork "base" "9547" "84532" "https://sepolia.base.org"
  start_anvil_fork "bsc" "9548" "97" "https://bsc-testnet-rpc.publicnode.com"
  start_surfpool
  patch_local_testnet_donut_chain_configs

  local sepolia_latest_block arbitrum_latest_block base_latest_block bsc_latest_block solana_latest_slot
  sepolia_latest_block="$(wait_for_block_number "sepolia" "$sepolia_host_rpc")"
  arbitrum_latest_block="$(wait_for_block_number "arbitrum" "$arbitrum_host_rpc")"
  base_latest_block="$(wait_for_block_number "base" "$base_host_rpc")"
  bsc_latest_block="$(wait_for_block_number "bsc" "$bsc_host_rpc")"
  solana_latest_slot="$(wait_for_solana_slot "$solana_host_rpc")"

  local patched_count=0
  local config_path="/root/.puniversal/config/pushuv_config.json"
  local uv_container
  for uv_container in universal-validator-1 universal-validator-2 universal-validator-3 universal-validator-4; do
    if ! docker ps --format '{{.Names}}' | grep -qx "$uv_container"; then
      continue
    fi

    local tmp_in tmp_out
    tmp_in="$(mktemp)"
    tmp_out="$(mktemp)"

    if ! docker exec "$uv_container" cat "$config_path" >"$tmp_in"; then
      rm -f "$tmp_in" "$tmp_out"
      log_warn "Failed to read $config_path from $uv_container"
      continue
    fi

    jq \
      --arg sepolia_rpc "$uv_sepolia_rpc_url" \
      --arg arbitrum_rpc "$uv_arbitrum_rpc_url" \
      --arg base_rpc "$uv_base_rpc_url" \
      --arg bsc_rpc "$uv_bsc_rpc_url" \
      --arg solana_rpc "$uv_solana_rpc_url" \
      --argjson sepolia_start "$sepolia_latest_block" \
      --argjson arbitrum_start "$arbitrum_latest_block" \
      --argjson base_start "$base_latest_block" \
      --argjson bsc_start "$bsc_latest_block" \
      --argjson solana_start "$solana_latest_slot" \
      '
        .chain_configs["eip155:11155111"].rpc_urls = [$sepolia_rpc]
        | .chain_configs["eip155:11155111"].event_start_from = $sepolia_start
        | .chain_configs["eip155:421614"].rpc_urls = [$arbitrum_rpc]
        | .chain_configs["eip155:421614"].event_start_from = $arbitrum_start
        | .chain_configs["eip155:84532"].rpc_urls = [$base_rpc]
        | .chain_configs["eip155:84532"].event_start_from = $base_start
        | .chain_configs["eip155:97"].rpc_urls = [$bsc_rpc]
        | .chain_configs["eip155:97"].event_start_from = $bsc_start
        | .chain_configs["solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1"].rpc_urls = [$solana_rpc]
        | .chain_configs["solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1"].event_start_from = $solana_start
      ' "$tmp_in" >"$tmp_out"

    docker cp "$tmp_out" "$uv_container":"$config_path"
    rm -f "$tmp_in" "$tmp_out"

    patched_count=$((patched_count + 1))
    log_ok "Updated $uv_container config for Sepolia/Arbitrum/Base/BSC/Solana local forks"
  done

  if [[ "$patched_count" -eq 0 ]]; then
    log_warn "No universal-validator containers are running yet; skipped pushuv_config.json patch"
    return 0
  fi

  log_ok "Patched $patched_count universal validator config(s) with local fork RPC/event_start_from (including Solana)"
}

step_stop_running_nodes() {
  log_info "Stopping running local nodes/validators"

  if [[ -x "$LOCAL_DEVNET_DIR/devnet" ]]; then
    (
      cd "$LOCAL_DEVNET_DIR"
      ./devnet down || true
    )
  fi

  if [[ -x "$LEGACY_LOCAL_NATIVE_DIR/devnet" ]]; then
    (
      cd "$LEGACY_LOCAL_NATIVE_DIR"
      ./devnet down || true
    )
  fi

  pkill -f "$PUSH_CHAIN_DIR/build/pchaind start" >/dev/null 2>&1 || true
  pkill -f "$PUSH_CHAIN_DIR/build/puniversald" >/dev/null 2>&1 || true

  log_ok "Running nodes stopped"
}

step_print_genesis() {
  require_cmd jq
  local accounts_json
  if ! accounts_json="$(get_genesis_accounts_json)"; then
    log_err "Could not resolve genesis accounts from $GENESIS_ACCOUNTS_JSON or docker container core-validator-1"
    exit 1
  fi

  jq -r '.[0] | "Account: \(.name)\nAddress: \(.address)\nMnemonic: \(.mnemonic)"' <<<"$accounts_json"
}

step_recover_genesis_key() {
  require_cmd "$PUSH_CHAIN_DIR/build/pchaind" jq

  local mnemonic="${GENESIS_MNEMONIC:-}"
  if [[ -z "$mnemonic" ]]; then
    local accounts_json
    accounts_json="$(get_genesis_accounts_json || true)"
    if [[ -n "$accounts_json" ]]; then
      mnemonic="$(jq -r --arg n "$GENESIS_KEY_NAME" '
        (first(.[] | select(.name == $n) | .mnemonic) // first(.[].mnemonic) // "")
      ' <<<"$accounts_json")"
    fi
  fi

  if [[ -z "$mnemonic" ]]; then
    log_err "Could not auto-resolve mnemonic from $GENESIS_ACCOUNTS_JSON or docker container core-validator-1"
    log_err "Set GENESIS_MNEMONIC in e2e-tests/.env"
    exit 1
  fi

  if "$PUSH_CHAIN_DIR/build/pchaind" keys show "$GENESIS_KEY_NAME" \
    --keyring-backend "$KEYRING_BACKEND" \
    --home "$GENESIS_KEY_HOME" >/dev/null 2>&1; then
    log_warn "Key ${GENESIS_KEY_NAME} already exists. Deleting before recover."
    "$PUSH_CHAIN_DIR/build/pchaind" keys delete "$GENESIS_KEY_NAME" \
      --keyring-backend "$KEYRING_BACKEND" \
      --home "$GENESIS_KEY_HOME" \
      -y >/dev/null
  fi

  log_info "Recovering key ${GENESIS_KEY_NAME}"
  printf "%s\n" "$mnemonic" | "$PUSH_CHAIN_DIR/build/pchaind" keys add "$GENESIS_KEY_NAME" \
    --recover \
    --keyring-backend "$KEYRING_BACKEND" \
    --algo eth_secp256k1 \
    --home "$GENESIS_KEY_HOME" >/dev/null

  log_ok "Recovered key ${GENESIS_KEY_NAME}"
}

step_fund_account() {
  require_cmd "$PUSH_CHAIN_DIR/build/pchaind"

  local to_addr="${FUND_TO_ADDRESS:-}"
  if [[ -z "$to_addr" ]]; then
    log_err "Set FUND_TO_ADDRESS in e2e-tests/.env"
    exit 1
  fi
  if ! validate_eth_address "$to_addr" && [[ ! "$to_addr" =~ ^push1[0-9a-z]+$ ]]; then
    log_err "Invalid FUND_TO_ADDRESS: $to_addr"
    exit 1
  fi

  log_info "Funding $to_addr with $FUND_AMOUNT"
  "$PUSH_CHAIN_DIR/build/pchaind" tx bank send "$GENESIS_KEY_NAME" "$to_addr" "$FUND_AMOUNT" \
    --gas-prices "$GAS_PRICES" \
    --keyring-backend "$KEYRING_BACKEND" \
    --chain-id "$CHAIN_ID" \
    --home "$GENESIS_KEY_HOME" \
    -y

  log_ok "Funding transaction submitted"
}

step_update_env_fund_to_address() {
  require_cmd jq
  ENV_FILE="$SCRIPT_DIR/.env"
  if [[ ! -f "$ENV_FILE" ]]; then
    log_err ".env file not found in e2e-tests folder"
    exit 1
  fi
  PRIVATE_KEY=$(grep '^PRIVATE_KEY=' "$ENV_FILE" | cut -d= -f2 | tr -d '"' | tr -d "'")
  if [[ -z "$PRIVATE_KEY" ]]; then
    log_err "PRIVATE_KEY not found in .env"
    exit 1
  fi
  if ! command -v $PUSH_CHAIN_DIR/build/pchaind >/dev/null 2>&1; then
    log_err "pchaind binary not found in build/ (run make build)"
    exit 1
  fi
  EVM_ADDRESS=$(cast wallet address $PRIVATE_KEY)
  COSMOS_ADDRESS=$($PUSH_CHAIN_DIR/build/pchaind debug addr $(echo $EVM_ADDRESS | tr '[:upper:]' '[:lower:]' | sed 's/^0x//') | awk -F': ' '/Bech32 Acc:/ {print $2; exit}')
  if [[ -z "$COSMOS_ADDRESS" ]]; then
    log_err "Could not derive cosmos address from $EVM_ADDRESS"
    exit 1
  fi
  if grep -q '^FUND_TO_ADDRESS=' "$ENV_FILE"; then
    sed -i.bak "s|^FUND_TO_ADDRESS=.*$|FUND_TO_ADDRESS=$COSMOS_ADDRESS|" "$ENV_FILE"
  else
    echo "FUND_TO_ADDRESS=$COSMOS_ADDRESS" >> "$ENV_FILE"
  fi
  # Refresh .env after updating FUND_TO_ADDRESS
  set -a
  source "$SCRIPT_DIR/.env"
  set +a
  log_ok "Updated FUND_TO_ADDRESS in .env to $COSMOS_ADDRESS"
}

parse_core_prc20_logs() {
  local log_file="$1"
  local current_addr=""
  local line

  while IFS= read -r line; do
    if [[ "$line" =~ PRC20[[:space:]]deployed[[:space:]]at:[[:space:]](0x[a-fA-F0-9]{40}) ]]; then
      current_addr="${BASH_REMATCH[1]}"
      continue
    fi

    if [[ -n "$current_addr" && "$line" =~ Name:[[:space:]](.+)[[:space:]]Symbol:[[:space:]]([A-Za-z0-9._-]+)$ ]]; then
      local token_name="${BASH_REMATCH[1]}"
      local token_symbol="${BASH_REMATCH[2]}"
      record_token "$token_name" "$token_symbol" "$current_addr" "core-contracts"
      current_addr=""
    fi
  done <"$log_file"
}

enrich_core_token_decimals() {
  require_cmd jq cast
  ensure_deploy_file

  local addr decimals tmp
  while IFS= read -r addr; do
    [[ -n "$addr" ]] || continue
    decimals="$(cast call "$addr" "decimals()(uint8)" --rpc-url "$PUSH_RPC_URL" 2>/dev/null || true)"
    decimals="$(echo "$decimals" | tr -d '[:space:]')"

    if [[ "$decimals" =~ ^[0-9]+$ ]]; then
      tmp="$(mktemp)"
      jq --arg addr "$addr" --argjson dec "$decimals" '
        .tokens |= map(
          if ((.address | ascii_downcase) == ($addr | ascii_downcase))
          then . + {decimals: $dec}
          else .
          end
        )
      ' "$DEPLOY_ADDRESSES_FILE" >"$tmp"
      mv "$tmp" "$DEPLOY_ADDRESSES_FILE"
      log_ok "Resolved token decimals: $addr => $decimals"
    else
      log_warn "Could not resolve decimals() for token $addr"
    fi
  done < <(jq -r '.tokens[]? | select(.decimals == null) | .address' "$DEPLOY_ADDRESSES_FILE")
}

step_setup_core_contracts() {
  require_cmd git forge jq
  [[ -n "${PRIVATE_KEY:-}" ]] || { log_err "Set PRIVATE_KEY in e2e-tests/.env"; exit 1; }

  ensure_deploy_file
  clone_or_update_repo "$CORE_CONTRACTS_REPO" "$CORE_CONTRACTS_BRANCH" "$CORE_CONTRACTS_DIR"

  log_info "Running forge build in core contracts"
  (cd "$CORE_CONTRACTS_DIR" && forge build)

  local log_file="$LOG_DIR/core_setup_$(date +%Y%m%d_%H%M%S).log"
  local failed=0
  local resume_attempt=1
  local resume_max_attempts="${CORE_RESUME_MAX_ATTEMPTS:-0}"  # 0 = unlimited

  log_info "Running local core setup script"
  (
    cd "$CORE_CONTRACTS_DIR"
    forge script scripts/localSetup/setup.s.sol \
      --broadcast \
      --rpc-url "$PUSH_RPC_URL" \
      --private-key "$PRIVATE_KEY" \
      --slow
  ) 2>&1 | tee "$log_file" || failed=1

  if [[ "$failed" -ne 0 ]]; then
    log_warn "Initial run failed. Retrying with --resume until success"
    while true; do
      log_info "Resume attempt: $resume_attempt"
      if (
        cd "$CORE_CONTRACTS_DIR"
        forge script scripts/localSetup/setup.s.sol \
          --broadcast \
          --rpc-url "$PUSH_RPC_URL" \
          --private-key "$PRIVATE_KEY" \
          --slow \
          --resume
      ) 2>&1 | tee -a "$log_file"; then
        break
      fi

      if [[ "$resume_max_attempts" != "0" && "$resume_attempt" -ge "$resume_max_attempts" ]]; then
        log_err "Reached CORE_RESUME_MAX_ATTEMPTS=$resume_max_attempts without success"
        exit 1
      fi

      resume_attempt=$((resume_attempt + 1))
      sleep 2
    done
  fi

  parse_core_prc20_logs "$log_file"
  enrich_core_token_decimals
  log_ok "Core contracts setup complete"
}

find_first_address_with_keywords() {
  local log_file="$1"
  shift
  local pattern
  pattern="$(printf '%s|' "$@")"
  pattern="${pattern%|}"
  grep -Ei "$pattern" "$log_file" | grep -Eo '0x[a-fA-F0-9]{40}' | tail -1 || true
}

address_from_deploy_contract() {
  local key="$1"
  jq -r --arg k "$key" '.contracts[$k] // ""' "$DEPLOY_ADDRESSES_FILE"
}

address_from_deploy_token() {
  local sym="$1"
  jq -r --arg s "$sym" 'first(.tokens[]? | select((.symbol|ascii_downcase) == ($s|ascii_downcase)) | .address) // ""' "$DEPLOY_ADDRESSES_FILE"
}

resolve_peth_token_address() {
  local addr=""
  addr="$(address_from_deploy_token "pETH")"
  [[ -n "$addr" ]] || addr="$(address_from_deploy_token "WETH")"
  if [[ -z "$addr" ]]; then
    addr="$(jq -r 'first(.tokens[]? | select((.name|ascii_downcase) | test("eth")) | .address) // ""' "$DEPLOY_ADDRESSES_FILE")"
  fi
  printf "%s" "$addr"
}

assert_required_addresses() {
  ensure_deploy_file
  local required=("WPC" "Factory" "QuoterV2" "SwapRouter")
  local missing=0
  local key val

  for key in "${required[@]}"; do
    val="$(address_from_deploy_contract "$key")"
    if [[ -z "$val" ]]; then
      log_warn "Missing address in deploy file: contracts.$key"
      missing=1
    else
      log_ok "contracts.$key=$val"
    fi
  done

  if [[ "$missing" -ne 0 ]]; then
    log_warn "Some addresses are missing in $DEPLOY_ADDRESSES_FILE; continuing with available values"
  fi
}

step_write_core_env() {
  require_cmd jq
  ensure_deploy_file
  assert_required_addresses

  local core_env="$CORE_CONTRACTS_DIR/.env"
  local wpc factory quoter router
  wpc="$(address_from_deploy_contract "WPC")"
  factory="$(address_from_deploy_contract "Factory")"
  quoter="$(address_from_deploy_contract "QuoterV2")"
  router="$(address_from_deploy_contract "SwapRouter")"

  log_info "Writing core-contracts .env"
  {
    echo "PUSH_RPC_URL=$PUSH_RPC_URL"
    echo "PRIVATE_KEY=$PRIVATE_KEY"
    echo "WPC_ADDRESS=$wpc"
    echo "FACTORY_ADDRESS=$factory"
    echo "QUOTER_V2_ADDRESS=$quoter"
    echo "SWAP_ROUTER_ADDRESS=$router"
    echo "WPC=$wpc"
    echo "UNISWAP_V3_FACTORY=$factory"
    echo "UNISWAP_V3_QUOTER=$quoter"
    echo "UNISWAP_V3_ROUTER=$router"
    echo ""
    echo "# Tokens deployed from core setup"
    jq -r '.tokens | to_entries[]? | "TOKEN" + ((.key + 1)|tostring) + "=" + .value.address' "$DEPLOY_ADDRESSES_FILE"
  } >"$core_env"

  log_ok "Generated $core_env"
}

step_update_eth_token_config() {
  step_update_deployed_token_configs
}

norm_token_key() {
  local s="$1"
  s="$(echo "$s" | tr '[:upper:]' '[:lower:]')"
  s="$(echo "$s" | sed -E 's/[^a-z0-9]+//g')"
  printf "%s" "$s"
}

norm_token_key_without_leading_p() {
  local s
  s="$(norm_token_key "$1")"
  if [[ "$s" == p* && ${#s} -gt 1 ]]; then
    printf "%s" "${s#p}"
  else
    printf "%s" "$s"
  fi
}

find_matching_token_config_file() {
  local deployed_symbol="$1"
  local deployed_name="$2"
  local best_file=""
  local best_score=0

  local d_sym d_name d_sym_np d_name_np
  d_sym="$(norm_token_key "$deployed_symbol")"
  d_name="$(norm_token_key "$deployed_name")"
  d_sym_np="$(norm_token_key_without_leading_p "$deployed_symbol")"
  d_name_np="$(norm_token_key_without_leading_p "$deployed_name")"

  local file f_sym f_name f_base f_sym_np f_name_np score
  while IFS= read -r file; do
    [[ -f "$file" ]] || continue
    f_sym="$(jq -r '.symbol // ""' "$file")"
    f_name="$(jq -r '.name // ""' "$file")"
    f_base="$(basename "$file" .json)"

    f_sym="$(norm_token_key "$f_sym")"
    f_name="$(norm_token_key "$f_name")"
    f_base="$(norm_token_key "$f_base")"
    f_sym_np="$(norm_token_key_without_leading_p "$f_sym")"
    f_name_np="$(norm_token_key_without_leading_p "$f_name")"

    score=0
    [[ -n "$d_sym" && "$d_sym" == "$f_sym" ]] && score=$((score + 100))
    [[ -n "$d_name" && "$d_name" == "$f_name" ]] && score=$((score + 90))
    [[ -n "$d_sym_np" && "$d_sym_np" == "$f_sym" ]] && score=$((score + 80))
    [[ -n "$d_name_np" && "$d_name_np" == "$f_name" ]] && score=$((score + 70))
    [[ -n "$d_sym" && "$d_sym" == "$f_name" ]] && score=$((score + 60))
    [[ -n "$d_name" && "$d_name" == "$f_sym" ]] && score=$((score + 60))
    [[ -n "$d_sym_np" && "$f_base" == *"$d_sym_np"* ]] && score=$((score + 30))
    [[ -n "$d_name_np" && "$f_base" == *"$d_name_np"* ]] && score=$((score + 20))

    if (( score > best_score )); then
      best_score=$score
      best_file="$file"
    fi
  done < <(find "$TOKENS_CONFIG_DIR" -type f -path '*/tokens/*.json' | sort)

  if (( best_score >= 60 )); then
    printf "%s" "$best_file"
  fi
}

step_update_deployed_token_configs() {
  require_cmd jq
  ensure_deploy_file

  if [[ ! -d "$TOKENS_CONFIG_DIR" ]]; then
    log_err "Tokens config directory missing: $TOKENS_CONFIG_DIR"
    exit 1
  fi

  if ! find "$TOKENS_CONFIG_DIR" -type f -path '*/tokens/*.json' | grep -q .; then
    log_err "No token config files found under: $TOKENS_CONFIG_DIR"
    exit 1
  fi

  local used_files=""
  local updated=0
  local token_json token_symbol token_name token_address match_file tmp

  while IFS= read -r token_json; do
    token_symbol="$(echo "$token_json" | jq -r '.symbol // ""')"
    token_name="$(echo "$token_json" | jq -r '.name // ""')"
    token_address="$(echo "$token_json" | jq -r '.address // ""')"

    [[ -n "$token_address" ]] || continue
    match_file="$(find_matching_token_config_file "$token_symbol" "$token_name")"

    if [[ -z "$match_file" ]]; then
      log_warn "No token config match found for deployed token: $token_symbol ($token_name)"
      continue
    fi

    if echo "$used_files" | grep -Fxq "$match_file"; then
      log_warn "Token config already matched by another token, skipping: $(basename "$match_file")"
      continue
    fi

    tmp="$(mktemp)"
    jq --arg a "$token_address" '.native_representation.contract_address = $a' "$match_file" >"$tmp"
    mv "$tmp" "$match_file"
    used_files+="$match_file"$'\n'
    updated=$((updated + 1))
    log_ok "Updated $(basename "$match_file") contract_address => $token_address"
  done < <(jq -c '.tokens[]?' "$DEPLOY_ADDRESSES_FILE")

  if [[ "$updated" -eq 0 ]]; then
    log_warn "No token config files were updated from deployed tokens"
  else
    log_ok "Updated $updated token config file(s) from deployed tokens"
  fi
}

step_setup_swap_amm() {
  require_cmd git node npm npx jq
  [[ -n "${PRIVATE_KEY:-}" ]] || { log_err "Set PRIVATE_KEY in e2e-tests/.env"; exit 1; }

  ensure_deploy_file
  clone_or_update_repo "$SWAP_AMM_REPO" "$SWAP_AMM_BRANCH" "$SWAP_AMM_DIR"

  log_info "Installing swap-amm dependencies"
  (
    cd "$SWAP_AMM_DIR"
    npm install
    (cd v3-core && npm install)
    (cd v3-periphery && npm install)
  )

  log_info "Writing swap repo .env from main e2e .env"
  cat >"$SWAP_AMM_DIR/.env" <<EOF
PUSH_RPC_URL=$PUSH_RPC_URL
PRIVATE_KEY=$PRIVATE_KEY
EOF

  local wpc_log="$LOG_DIR/swap_wpc_$(date +%Y%m%d_%H%M%S).log"
  log_info "Deploying WPC token"
  (
    cd "$SWAP_AMM_DIR"
    npx hardhat compile
    node scripts/deploy-wpush.js
  ) 2>&1 | tee "$wpc_log"

  local wpc_addr
  wpc_addr="$(find_first_address_with_keywords "$wpc_log" wpc wpush wrapped)"
  if [[ -n "$wpc_addr" ]]; then
    record_contract "WPC" "$wpc_addr"
  else
    log_warn "Could not auto-detect WPC address from logs"
  fi

  local core_log="$LOG_DIR/swap_core_$(date +%Y%m%d_%H%M%S).log"
  log_info "Deploying v3-core"
  (
    cd "$SWAP_AMM_DIR/v3-core"
    npx hardhat compile
    npx hardhat run scripts/deploy-core.js --network pushchain
  ) 2>&1 | tee "$core_log"

  local factory_addr
  factory_addr="$(grep -E 'Factory Address|FACTORY_ADDRESS=' "$core_log" | grep -Eo '0x[a-fA-F0-9]{40}' | tail -1 || true)"
  if [[ -n "$factory_addr" ]]; then
    record_contract "Factory" "$factory_addr"
  else
    log_warn "Could not auto-detect Factory address from logs"
  fi

  local periphery_log="$LOG_DIR/swap_periphery_$(date +%Y%m%d_%H%M%S).log"
  log_info "Deploying v3-periphery"
  (
    cd "$SWAP_AMM_DIR/v3-periphery"
    npx hardhat compile
    npx hardhat run scripts/deploy-periphery.js --network pushchain
  ) 2>&1 | tee "$periphery_log"

  local swap_router quoter_v2 position_manager
  swap_router="$(grep -E 'SwapRouter' "$periphery_log" | grep -Eo '0x[a-fA-F0-9]{40}' | tail -1 || true)"
  quoter_v2="$(grep -E 'QuoterV2' "$periphery_log" | grep -Eo '0x[a-fA-F0-9]{40}' | tail -1 || true)"
  position_manager="$(grep -E 'PositionManager' "$periphery_log" | grep -Eo '0x[a-fA-F0-9]{40}' | tail -1 || true)"
  wpc_addr="$(grep -E '^.*WPC:' "$periphery_log" | grep -Eo '0x[a-fA-F0-9]{40}' | tail -1 || true)"

  [[ -n "$swap_router" ]] && record_contract "SwapRouter" "$swap_router"
  [[ -n "$quoter_v2" ]] && record_contract "QuoterV2" "$quoter_v2"
  [[ -n "$position_manager" ]] && record_contract "PositionManager" "$position_manager"
  [[ -n "$wpc_addr" ]] && record_contract "WPC" "$wpc_addr"

  assert_required_addresses

  log_ok "Swap AMM setup complete"
}

step_setup_gateway() {
  require_cmd git forge
  [[ -n "${PRIVATE_KEY:-}" ]] || { log_err "Set PRIVATE_KEY in e2e-tests/.env"; exit 1; }

  clone_or_update_repo "$GATEWAY_REPO" "$GATEWAY_BRANCH" "$GATEWAY_DIR"

  log_info "Preparing gateway repo submodules"
  (
    cd "$GATEWAY_DIR"
    if [[ -d "contracts/svm-gateway/mock-pyth" ]]; then
      git rm --cached contracts/svm-gateway/mock-pyth || true
      rm -rf contracts/svm-gateway/mock-pyth
    fi
    git submodule update --init --recursive
  )

  local gw_dir="$GATEWAY_DIR/contracts/evm-gateway"
  local gw_log="$LOG_DIR/gateway_setup_$(date +%Y%m%d_%H%M%S).log"
  local failed=0
  local resume_attempt=1
  local resume_max_attempts="${GATEWAY_RESUME_MAX_ATTEMPTS:-0}"  # 0 = unlimited

  log_info "Building gateway evm contracts"
  (cd "$gw_dir" && forge build)

  log_info "Running gateway local setup script"
  (
    cd "$gw_dir"
    forge script scripts/localSetup/setup.s.sol \
      --broadcast \
      --rpc-url "$PUSH_RPC_URL" \
      --private-key "$PRIVATE_KEY" \
      --slow
  ) 2>&1 | tee "$gw_log" || failed=1

  if [[ "$failed" -ne 0 ]]; then
    log_warn "Gateway script failed. Retrying with --resume until success"
    while true; do
      log_info "Gateway resume attempt: $resume_attempt"
      if (
        cd "$gw_dir"
        forge script scripts/localSetup/setup.s.sol \
          --broadcast \
          --rpc-url "$PUSH_RPC_URL" \
          --private-key "$PRIVATE_KEY" \
          --slow \
          --resume
      ) 2>&1 | tee -a "$gw_log"; then
        break
      fi

      if [[ "$resume_max_attempts" != "0" && "$resume_attempt" -ge "$resume_max_attempts" ]]; then
        log_err "Reached GATEWAY_RESUME_MAX_ATTEMPTS=$resume_max_attempts without success"
        exit 1
      fi

      resume_attempt=$((resume_attempt + 1))
      sleep 2
    done
  fi

  log_ok "Gateway setup complete"
}

step_add_uregistry_configs() {
  require_cmd "$PUSH_CHAIN_DIR/build/pchaind" jq

  [[ -d "$TOKENS_CONFIG_DIR" ]] || { log_err "Missing tokens config directory: $TOKENS_CONFIG_DIR"; exit 1; }

  local token_payload

  run_registry_tx() {
    local kind="$1"
    local payload="$2"
    local max_attempts=10
    local attempt=1
    local out code raw

    while true; do
      if [[ "$kind" == "chain" ]]; then
        out="$("$PUSH_CHAIN_DIR/build/pchaind" tx uregistry add-chain-config \
          --chain-config "$payload" \
          --from "$GENESIS_KEY_NAME" \
          --keyring-backend "$KEYRING_BACKEND" \
          --home "$GENESIS_KEY_HOME" \
          --node tcp://127.0.0.1:26657 \
          --gas-prices "$GAS_PRICES" \
          -y)"
      else
        out="$("$PUSH_CHAIN_DIR/build/pchaind" tx uregistry add-token-config \
          --token-config "$payload" \
          --from "$GENESIS_KEY_NAME" \
          --keyring-backend "$KEYRING_BACKEND" \
          --home "$GENESIS_KEY_HOME" \
          --node tcp://127.0.0.1:26657 \
          --gas-prices "$GAS_PRICES" \
          -y)"
      fi
      echo "$out"
      if [[ "$out" =~ ^\{ ]]; then
        code="$(echo "$out" | jq -r '.code // 1')"
        raw="$(echo "$out" | jq -r '.raw_log // ""')"
      else
        code="$(echo "$out" | awk -F': ' '/^code:/ {print $2; exit}')"
        raw="$(echo "$out" | awk -F': ' '/^raw_log:/ {sub(/^\x27|\x27$/, "", $2); print $2; exit}')"
        [[ -n "$code" ]] || code="1"
      fi

      if [[ "$code" == "0" ]]; then
        return 0
      fi

      if [[ "$raw" == *"account sequence mismatch"* && "$attempt" -lt "$max_attempts" ]]; then
        log_warn "Sequence mismatch on attempt $attempt/$max_attempts. Retrying..."
        attempt=$((attempt + 1))
        sleep 2
        continue
      fi

      log_err "Registry tx failed: code=$code raw_log=$raw"
      return 1
    done
  }

  local chain_config_dir chain_file chain_payload chain_count
  chain_config_dir="$TOKENS_CONFIG_DIR"
  chain_count=0

  while IFS= read -r chain_file; do
    [[ -f "$chain_file" ]] || continue
    chain_payload="$(jq -c . "$chain_file")"
    log_info "Adding chain config to uregistry: $(basename "$chain_file")"
    run_registry_tx "chain" "$chain_payload"
    chain_count=$((chain_count + 1))
  done < <(find "$chain_config_dir" -type f \( -name 'chain.json' -o -name '*_chain_config.json' \) | sort)

  if [[ "$chain_count" -eq 0 ]]; then
    log_err "No chain config files found in: $chain_config_dir"
    exit 1
  fi

  log_ok "Registered $chain_count chain config(s) from $chain_config_dir"

  local token_json token_file token_addr token_symbol token_name matched_count submitted_files tmp
  matched_count=0
  submitted_files=""

  while IFS= read -r token_json; do
    token_symbol="$(echo "$token_json" | jq -r '.symbol // ""')"
    token_name="$(echo "$token_json" | jq -r '.name // ""')"
    token_addr="$(echo "$token_json" | jq -r '.address // ""')"

    [[ -n "$token_addr" ]] || continue

    token_file="$(find_matching_token_config_file "$token_symbol" "$token_name")"
    if [[ -z "$token_file" ]]; then
      log_warn "No token config match found for deployed token (uregistry): $token_symbol ($token_name)"
      continue
    fi

    if echo "$submitted_files" | grep -Fxq "$token_file"; then
      log_warn "Token config already submitted by another deployed token, skipping: $(basename "$token_file")"
      continue
    fi

    tmp="$(mktemp)"
    jq --arg a "$token_addr" '.native_representation.contract_address = $a' "$token_file" >"$tmp"
    mv "$tmp" "$token_file"

    token_payload="$(jq -c . "$token_file")"
    log_info "Adding token config to uregistry: $(basename "$token_file") (from $token_symbol)"
    run_registry_tx "token" "$token_payload"

    submitted_files+="$token_file"$'\n'
    matched_count=$((matched_count + 1))
  done < <(jq -c '.tokens[]?' "$DEPLOY_ADDRESSES_FILE")

  if [[ "$matched_count" -eq 0 ]]; then
    log_warn "No token configs were registered from deploy_addresses.json tokens"
  else
    log_ok "Registered $matched_count token config(s) from deploy_addresses.json"
  fi

  log_ok "uregistry chain/token configs added"
}

step_sync_test_addresses() {
  require_cmd jq
  ensure_deploy_file

  if [[ ! -f "$TEST_ADDRESSES_PATH" ]]; then
    log_err "test-addresses.json not found: $TEST_ADDRESSES_PATH"
    exit 1
  fi

  log_info "Syncing deploy addresses into test-addresses.json"
  local tmp
  tmp="$(mktemp)"

  jq \
    --arg today "$(date +%F)" \
    --arg rpc "$PUSH_RPC_URL" \
    --slurpfile dep "$DEPLOY_ADDRESSES_FILE" \
    '
      ($dep[0]) as $d
      | def token_addr($sym): first(($d.tokens[]? | select(.symbol == $sym) | .address), empty);
      .lastUpdated = $today
      | .network.rpcUrl = $rpc
      | if ($d.contracts.Factory // "") != "" then .contracts.factory = $d.contracts.Factory else . end
      | if ($d.contracts.WPC // "") != "" then .contracts.WPC = $d.contracts.WPC else . end
      | if ($d.contracts.SwapRouter // "") != "" then .contracts.swapRouter = $d.contracts.SwapRouter else . end
      | if ($d.contracts.PositionManager // "") != "" then .contracts.positionManager = $d.contracts.PositionManager else . end
      | if ($d.contracts.QuoterV2 // "") != "" then .contracts.quoterV2 = $d.contracts.QuoterV2 else . end
      | .testTokens |= with_entries(
          .value.address = (token_addr(.key) // .value.address)
        )
      | .testTokens = (
          .testTokens as $existing
          | $existing
          + (
              reduce ($d.tokens[]?) as $t ({};
                .[$t.symbol] = {
                  name: $t.name,
                  symbol: $t.symbol,
                  address: $t.address,
                  decimals: ($t.decimals // ($existing[$t.symbol].decimals // null)),
                  totalSupply: ($existing[$t.symbol].totalSupply // "")
                }
              )
            )
        )
      | .pools |= with_entries(
          .value.token0 = (token_addr(.value.token0Symbol) // .value.token0)
          | .value.token1 = (token_addr(.value.token1Symbol) // .value.token1)
        )
    ' "$TEST_ADDRESSES_PATH" >"$tmp"

  mv "$tmp" "$TEST_ADDRESSES_PATH"
  log_ok "Updated $TEST_ADDRESSES_PATH"
}

step_create_all_wpc_pools() {
  require_cmd node cast "$PUSH_CHAIN_DIR/build/pchaind"
  ensure_deploy_file

  [[ -n "${PRIVATE_KEY:-}" ]] || { log_err "Set PRIVATE_KEY in e2e-tests/.env"; exit 1; }

  if [[ ! -f "$TEST_ADDRESSES_PATH" ]]; then
    log_err "Missing test-addresses.json at $TEST_ADDRESSES_PATH"
    exit 1
  fi

  local wpc_addr token_count token_addr token_symbol
  wpc_addr="$(address_from_deploy_contract "WPC")"
  if [[ -z "$wpc_addr" ]]; then
    log_err "Missing WPC contract address in $DEPLOY_ADDRESSES_FILE"
    exit 1
  fi

  token_count="$(jq -r '.tokens | length' "$DEPLOY_ADDRESSES_FILE")"
  if [[ "$token_count" == "0" ]]; then
    log_warn "No core tokens found in deploy addresses; skipping pool creation"
    return 0
  fi

  local deployer_evm_addr
  deployer_evm_addr="$(cast wallet address --private-key "$PRIVATE_KEY" 2>/dev/null || true)"
  if ! validate_eth_address "$deployer_evm_addr"; then
    log_err "Could not resolve deployer EVM address from PRIVATE_KEY"
    exit 1
  fi

  local deployer_hex deployer_push_addr
  deployer_hex="$(echo "$deployer_evm_addr" | tr '[:upper:]' '[:lower:]' | sed 's/^0x//')"
  deployer_push_addr="$("$PUSH_CHAIN_DIR/build/pchaind" debug addr "$deployer_hex" 2>/dev/null | awk -F': ' '/Bech32 Acc:/ {print $2; exit}')"
  if [[ -z "$deployer_push_addr" ]]; then
    log_err "Could not derive bech32 deployer address from $deployer_evm_addr"
    exit 1
  fi

  log_info "Funding deployer $deployer_push_addr ($deployer_evm_addr) for pool creation ($POOL_CREATION_TOPUP_AMOUNT)"
  local fund_attempt=1
  local fund_max_attempts=5
  local fund_out=""
  while true; do
    fund_out="$("$PUSH_CHAIN_DIR/build/pchaind" tx bank send "$GENESIS_KEY_NAME" "$deployer_push_addr" "$POOL_CREATION_TOPUP_AMOUNT" \
      --gas-prices "$GAS_PRICES" \
      --keyring-backend "$KEYRING_BACKEND" \
      --chain-id "$CHAIN_ID" \
      --home "$GENESIS_KEY_HOME" \
      -y 2>&1 || true)"

    if echo "$fund_out" | grep -q 'txhash:' || echo "$fund_out" | grep -q '"txhash"'; then
      log_ok "Deployer funding transaction submitted"
      break
    fi

    if echo "$fund_out" | grep -qi 'account sequence mismatch' && [[ "$fund_attempt" -lt "$fund_max_attempts" ]]; then
      log_warn "Funding sequence mismatch on attempt $fund_attempt/$fund_max_attempts. Retrying..."
      fund_attempt=$((fund_attempt + 1))
      sleep 2
      continue
    fi

    log_err "Failed to fund deployer for pool creation"
    echo "$fund_out"
    exit 1
  done
  sleep 2

  while IFS=$'\t' read -r token_symbol token_addr; do
    [[ -n "$token_addr" ]] || continue
    if [[ "$(echo "$token_addr" | tr '[:upper:]' '[:lower:]')" == "$(echo "$wpc_addr" | tr '[:upper:]' '[:lower:]')" ]]; then
      continue
    fi

    log_info "Creating ${token_symbol}/WPC pool with liquidity"
    (
      cd "$SWAP_AMM_DIR"
      node scripts/pool-manager.js create-pool "$token_addr" "$wpc_addr" 4 500 true 1 4
    )
  done < <(jq -r '.tokens[]? | [.symbol, .address] | @tsv' "$DEPLOY_ADDRESSES_FILE")

  log_ok "All token/WPC pool creation commands completed"
}

step_configure_universal_core() {
  require_cmd forge
  [[ -n "${PRIVATE_KEY:-}" ]] || { log_err "Set PRIVATE_KEY in e2e-tests/.env"; exit 1; }

  # configureUniversalCore depends on values from core .env
  step_write_core_env

  local script_path="scripts/localSetup/configureUniversalCore.s.sol"
  local log_file="$LOG_DIR/core_configure_$(date +%Y%m%d_%H%M%S).log"
  local resume_attempt=1
  local resume_max_attempts="${CORE_CONFIGURE_RESUME_MAX_ATTEMPTS:-0}"  # 0 = unlimited

  if [[ ! -f "$CORE_CONTRACTS_DIR/$script_path" ]]; then
    log_warn "configureUniversalCore script not found at $CORE_CONTRACTS_DIR/$script_path; skipping"
    return 0
  fi

  log_info "Running configureUniversalCore script"
  if (
    cd "$CORE_CONTRACTS_DIR"
    forge script "$script_path" \
      --broadcast \
      --rpc-url "$PUSH_RPC_URL" \
      --private-key "$PRIVATE_KEY" \
      --slow
  ) 2>&1 | tee "$log_file"; then
    log_ok "configureUniversalCore completed"
    return 0
  fi

  log_warn "configureUniversalCore failed. Retrying with --resume until success"
  while true; do
    log_info "configureUniversalCore resume attempt: $resume_attempt"
    if (
      cd "$CORE_CONTRACTS_DIR"
      forge script "$script_path" \
        --broadcast \
        --rpc-url "$PUSH_RPC_URL" \
        --private-key "$PRIVATE_KEY" \
        --slow \
        --resume
    ) 2>&1 | tee -a "$log_file"; then
      log_ok "configureUniversalCore resumed successfully"
      return 0
    fi

    if [[ "$resume_max_attempts" != "0" && "$resume_attempt" -ge "$resume_max_attempts" ]]; then
      log_err "Reached CORE_CONFIGURE_RESUME_MAX_ATTEMPTS=$resume_max_attempts without success"
      exit 1
    fi

    resume_attempt=$((resume_attempt + 1))
    sleep 2
  done
}

cmd_all() {
  if is_local_testing_env; then
    step_setup_environment
  fi
  (cd "$PUSH_CHAIN_DIR" && make replace-addresses)
  (cd "$PUSH_CHAIN_DIR" && make build)
  step_update_env_fund_to_address
  step_stop_running_nodes
  step_devnet
  if is_local_testing_env; then
    step_setup_environment
  fi
  step_recover_genesis_key
  step_fund_account
  step_setup_core_contracts
  step_setup_swap_amm
  step_sync_test_addresses
  step_create_all_wpc_pools
  assert_required_addresses
  step_write_core_env
  step_configure_universal_core
  step_update_eth_token_config
  step_setup_gateway
  step_add_uregistry_configs
}

cmd_show_help() {
  cat <<EOF
Usage: $(basename "$0") <command>

Commands:
  setup-environment      For TESTING_ENV=LOCAL: start anvil/surfpool + patch validator and testnet-donut chain RPC configs
  devnet                 Build/start local-multi-validator devnet + uvalidators
  print-genesis          Print first genesis account + mnemonic
  recover-genesis-key    Recover genesis key into local keyring
  fund                   Fund FUND_TO_ADDRESS from genesis key
  setup-core             Clone/build/setup core contracts (auto resume on failure)
  setup-swap             Clone/install/deploy swap AMM contracts
  sync-addresses         Apply deploy_addresses.json into test-addresses.json
  create-pool            Create WPC pools for all deployed core tokens
  configure-core         Run configureUniversalCore.s.sol (auto --resume retries)
  check-addresses        Check/report deploy addresses (WPC/Factory/QuoterV2/SwapRouter)
  write-core-env         Create core-contracts .env from deploy_addresses.json
  update-token-config    Update eth_sepolia_eth.json contract_address using deployed token
  setup-gateway          Clone/setup gateway repo and run forge localSetup (with --resume retry)
  setup-sdk              Clone/setup push-chain-sdk, generate SDK .env from e2e .env, and install dependencies
  sdk-test-all           Replace PUSH_NETWORK TESTNET variants with LOCALNET and run all configured SDK E2E tests
  sdk-test-pctx-last-transaction  Run pctx-last-transaction.spec.ts
  sdk-test-send-to-self  Run send-to-self.spec.ts
  sdk-test-progress-hook Run progress-hook-per-tx.spec.ts
  sdk-test-bridge-multicall  Run bridge-multicall.spec.ts
  sdk-test-pushchain     Run pushchain.spec.ts
  sdk-test-bridge-hooks  Run bridge-hooks.spec.ts
  add-uregistry-configs  Submit chain + token config txs via local-multi-validator validator1
  record-contract K A    Manually record contract key/address
  record-token N S A     Manually record token name/symbol/address
  all                    Run full setup pipeline
  help                   Show this help

Primary files:
  Env:     $ENV_FILE
  Address: $DEPLOY_ADDRESSES_FILE

Important env:
  TESTING_ENV=LOCAL      Enables local anvil setup and config rewrites for testnet-donut chain.json and universal validator RPCs in setup-environment/all
  ANVIL_SEPOLIA_HOST_RPC_URL=http://localhost:9545
  ANVIL_ARBITRUM_HOST_RPC_URL=http://localhost:9546
  ANVIL_BASE_HOST_RPC_URL=http://localhost:9547
  ANVIL_BSC_HOST_RPC_URL=http://localhost:9548
  LOCAL_SEPOLIA_UV_RPC_URL=http://host.docker.internal:9545
  LOCAL_ARBITRUM_UV_RPC_URL=http://host.docker.internal:9546
  LOCAL_BASE_UV_RPC_URL=http://host.docker.internal:9547
  LOCAL_BSC_UV_RPC_URL=http://host.docker.internal:9548
  SURFPOOL_SOLANA_HOST_RPC_URL=http://localhost:8899
  LOCAL_SOLANA_UV_RPC_URL=http://host.docker.internal:8899
EOF
}

main() {
  ensure_testing_env_var_in_env_file

  local cmd="${1:-help}"
  case "$cmd" in
    setup-environment) step_setup_environment ;;
    devnet) step_devnet ;;
    print-genesis) step_print_genesis ;;
    recover-genesis-key) step_recover_genesis_key ;;
    fund) step_fund_account ;;
    setup-core) step_setup_core_contracts ;;
    setup-swap) step_setup_swap_amm ;;
    sync-addresses) step_sync_test_addresses ;;
    create-pool) step_create_all_wpc_pools ;;
    configure-core) step_configure_universal_core ;;
    check-addresses) assert_required_addresses ;;
    write-core-env) step_write_core_env ;;
    update-token-config) step_update_deployed_token_configs ;;
    setup-gateway) step_setup_gateway ;;
    setup-sdk) step_setup_push_chain_sdk ;;
    sdk-test-all) step_run_sdk_tests_all ;;
    sdk-test-pctx-last-transaction) step_run_sdk_test_file "pctx-last-transaction.spec.ts" ;;
    sdk-test-send-to-self) step_run_sdk_test_file "send-to-self.spec.ts" ;;
    sdk-test-progress-hook) step_run_sdk_test_file "progress-hook-per-tx.spec.ts" ;;
    sdk-test-bridge-multicall) step_run_sdk_test_file "bridge-multicall.spec.ts" ;;
    sdk-test-pushchain) step_run_sdk_test_file "pushchain.spec.ts" ;;
    sdk-test-bridge-hooks) step_run_sdk_test_file "bridge-hooks.spec.ts" ;;
    add-uregistry-configs) step_add_uregistry_configs ;;
    record-contract)
      ensure_deploy_file
      [[ $# -eq 3 ]] || { log_err "Usage: $0 record-contract <KEY> <ADDRESS>"; exit 1; }
      validate_eth_address "$3" || { log_err "Invalid address: $3"; exit 1; }
      record_contract "$2" "$3"
      ;;
    record-token)
      ensure_deploy_file
      [[ $# -eq 4 ]] || { log_err "Usage: $0 record-token <NAME> <SYMBOL> <ADDRESS>"; exit 1; }
      validate_eth_address "$4" || { log_err "Invalid address: $4"; exit 1; }
      record_token "$2" "$3" "$4" "manual"
      ;;
    all) cmd_all ;;
    help|--help|-h) cmd_show_help ;;
    *) log_err "Unknown command: $cmd"; cmd_show_help; exit 1 ;;
  esac
}

main "$@"