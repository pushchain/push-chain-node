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
: "${LOCAL_DEVNET_DIR:=./local-native}"

: "${CORE_CONTRACTS_REPO:=https://github.com/pushchain/push-chain-core-contracts.git}"
: "${CORE_CONTRACTS_BRANCH:=e2e-push-node}"
: "${SWAP_AMM_REPO:=https://github.com/pushchain/push-chain-swap-internal-amm-contracts.git}"
: "${SWAP_AMM_BRANCH:=e2e-push-node}"
: "${GATEWAY_REPO:=https://github.com/pushchain/push-chain-gateway-contracts.git}"
: "${GATEWAY_BRANCH:=e2e-push-node}"
: "${PUSH_CHAIN_SDK_REPO:=https://github.com/pushchain/push-chain-sdk.git}"
: "${PUSH_CHAIN_SDK_BRANCH:=outbound_changes}"
: "${PREFER_SIBLING_REPO_DIRS:=true}"

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

normalize_path() {
  local path="$1"
  if [[ -d "$path" ]]; then
    (cd -P "$path" && pwd)
    return
  fi

  local parent base
  parent="$(dirname "$path")"
  base="$(basename "$path")"

  if [[ -d "$parent" ]]; then
    printf "%s/%s" "$(cd -P "$parent" && pwd)" "$base"
  else
    printf "%s" "$path"
  fi
}

prefer_sibling_repo_dirs() {
  if [[ "$(echo "$PREFER_SIBLING_REPO_DIRS" | tr '[:upper:]' '[:lower:]')" != "true" ]]; then
    CORE_CONTRACTS_DIR="$(normalize_path "$CORE_CONTRACTS_DIR")"
    GATEWAY_DIR="$(normalize_path "$GATEWAY_DIR")"
    return
  fi

  local sibling_core sibling_gateway
  sibling_core="$(normalize_path "$PUSH_CHAIN_DIR/../push-chain-core-contracts")"
  sibling_gateway="$(normalize_path "$PUSH_CHAIN_DIR/../push-chain-gateway-contracts")"

  CORE_CONTRACTS_DIR="$(normalize_path "$CORE_CONTRACTS_DIR")"
  GATEWAY_DIR="$(normalize_path "$GATEWAY_DIR")"

  if [[ -d "$sibling_core" ]]; then
    CORE_CONTRACTS_DIR="$sibling_core"
  fi

  if [[ -d "$sibling_gateway" ]]; then
    GATEWAY_DIR="$sibling_gateway"
  fi
}

prefer_sibling_repo_dirs

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

sdk_outbound_test_files() {
  local outbound_dir="$PUSH_CHAIN_SDK_DIR/packages/core/__e2e__/evm/outbound"
  local file
  local requested_files=(
    "cea-to-eoa.spec.ts"
  )

  for file in "${requested_files[@]}"; do
    if [[ -f "$outbound_dir/$file" ]]; then
      printf "%s\n" "$outbound_dir/$file"
    else
      log_err "SDK outbound test file not found: $outbound_dir/$file"
      exit 1
    fi
  done
}

sdk_rewrite_chain_endpoints_for_local() {
  local chain_constants_file="$1"

  CHAIN_CONSTANTS_FILE="$chain_constants_file" node <<'NODE'
const fs = require('fs');

const filePath = process.env.CHAIN_CONSTANTS_FILE;
if (!filePath || !fs.existsSync(filePath)) {
  console.error('chain.ts file not found for LOCAL endpoint rewrite');
  process.exit(1);
}

let source = fs.readFileSync(filePath, 'utf8');

const endpointMap = [
  { chain: 'ETHEREUM_SEPOLIA', url: 'http://localhost:9545' },
  { chain: 'ARBITRUM_SEPOLIA', url: 'http://localhost:9546' },
  { chain: 'BASE_SEPOLIA', url: 'http://localhost:9547' },
  { chain: 'BNB_TESTNET', url: 'http://localhost:9548' },
  { chain: 'SOLANA_DEVNET', url: 'http://localhost:8899' },
];

function findChainBlockRange(text, chainName) {
  const marker = `[CHAIN.${chainName}]`;
  const markerIdx = text.indexOf(marker);
  if (markerIdx === -1) {
    return null;
  }

  const openBraceIdx = text.indexOf('{', markerIdx);
  if (openBraceIdx === -1) {
    return null;
  }

  let depth = 0;
  for (let i = openBraceIdx; i < text.length; i += 1) {
    const ch = text[i];
    if (ch === '{') {
      depth += 1;
    } else if (ch === '}') {
      depth -= 1;
      if (depth === 0) {
        return { start: openBraceIdx, end: i };
      }
    }
  }

  return null;
}

function detectIndent(blockText) {
  const match = blockText.match(/\n(\s+)[A-Za-z_\[]/);
  return match ? match[1] : '    ';
}

function findMatchingBracket(text, openIdx) {
  let depth = 0;
  let quote = '';

  for (let i = openIdx; i < text.length; i += 1) {
    const ch = text[i];
    const prev = i > 0 ? text[i - 1] : '';

    if (quote) {
      if (ch === quote && prev !== '\\') {
        quote = '';
      }
      continue;
    }

    if (ch === '\'' || ch === '"' || ch === '`') {
      quote = ch;
      continue;
    }

    if (ch === '[') {
      depth += 1;
      continue;
    }

    if (ch === ']') {
      depth -= 1;
      if (depth === 0) {
        return i;
      }
    }
  }

  return -1;
}

function upsertDefaultRpc(blockText, rpcUrl, indent) {
  const keyRegex = /\bdefaultRPC\s*:/m;
  const keyMatch = keyRegex.exec(blockText);
  if (keyMatch) {
    const arrayStart = blockText.indexOf('[', keyMatch.index);
    if (arrayStart !== -1) {
      const arrayEnd = findMatchingBracket(blockText, arrayStart);
      if (arrayEnd !== -1) {
        return {
          text: `${blockText.slice(0, arrayStart)}['${rpcUrl}']${blockText.slice(arrayEnd + 1)}`,
          changed: true,
        };
      }
    }

    return {
      text: blockText.replace(/(defaultRPC\s*:\s*)[^\n,]+/, `$1['${rpcUrl}']`),
      changed: true,
    };
  }

  return {
    text: blockText.replace(/\{\s*/, `{\n${indent}defaultRPC: ['${rpcUrl}'],\n`),
    changed: true,
  };
}

function upsertExplorerUrl(blockText, explorerUrl, indent) {
  const explorerRegex = /((explorerURL|explorerUrl)\s*:\s*)['"`][^'"`\n]*['"`]/m;
  if (explorerRegex.test(blockText)) {
    return {
      text: blockText.replace(explorerRegex, `$1'${explorerUrl}'`),
      changed: true,
    };
  }

  const defaultRpcLineRegex = /(defaultRPC\s*:\s*\[[\s\S]*?\]\s*,?)/m;
  if (defaultRpcLineRegex.test(blockText)) {
    return {
      text: blockText.replace(defaultRpcLineRegex, `$1\n${indent}explorerUrl: '${explorerUrl}',`),
      changed: true,
    };
  }

  return {
    text: blockText.replace(/\{\s*/, `{\n${indent}explorerUrl: '${explorerUrl}',\n`),
    changed: true,
  };
}

const edits = [];
for (const entry of endpointMap) {
  const range = findChainBlockRange(source, entry.chain);
  if (!range) {
    console.error(`Could not find chain block for CHAIN.${entry.chain} in ${filePath}`);
    process.exit(1);
  }

  const originalBlock = source.slice(range.start, range.end + 1);
  const indent = detectIndent(originalBlock);

  const defaultRpcResult = upsertDefaultRpc(originalBlock, entry.url, indent);
  const explorerResult = upsertExplorerUrl(defaultRpcResult.text, entry.url, indent);

  edits.push({
    start: range.start,
    end: range.end,
    text: explorerResult.text,
  });
}

edits.sort((a, b) => b.start - a.start);
for (const edit of edits) {
  source = source.slice(0, edit.start) + edit.text + source.slice(edit.end + 1);
}

fs.writeFileSync(filePath, source);
NODE
}

sdk_sync_localnet_constants() {
  require_cmd jq perl node

  local chain_constants_file="$PUSH_CHAIN_SDK_DIR/$PUSH_CHAIN_SDK_CHAIN_CONSTANTS_PATH"
  local sdk_utils_file="$PUSH_CHAIN_SDK_DIR/packages/core/src/lib/utils.ts"
  local orchestrator_file="$PUSH_CHAIN_SDK_DIR/packages/core/src/lib/orchestrator/orchestrator.ts"

  if [[ ! -f "$chain_constants_file" ]]; then
    log_err "SDK chain constants file not found: $chain_constants_file"
    exit 1
  fi

  ensure_deploy_file

  local peth peth_arb peth_base pbnb psol usdt_eth usdt_bnb
  peth="$(address_from_deploy_token "pETH")"
  peth_arb="$(address_from_deploy_token "pETH.arb")"
  peth_base="$(address_from_deploy_token "pETH.base")"
  pbnb="$(address_from_deploy_token "pBNB")"
  psol="$(address_from_deploy_token "pSOL")"
  usdt_eth="$(address_from_deploy_token "USDT.eth")"
  usdt_bnb="$(address_from_deploy_token "USDT.bsc")"

  [[ -n "$peth" ]] || peth="0xTBD"
  [[ -n "$peth_arb" ]] || peth_arb="0xTBD"
  [[ -n "$peth_base" ]] || peth_base="0xTBD"
  [[ -n "$pbnb" ]] || pbnb="0xTBD"
  [[ -n "$psol" ]] || psol="0xTBD"
  [[ -n "$usdt_eth" ]] || usdt_eth="0xTBD"
  [[ -n "$usdt_bnb" ]] || usdt_bnb="$usdt_eth"

  PETH_ADDR="$peth" \
  PETH_ARB_ADDR="$peth_arb" \
  PETH_BASE_ADDR="$peth_base" \
  PBNB_ADDR="$pbnb" \
  PSOL_ADDR="$psol" \
  USDT_ETH_ADDR="$usdt_eth" \
  USDT_BNB_ADDR="$usdt_bnb" \
  perl -0pi -e '
    s#(\[PUSH_NETWORK\.LOCALNET\]:\s*\{[\s\S]*?pETH:\s*)'\''[^'\''\n]*'\''#$1'\''$ENV{PETH_ADDR}'\''#s;
    s#(\[PUSH_NETWORK\.LOCALNET\]:\s*\{[\s\S]*?pETH_ARB:\s*)'\''[^'\''\n]*'\''#$1'\''$ENV{PETH_ARB_ADDR}'\''#s;
    s#(\[PUSH_NETWORK\.LOCALNET\]:\s*\{[\s\S]*?pETH_BASE:\s*)'\''[^'\''\n]*'\''#$1'\''$ENV{PETH_BASE_ADDR}'\''#s;
    s#(\[PUSH_NETWORK\.LOCALNET\]:\s*\{[\s\S]*?pETH_BNB:\s*)'\''[^'\''\n]*'\''#$1'\''$ENV{PBNB_ADDR}'\''#s;
    s#(\[PUSH_NETWORK\.LOCALNET\]:\s*\{[\s\S]*?pSOL:\s*)'\''[^'\''\n]*'\''#$1'\''$ENV{PSOL_ADDR}'\''#s;
    s#(\[PUSH_NETWORK\.LOCALNET\]:\s*\{[\s\S]*?USDT_ETH:\s*)'\''[^'\''\n]*'\''#$1'\''$ENV{USDT_ETH_ADDR}'\''#s;
    s#(\[PUSH_NETWORK\.LOCALNET\]:\s*\{[\s\S]*?USDT_BNB:\s*)'\''[^'\''\n]*'\''#$1'\''$ENV{USDT_BNB_ADDR}'\''#s;
  ' "$chain_constants_file"

  if [[ -f "$orchestrator_file" ]]; then
    perl -0pi -e "s/return '\\Q0x00000000000000000000000000000000000000C0\\E';/return '0x00000000000000000000000000000000000000C1';/g" "$orchestrator_file"
  fi

  # For LOCAL testing only, force selected chain endpoints to localhost RPC/explorer URLs.
  if is_local_testing_env; then
    sdk_rewrite_chain_endpoints_for_local "$chain_constants_file"
    log_ok "Patched SDK chain.ts RPC/explorer endpoints for LOCAL testing"
  fi

  if [[ -f "$sdk_utils_file" ]]; then
    perl -0pi -e "s/\[PUSH_NETWORK\\.LOCALNET\]:\s*\[\s*CHAIN\\.PUSH_TESTNET_DONUT,/\[PUSH_NETWORK.LOCALNET\]: [CHAIN.PUSH_LOCALNET,/g" "$sdk_utils_file"
  fi

  log_ok "Synced SDK LOCALNET synthetic token constants from deploy addresses"
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

  while IFS= read -r outbound_file; do
    [[ -n "$outbound_file" ]] || continue
    perl -0pi -e 's/\bPUSH_NETWORK\.TESTNET_DONUT\b/PUSH_NETWORK.LOCALNET/g; s/\bPUSH_NETWORK\.TESTNET\b/PUSH_NETWORK.LOCALNET/g; s/\bCHAIN\.PUSH_TESTNET_DONUT\b/CHAIN.PUSH_LOCALNET/g' "$outbound_file"
    log_ok "Prepared LOCALNET network replacement in $(basename "$outbound_file")"
  done < <(find "$PUSH_CHAIN_SDK_DIR/packages/core/__e2e__/evm/outbound" -type f -name '*.spec.ts' | sort)
}

step_clone_push_chain_sdk() {
  require_cmd git
  clone_or_update_repo "$PUSH_CHAIN_SDK_REPO" "$PUSH_CHAIN_SDK_BRANCH" "$PUSH_CHAIN_SDK_DIR"
  log_ok "push-chain-sdk ready at $PUSH_CHAIN_SDK_DIR"
}

step_setup_push_chain_sdk() {
  require_cmd git yarn npm cast jq perl

  local chain_constants_file="$PUSH_CHAIN_SDK_DIR/$PUSH_CHAIN_SDK_CHAIN_CONSTANTS_PATH"
  local sdk_account_file="$PUSH_CHAIN_SDK_DIR/$PUSH_CHAIN_SDK_ACCOUNT_TS_PATH"
  local uea_impl_raw uea_impl synced_localnet_uea

  if [[ ! -d "$PUSH_CHAIN_SDK_DIR/.git" ]]; then
    log_err "SDK repo not found at $PUSH_CHAIN_SDK_DIR"
    log_err "Run: $0 clone-sdk (or 'setup all' which clones it automatically)"
    exit 1
  fi

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
    [[ -n "${E2E_TARGET_CHAINS:-}" ]] && echo "E2E_TARGET_CHAINS=${E2E_TARGET_CHAINS}"
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

  sdk_sync_localnet_constants

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

  local sdk_e2e_root="$PUSH_CHAIN_SDK_DIR/packages/core/__e2e__"
  if [[ -d "$sdk_e2e_root" ]]; then
    log_info "Replacing TESTNET/TESTNET_DONUT with LOCALNET across all SDK __e2e__ test files"
    local patched_count=0
    while IFS= read -r -d '' e2e_file; do
      perl -0pi -e '
        s/\bPUSH_NETWORK\.TESTNET_DONUT\b/PUSH_NETWORK.LOCALNET/g;
        s/\bPUSH_NETWORK\.TESTNET\b/PUSH_NETWORK.LOCALNET/g;
        s/\bCHAIN\.PUSH_TESTNET_DONUT\b/CHAIN.PUSH_LOCALNET/g;
      ' "$e2e_file"
      patched_count=$((patched_count + 1))
    done < <(find "$sdk_e2e_root" -type f \( -name '*.ts' -o -name '*.tsx' \) -print0)
    log_ok "Applied LOCALNET replacement to $patched_count file(s) under $sdk_e2e_root"
  else
    log_warn "SDK __e2e__ directory not found at $sdk_e2e_root; skipping TESTNET→LOCALNET replacement"
  fi

  log_info "Installing push-chain-sdk dependencies"
  (
    cd "$PUSH_CHAIN_SDK_DIR"
    yarn install || true
    npm install
    npm i --save-dev @types/bs58
    npm i tweetnacl
  )

  log_ok "push-chain-sdk setup complete"
}

step_run_sdk_test_file() {
  local test_basename="$1"
  local test_file=""

  # Search inbound test files first
  while IFS= read -r candidate; do
    [[ -n "$candidate" ]] || continue
    if [[ "$(basename "$candidate")" == "$test_basename" ]]; then
      test_file="$candidate"
      break
    fi
  done < <(sdk_test_files)

  if [[ -n "$test_file" ]]; then
    # Inbound file — use full prepare (TESTNET→LOCALNET for all inbound files)
    sdk_prepare_test_files_for_localnet
  else
    # Search outbound test files
    while IFS= read -r candidate; do
      [[ -n "$candidate" ]] || continue
      if [[ "$(basename "$candidate")" == "$test_basename" ]]; then
        test_file="$candidate"
        break
      fi
    done < <(sdk_outbound_test_files)

    if [[ -n "$test_file" ]]; then
      # Outbound file — sync localnet constants and apply TESTNET→LOCALNET to outbound files only
      sdk_sync_localnet_constants
      perl -0pi -e 's/\bPUSH_NETWORK\.TESTNET_DONUT\b/PUSH_NETWORK.LOCALNET/g; s/\bPUSH_NETWORK\.TESTNET\b/PUSH_NETWORK.LOCALNET/g; s/\bCHAIN\.PUSH_TESTNET_DONUT\b/CHAIN.PUSH_LOCALNET/g' "$test_file"
      log_ok "Prepared LOCALNET network replacement in $test_basename"
      # Also patch shared evm-client.ts default network
      local evm_client_file="$PUSH_CHAIN_SDK_DIR/packages/core/__e2e__/shared/evm-client.ts"
      if [[ -f "$evm_client_file" ]]; then
        perl -0pi -e 's/\bPUSH_NETWORK\.TESTNET_DONUT\b/PUSH_NETWORK.LOCALNET/g' "$evm_client_file"
        log_ok "Patched evm-client.ts default network to PUSH_NETWORK.LOCALNET"
      fi
      # Patch utils.ts: fix TESTNET_DONUT default in getPRC20Address
      local utils_file="$PUSH_CHAIN_SDK_DIR/packages/core/src/lib/utils.ts"
      if [[ -f "$utils_file" ]]; then
        perl -0pi -e 's/(const network = options\?\.network \?\?)\s*PUSH_NETWORK\.TESTNET_DONUT/$1 PUSH_NETWORK.LOCALNET/' "$utils_file"
        log_ok "Patched utils.ts getPRC20Address default network to PUSH_NETWORK.LOCALNET"
      fi
      # Patch tokens.ts: fix TESTNET_DONUT in buildPushChainMoveableTokenAccessor
      local tokens_file="$PUSH_CHAIN_SDK_DIR/packages/core/src/lib/constants/tokens.ts"
      if [[ -f "$tokens_file" ]]; then
        perl -0pi -e 's/(const s = SYNTHETIC_PUSH_ERC20\[)PUSH_NETWORK\.TESTNET_DONUT(\])/$1PUSH_NETWORK.LOCALNET$2/' "$tokens_file"
        log_ok "Patched tokens.ts buildPushChainMoveableTokenAccessor default network to PUSH_NETWORK.LOCALNET"
      fi
    fi
  fi

  if [[ -z "$test_file" ]]; then
    log_err "Requested SDK test file not in configured list: $test_basename"
    exit 1
  fi

  log_info "Running SDK test: $test_basename"
  local rel_pattern="${test_file##*/packages/core/}"
  (
    cd "$PUSH_CHAIN_SDK_DIR"
    npx nx test core --runInBand --testPathPattern="$rel_pattern"
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

step_run_sdk_outbound_tests_all() {
  local test_file
  local evm_client_file="$PUSH_CHAIN_SDK_DIR/packages/core/__e2e__/shared/evm-client.ts"

  # Sync localnet constants (rewrites chain.ts defaultRPC for LOCAL mode) and
  # apply TESTNET_DONUT → LOCALNET replacement in outbound spec files.
  sdk_sync_localnet_constants

  while IFS= read -r outbound_file; do
    [[ -n "$outbound_file" ]] || continue
    perl -0pi -e 's/\bPUSH_NETWORK\.TESTNET_DONUT\b/PUSH_NETWORK.LOCALNET/g; s/\bPUSH_NETWORK\.TESTNET\b/PUSH_NETWORK.LOCALNET/g; s/\bCHAIN\.PUSH_TESTNET_DONUT\b/CHAIN.PUSH_LOCALNET/g' "$outbound_file"
    log_ok "Prepared LOCALNET network replacement in $(basename "$outbound_file")"
  done < <(find "$PUSH_CHAIN_SDK_DIR/packages/core/__e2e__/evm/outbound" -type f -name '*.spec.ts' | sort)

  # Also patch shared evm-client.ts default network so PushChain.initialize uses LOCALNET
  if [[ -f "$evm_client_file" ]]; then
    perl -0pi -e 's/\bPUSH_NETWORK\.TESTNET_DONUT\b/PUSH_NETWORK.LOCALNET/g' "$evm_client_file"
    log_ok "Patched evm-client.ts default network to PUSH_NETWORK.LOCALNET"
  fi

  # Patch utils.ts: fix TESTNET_DONUT default in getPRC20Address (used for PRC20 token lookup)
  local utils_file="$PUSH_CHAIN_SDK_DIR/packages/core/src/lib/utils.ts"
  if [[ -f "$utils_file" ]]; then
    perl -0pi -e 's/(const network = options\?\.network \?\?)\s*PUSH_NETWORK\.TESTNET_DONUT/$1 PUSH_NETWORK.LOCALNET/' "$utils_file"
    log_ok "Patched utils.ts getPRC20Address default network to PUSH_NETWORK.LOCALNET"
  fi

  # Patch tokens.ts: fix TESTNET_DONUT in buildPushChainMoveableTokenAccessor
  local tokens_file="$PUSH_CHAIN_SDK_DIR/packages/core/src/lib/constants/tokens.ts"
  if [[ -f "$tokens_file" ]]; then
    perl -0pi -e 's/(const s = SYNTHETIC_PUSH_ERC20\[)PUSH_NETWORK\.TESTNET_DONUT(\])/$1PUSH_NETWORK.LOCALNET$2/' "$tokens_file"
    log_ok "Patched tokens.ts buildPushChainMoveableTokenAccessor default network to PUSH_NETWORK.LOCALNET"
  fi

  while IFS= read -r test_file; do
    [[ -n "$test_file" ]] || continue
    log_info "Running SDK outbound test: $(basename "$test_file")"
    # Strip everything up to and including "packages/core/" to get a relative path
    # that Jest can match against canonical absolute paths (avoids ".." in the pattern)
    local rel_pattern="${test_file##*/packages/core/}"
    (
      cd "$PUSH_CHAIN_SDK_DIR"
      npx nx test core --runInBand --testPathPattern="$rel_pattern"
    )
  done < <(sdk_outbound_test_files)

  log_ok "Completed all configured SDK outbound E2E tests"
}

step_run_sdk_quick_testing_outbound() {
  local outbound_dir="$PUSH_CHAIN_SDK_DIR/packages/core/__e2e__/evm/outbound"
  local quick_files=(
    "cea-to-eoa.spec.ts"
    "cea-to-uea.spec.ts"
  )
  local evm_client_file="$PUSH_CHAIN_SDK_DIR/packages/core/__e2e__/shared/evm-client.ts"
  local utils_file="$PUSH_CHAIN_SDK_DIR/packages/core/src/lib/utils.ts"
  local tokens_file="$PUSH_CHAIN_SDK_DIR/packages/core/src/lib/constants/tokens.ts"
  local file full_path

  step_setup_push_chain_sdk
  step_fund_uea_prc20

  sdk_sync_localnet_constants

  for file in "${quick_files[@]}"; do
    full_path="$outbound_dir/$file"
    if [[ ! -f "$full_path" ]]; then
      log_err "SDK outbound test file not found: $full_path"
      exit 1
    fi
    perl -0pi -e 's/\bPUSH_NETWORK\.TESTNET_DONUT\b/PUSH_NETWORK.LOCALNET/g; s/\bPUSH_NETWORK\.TESTNET\b/PUSH_NETWORK.LOCALNET/g; s/\bCHAIN\.PUSH_TESTNET_DONUT\b/CHAIN.PUSH_LOCALNET/g' "$full_path"
    log_ok "Prepared LOCALNET network replacement in $file"
  done

  if [[ -f "$evm_client_file" ]]; then
    perl -0pi -e 's/\bPUSH_NETWORK\.TESTNET_DONUT\b/PUSH_NETWORK.LOCALNET/g' "$evm_client_file"
    log_ok "Patched evm-client.ts default network to PUSH_NETWORK.LOCALNET"
  fi
  if [[ -f "$utils_file" ]]; then
    perl -0pi -e 's/(const network = options\?\.network \?\?)\s*PUSH_NETWORK\.TESTNET_DONUT/$1 PUSH_NETWORK.LOCALNET/' "$utils_file"
    log_ok "Patched utils.ts getPRC20Address default network to PUSH_NETWORK.LOCALNET"
  fi
  if [[ -f "$tokens_file" ]]; then
    perl -0pi -e 's/(const s = SYNTHETIC_PUSH_ERC20\[)PUSH_NETWORK\.TESTNET_DONUT(\])/$1PUSH_NETWORK.LOCALNET$2/' "$tokens_file"
    log_ok "Patched tokens.ts buildPushChainMoveableTokenAccessor default network to PUSH_NETWORK.LOCALNET"
  fi

  for file in "${quick_files[@]}"; do
    full_path="$outbound_dir/$file"
    log_info "Running SDK outbound test: $file"
    local rel_pattern="${full_path##*/packages/core/}"
    (
      cd "$PUSH_CHAIN_SDK_DIR"
      npx nx test core --runInBand --testPathPattern="$rel_pattern"
    )
  done

  log_ok "Completed quick-testing-outbound SDK E2E tests"
}

step_devnet() {
  require_cmd bash jq

  local sepolia_rpc_override arbitrum_rpc_override base_rpc_override bsc_rpc_override solana_rpc_override

  chain_public_rpc_from_config() {
    local file_path="$1"
    local fallback_rpc="$2"
    local label="$3"
    local rpc_url

    if [[ ! -f "$file_path" ]]; then
      log_warn "Chain config file not found for $label while preparing devnet RPC overrides: $file_path; using fallback $fallback_rpc"
      printf "%s" "$fallback_rpc"
      return
    fi

    rpc_url="$(jq -r '.public_rpc_url // empty' "$file_path" 2>/dev/null || true)"
    if [[ -z "$rpc_url" || "$rpc_url" == "null" ]]; then
      log_warn "public_rpc_url missing in $file_path while preparing devnet RPC overrides; using fallback $fallback_rpc"
      printf "%s" "$fallback_rpc"
      return
    fi

    printf "%s" "$rpc_url"
  }

  if is_local_testing_env; then
    local local_sepolia_rpc local_arbitrum_rpc local_base_rpc local_bsc_rpc local_solana_rpc
    local_sepolia_rpc="${LOCAL_SEPOLIA_UV_RPC_URL:-${ANVIL_SEPOLIA_HOST_RPC_URL:-http://localhost:9545}}"
    local_arbitrum_rpc="${LOCAL_ARBITRUM_UV_RPC_URL:-${ANVIL_ARBITRUM_HOST_RPC_URL:-http://localhost:9546}}"
    local_base_rpc="${LOCAL_BASE_UV_RPC_URL:-${ANVIL_BASE_HOST_RPC_URL:-http://localhost:9547}}"
    local_bsc_rpc="${LOCAL_BSC_UV_RPC_URL:-${ANVIL_BSC_HOST_RPC_URL:-http://localhost:9548}}"
    local_solana_rpc="${LOCAL_SOLANA_UV_RPC_URL:-${SURFPOOL_SOLANA_HOST_RPC_URL:-http://localhost:8899}}"

    sepolia_rpc_override="$local_sepolia_rpc"
    arbitrum_rpc_override="$local_arbitrum_rpc"
    base_rpc_override="$local_base_rpc"
    bsc_rpc_override="$local_bsc_rpc"
    solana_rpc_override="$local_solana_rpc"
  else
    sepolia_rpc_override="$(chain_public_rpc_from_config "$TOKENS_CONFIG_DIR/eth_sepolia/chain.json" "https://eth-sepolia.public.blastapi.io" "eth_sepolia")"
    arbitrum_rpc_override="$(chain_public_rpc_from_config "$TOKENS_CONFIG_DIR/arb_sepolia/chain.json" "https://arbitrum-sepolia.gateway.tenderly.co" "arb_sepolia")"
    base_rpc_override="$(chain_public_rpc_from_config "$TOKENS_CONFIG_DIR/base_sepolia/chain.json" "https://sepolia.base.org" "base_sepolia")"
    bsc_rpc_override="$(chain_public_rpc_from_config "$TOKENS_CONFIG_DIR/bsc_testnet/chain.json" "https://bsc-testnet-rpc.publicnode.com" "bsc_testnet")"
    solana_rpc_override="$(chain_public_rpc_from_config "$TOKENS_CONFIG_DIR/solana_devnet/chain.json" "https://api.devnet.solana.com" "solana_devnet")"
  fi

  log_info "Devnet RPC overrides: sepolia=$sepolia_rpc_override arbitrum=$arbitrum_rpc_override base=$base_rpc_override bsc=$bsc_rpc_override solana=$solana_rpc_override"

  local devnet_sepolia_start="" devnet_arbitrum_start="" devnet_base_start="" devnet_bsc_start="" devnet_solana_start=""

  if ! is_local_testing_env; then
    require_cmd curl jq
    local _fetch_block
    _fetch_block() {
      local label="$1" rpc_url="$2"
      local response hex_block decimal_block
      response="$(curl -sS --max-time 15 -X POST "$rpc_url" \
        -H 'Content-Type: application/json' \
        --data '{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}' 2>/dev/null || true)"
      hex_block="$(echo "$response" | jq -r '.result // empty' 2>/dev/null || true)"
      if [[ -n "$hex_block" && "$hex_block" != "null" && "$hex_block" =~ ^0x[0-9a-fA-F]+$ ]]; then
        decimal_block="$(printf '%d' "$hex_block" 2>/dev/null || true)"
        [[ "$decimal_block" =~ ^[0-9]+$ ]] && { printf "%s" "$decimal_block"; return 0; }
      fi
      log_warn "Could not read block number for $label from $rpc_url; event_start_from will not be set" >&2
      printf "%s" ""
    }
    _fetch_solana_slot() {
      local rpc_url="$1"
      local slot response
      response="$(curl -sS --max-time 15 -X POST "$rpc_url" -H 'Content-Type: application/json' \
        --data '{"jsonrpc":"2.0","id":1,"method":"getSlot","params":[{"commitment":"processed"}]}' 2>/dev/null || true)"
      slot="$(echo "$response" | jq -r '.result // empty' 2>/dev/null || true)"
      slot="$(echo "$slot" | tr -d '[:space:]')"
      [[ "$slot" =~ ^[0-9]+$ ]] && { printf "%s" "$slot"; return 0; }
      log_warn "Could not read Solana slot from $rpc_url; event_start_from will not be set" >&2
      printf "%s" ""
    }
    log_info "Fetching latest block/slot numbers from public chain RPCs for devnet startup"
    devnet_sepolia_start="$(_fetch_block "sepolia" "$sepolia_rpc_override")"
    devnet_arbitrum_start="$(_fetch_block "arbitrum" "$arbitrum_rpc_override")"
    devnet_base_start="$(_fetch_block "base" "$base_rpc_override")"
    devnet_bsc_start="$(_fetch_block "bsc" "$bsc_rpc_override")"
    devnet_solana_start="$(_fetch_solana_slot "$solana_rpc_override")"
    log_ok "Devnet event_start_from: sepolia=${devnet_sepolia_start:-n/a} arbitrum=${devnet_arbitrum_start:-n/a} base=${devnet_base_start:-n/a} bsc=${devnet_bsc_start:-n/a} solana=${devnet_solana_start:-n/a}"
  fi

  log_info "Starting local devnet"
  (
    cd "$LOCAL_DEVNET_DIR"

    # Start all 4 core validators
    ./devnet start 4

    # Build UV env array with RPC overrides and event_start_from values
    local _uv_env=(
      SEPOLIA_RPC_URL_OVERRIDE="$sepolia_rpc_override"
      ARBITRUM_RPC_URL_OVERRIDE="$arbitrum_rpc_override"
      BASE_RPC_URL_OVERRIDE="$base_rpc_override"
      BSC_RPC_URL_OVERRIDE="$bsc_rpc_override"
      SOLANA_RPC_URL_OVERRIDE="$solana_rpc_override"
    )
    [[ -n "$devnet_sepolia_start" ]]  && _uv_env+=(SEPOLIA_EVENT_START_FROM="$devnet_sepolia_start")
    [[ -n "$devnet_arbitrum_start" ]] && _uv_env+=(ARBITRUM_EVENT_START_FROM="$devnet_arbitrum_start")
    [[ -n "$devnet_base_start" ]]     && _uv_env+=(BASE_EVENT_START_FROM="$devnet_base_start")
    [[ -n "$devnet_bsc_start" ]]      && _uv_env+=(BSC_EVENT_START_FROM="$devnet_bsc_start")
    [[ -n "$devnet_solana_start" ]]   && _uv_env+=(SOLANA_EVENT_START_FROM="$devnet_solana_start")

    # Wait for all core validators to be bonded before registering UVs.
    # setup-validator-auto.sh runs in the background; without this wait,
    # MsgAddUniversalValidator fails with "core validator not found" for
    # validators 2+ because their MsgCreateValidator hasn't landed yet.
    local _wait_elapsed=0
    local _wait_max=180
    local _num_bonded
    log_info "Waiting for core validators to be bonded (needed before UV registration)..."
    while [ "$_wait_elapsed" -lt "$_wait_max" ]; do
      _num_bonded=$(curl -s "http://127.0.0.1:1317/cosmos/staking/v1beta1/validators?status=BOND_STATUS_BONDED" 2>/dev/null \
        | python3 -c "import sys,json; d=json.load(sys.stdin); print(len(d.get('validators',[])))" 2>/dev/null || echo "0")
      if [ "${_num_bonded:-0}" -ge 2 ]; then
        log_ok "Core validators bonded: $_num_bonded — proceeding with UV registration"
        break
      fi
      sleep 5
      _wait_elapsed=$((_wait_elapsed + 5))
      log_info "Core validators bonded so far: ${_num_bonded:-0}/2 (${_wait_elapsed}s elapsed)"
    done

    # Register universal validators on-chain and create authz grants
    env "${_uv_env[@]}" ./devnet setup-uvalidators

    # Start 4 universal validators with RPC overrides and event_start_from
    env "${_uv_env[@]}" ./devnet start-uv 2
  )

  # Sync freshly generated genesis accounts so step_recover_genesis_key uses the current mnemonic.
  # Each fresh devnet run (after `rm -rf data/`) regenerates accounts with new mnemonics.
  if [[ -f "$LOCAL_DEVNET_DIR/data/accounts/genesis_accounts.json" ]]; then
    cp "$LOCAL_DEVNET_DIR/data/accounts/genesis_accounts.json" "$GENESIS_ACCOUNTS_JSON"
    log_ok "Synced genesis_accounts.json from devnet"
  fi

  log_ok "Devnet is up"
}

step_ensure_tss_key_ready() {
  require_cmd bash
  log_info "Ensuring TSS key is ready"
  (
    cd "$LOCAL_DEVNET_DIR"
    ./devnet tss-keygen
  )
  log_ok "TSS key is ready"
}

step_setup_environment() {
  require_cmd jq curl

  local has_docker="false"
  if command -v docker >/dev/null 2>&1; then
    has_docker="true"
  fi

  if is_local_testing_env; then
    require_cmd anvil cast surfpool
  fi

  fetch_evm_block_number() {
    local label="$1"
    local rpc_url="$2"
    local response hex_block decimal_block

    response="$(curl -sS --max-time 15 -X POST "$rpc_url" \
      -H 'Content-Type: application/json' \
      --data '{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}' 2>/dev/null || true)"

    hex_block="$(echo "$response" | jq -r '.result // empty' 2>/dev/null || true)"
    if [[ -n "$hex_block" && "$hex_block" != "null" && "$hex_block" =~ ^0x[0-9a-fA-F]+$ ]]; then
      decimal_block="$(printf '%d' "$hex_block" 2>/dev/null || true)"
      if [[ "$decimal_block" =~ ^[0-9]+$ ]]; then
        printf "%s" "$decimal_block"
        return 0
      fi
    fi

    log_warn "Could not read block number for $label at $rpc_url; defaulting event_start_from to 0" >&2
    printf "%s" "0"
  }

  local sepolia_host_rpc="${ANVIL_SEPOLIA_HOST_RPC_URL:-http://localhost:9545}"
  local arbitrum_host_rpc="${ANVIL_ARBITRUM_HOST_RPC_URL:-http://localhost:9546}"
  local base_host_rpc="${ANVIL_BASE_HOST_RPC_URL:-http://localhost:9547}"
  local bsc_host_rpc="${ANVIL_BSC_HOST_RPC_URL:-http://localhost:9548}"

  local solana_host_rpc="${SURFPOOL_SOLANA_HOST_RPC_URL:-http://localhost:8899}"
  local uv_sepolia_rpc_url=""
  local uv_arbitrum_rpc_url=""
  local uv_base_rpc_url=""
  local uv_bsc_rpc_url=""
  local uv_solana_rpc_url=""

  chain_public_rpc_from_config() {
    local file_path="$1"
    local fallback_rpc="$2"
    local label="$3"
    local rpc_url

    if [[ ! -f "$file_path" ]]; then
      log_warn "Chain config file not found for $label: $file_path; using fallback $fallback_rpc"
      printf "%s" "$fallback_rpc"
      return
    fi

    rpc_url="$(jq -r '.public_rpc_url // empty' "$file_path" 2>/dev/null || true)"
    if [[ -z "$rpc_url" || "$rpc_url" == "null" ]]; then
      log_warn "public_rpc_url missing in $file_path; using fallback $fallback_rpc"
      printf "%s" "$fallback_rpc"
      return
    fi

    printf "%s" "$rpc_url"
  }

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

  if is_local_testing_env; then
    uv_sepolia_rpc_url="${LOCAL_SEPOLIA_UV_RPC_URL:-$sepolia_host_rpc}"
    uv_arbitrum_rpc_url="${LOCAL_ARBITRUM_UV_RPC_URL:-$arbitrum_host_rpc}"
    uv_base_rpc_url="${LOCAL_BASE_UV_RPC_URL:-$base_host_rpc}"
    uv_bsc_rpc_url="${LOCAL_BSC_UV_RPC_URL:-$bsc_host_rpc}"
    uv_solana_rpc_url="${LOCAL_SOLANA_UV_RPC_URL:-$solana_host_rpc}"
  else
    uv_sepolia_rpc_url="$(chain_public_rpc_from_config "$TOKENS_CONFIG_DIR/eth_sepolia/chain.json" "$sepolia_host_rpc" "eth_sepolia")"
    uv_arbitrum_rpc_url="$(chain_public_rpc_from_config "$TOKENS_CONFIG_DIR/arb_sepolia/chain.json" "$arbitrum_host_rpc" "arb_sepolia")"
    uv_base_rpc_url="$(chain_public_rpc_from_config "$TOKENS_CONFIG_DIR/base_sepolia/chain.json" "$base_host_rpc" "base_sepolia")"
    uv_bsc_rpc_url="$(chain_public_rpc_from_config "$TOKENS_CONFIG_DIR/bsc_testnet/chain.json" "$bsc_host_rpc" "bsc_testnet")"
    uv_solana_rpc_url="$(chain_public_rpc_from_config "$TOKENS_CONFIG_DIR/solana_devnet/chain.json" "$solana_host_rpc" "solana_devnet")"

    if pgrep -f "${PUSH_CHAIN_DIR}/build/puniversald start" >/dev/null 2>&1; then
      log_warn "puniversald processes are already running; RPC URL file changes apply fully after devnet restart"
    fi
  fi

  local sepolia_latest_block arbitrum_latest_block base_latest_block bsc_latest_block solana_latest_slot
  sepolia_latest_block="0"
  arbitrum_latest_block="0"
  base_latest_block="0"
  bsc_latest_block="0"
  solana_latest_slot="0"

  start_anvil_fork() {
    local label="$1"
    local port="$2"
    local chain_id="$3"
    local fork_url="$4"

    # Kill any process that is currently bound to the target port.
    # This avoids stale fork nodes when the command-line pattern changes.
    local pid
    while IFS= read -r pid; do
      [[ -n "$pid" ]] || continue
      log_info "Stopping process $pid on port $port before starting anvil $label"
      kill "$pid" >/dev/null 2>&1 || true
    done < <(lsof -ti tcp:"$port" 2>/dev/null || true)

    # Wait up to 8 seconds for the port to be fully released before binding the new process.
    local _w=0
    while lsof -ti tcp:"$port" >/dev/null 2>&1; do
      if [[ $_w -ge 8 ]]; then
        lsof -ti tcp:"$port" 2>/dev/null | xargs kill -9 2>/dev/null || true
        sleep 1
        break
      fi
      sleep 1
      _w=$(( _w + 1 ))
    done

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

    log_warn "Could not read block number from $label anvil at $rpc_url after 30s; defaulting event_start_from to 0" >&2
    printf "%s" "0"
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

    log_warn "Could not read Solana slot from surfpool at $rpc_url after 30s; defaulting event_start_from to 0" >&2
    printf "%s" "0"
  }

  if is_local_testing_env; then
    # Upstream RPCs that the local anvil forks are derived from.
    local sepolia_fork_rpc="https://sepolia.drpc.org"
    local arbitrum_fork_rpc="https://arbitrum-sepolia.gateway.tenderly.co"
    local base_fork_rpc="https://sepolia.base.org"
    local bsc_fork_rpc="wss://bsc-testnet-rpc.publicnode.com"
    local solana_upstream_rpc="https://api.devnet.solana.com"

    # Fetch event_start_from from the upstream RPCs BEFORE starting local forks.
    # This gives us the exact fork point block number reliably, without waiting for
    # local anvil startup. UVs configured to use the local anvil fork will start
    # scanning from this block number, which covers all locally-deployed contracts.
    log_info "Fetching latest block numbers from upstream RPCs for event_start_from"
    sepolia_latest_block="$(wait_for_block_number "sepolia" "$sepolia_fork_rpc")"
    arbitrum_latest_block="$(wait_for_block_number "arbitrum" "$arbitrum_fork_rpc")"
    base_latest_block="$(wait_for_block_number "base" "$base_fork_rpc")"
    bsc_latest_block="$(wait_for_block_number "bsc" "$bsc_fork_rpc")"
    solana_latest_slot="$(wait_for_solana_slot "$solana_upstream_rpc")"
    log_ok "event_start_from: sepolia=$sepolia_latest_block arbitrum=$arbitrum_latest_block base=$base_latest_block bsc=$bsc_latest_block solana=$solana_latest_slot"

    start_anvil_fork "sepolia" "9545" "11155111" "$sepolia_fork_rpc"
    start_anvil_fork "arbitrum" "9546" "421614" "$arbitrum_fork_rpc"
    start_anvil_fork "base" "9547" "84532" "$base_fork_rpc"
    # Use the configured BSC endpoint for anvil forking.
    start_anvil_fork "bsc" "9548" "97" "$bsc_fork_rpc"
    start_surfpool
    patch_local_testnet_donut_chain_configs

    # Wait for local forks to be ready before proceeding.
    wait_for_block_number "sepolia" "$sepolia_host_rpc" >/dev/null
    wait_for_block_number "arbitrum" "$arbitrum_host_rpc" >/dev/null
    wait_for_block_number "base" "$base_host_rpc" >/dev/null
    wait_for_block_number "bsc" "$bsc_host_rpc" >/dev/null
    wait_for_solana_slot "$solana_host_rpc" >/dev/null
  else
    log_info "Fetching latest block numbers from public chain RPCs for event_start_from"
    sepolia_latest_block="$(wait_for_block_number "sepolia" "$uv_sepolia_rpc_url")"
    arbitrum_latest_block="$(wait_for_block_number "arbitrum" "$uv_arbitrum_rpc_url")"
    base_latest_block="$(wait_for_block_number "base" "$uv_base_rpc_url")"
    bsc_latest_block="$(wait_for_block_number "bsc" "$uv_bsc_rpc_url")"
    solana_latest_slot="$(wait_for_solana_slot "$uv_solana_rpc_url")"
    log_ok "event_start_from: sepolia=$sepolia_latest_block arbitrum=$arbitrum_latest_block base=$base_latest_block bsc=$bsc_latest_block solana=$solana_latest_slot"
  fi

  local patched_count=0
  local uv_idx
  for uv_idx in 1 2 3 4; do
    # Prefer local file (local-native devnet); fall back to Docker container
    local local_cfg="$LOCAL_DEVNET_DIR/data/universal${uv_idx}/.puniversal/config/pushuv_config.json"
    local uv_container="universal-validator-${uv_idx}"

    local tmp_in tmp_out
    tmp_in="$(mktemp)"
    tmp_out="$(mktemp)"

    if [[ -f "$local_cfg" ]]; then
      cp "$local_cfg" "$tmp_in"
    elif [[ "$has_docker" == "true" ]] && docker ps --format '{{.Names}}' | grep -qx "$uv_container" 2>/dev/null; then
      local docker_cfg="/root/.puniversal/config/pushuv_config.json"
      if ! docker exec "$uv_container" cat "$docker_cfg" >"$tmp_in" 2>/dev/null; then
        rm -f "$tmp_in" "$tmp_out"
        log_warn "Failed to read config from $uv_container"
        continue
      fi
    else
      rm -f "$tmp_in" "$tmp_out"
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

    if [[ -f "$local_cfg" ]]; then
      cp "$tmp_out" "$local_cfg"
      if is_local_testing_env; then
        log_ok "Updated universal-validator-${uv_idx} local config for Sepolia/Arbitrum/Base/BSC/Solana LOCAL forks (event_start_from: sepolia=$sepolia_latest_block arbitrum=$arbitrum_latest_block base=$base_latest_block bsc=$bsc_latest_block solana=$solana_latest_slot)"
      else
        log_ok "Updated universal-validator-${uv_idx} local config from testnet-donut chain public RPCs (event_start_from: sepolia=$sepolia_latest_block arbitrum=$arbitrum_latest_block base=$base_latest_block bsc=$bsc_latest_block solana=$solana_latest_slot)"
      fi
    else
      local docker_cfg="/root/.puniversal/config/pushuv_config.json"
      docker cp "$tmp_out" "$uv_container":"$docker_cfg"
      if is_local_testing_env; then
        log_ok "Updated $uv_container Docker config for Sepolia/Arbitrum/Base/BSC/Solana LOCAL forks (event_start_from: sepolia=$sepolia_latest_block arbitrum=$arbitrum_latest_block base=$base_latest_block bsc=$bsc_latest_block solana=$solana_latest_slot)"
      else
        log_ok "Updated $uv_container Docker config from testnet-donut chain public RPCs (event_start_from: sepolia=$sepolia_latest_block arbitrum=$arbitrum_latest_block base=$base_latest_block bsc=$bsc_latest_block solana=$solana_latest_slot)"
      fi
    fi
    rm -f "$tmp_in" "$tmp_out"
    patched_count=$((patched_count + 1))
  done

  if [[ "$patched_count" -eq 0 ]]; then
    log_warn "No universal validators found (local or Docker); skipped pushuv_config.json patch"
    return 0
  fi

  if is_local_testing_env; then
    log_ok "Patched $patched_count universal validator config(s) with LOCAL fork RPC/event_start_from (including Solana)"
  else
    log_ok "Patched $patched_count universal validator config(s) with testnet-donut chain public RPCs and live event_start_from values"
  fi
}

step_stop_running_nodes() {
  log_info "Stopping running local nodes/validators"

  if [[ -x "$LOCAL_DEVNET_DIR/devnet" ]]; then
    (
      cd "$LOCAL_DEVNET_DIR"
      ./devnet down || true
    )
  fi

  pkill -f "$PUSH_CHAIN_DIR/build/pchaind start" >/dev/null 2>&1 || true
  pkill -f "$PUSH_CHAIN_DIR/build/puniversald" >/dev/null 2>&1 || true

  log_ok "Running nodes stopped"
}

step_fund_uv_broadcasters_on_anvil() {
  if ! is_local_testing_env; then
    log_info "step_fund_uv_broadcasters_on_anvil: skipping (non-LOCAL environment)"
    return 0
  fi
  require_cmd cast
  local anvil_rpc="${ANVIL_SEPOLIA_HOST_RPC_URL:-http://localhost:9545}"
  # Anvil default account 0 — always seeded with 10,000 ETH in any anvil fork (mnemonic: "test test ... junk")
  local funder_pk="0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
  local fund_amount="10ether"
  local funded=0
  for addr_file in "$LOCAL_DEVNET_DIR/data"/universal*/.puniversal/keyring-test/*.address; do
    [[ -f "$addr_file" ]] || continue
    local addr_hex
    addr_hex="$(basename "$addr_file" .address)"
    local addr="0x${addr_hex}"
    local balance
    balance="$(cast balance "$addr" --rpc-url "$anvil_rpc" 2>/dev/null || echo "0")"
    if [[ "$balance" == "0" ]]; then
      log_info "Funding UV broadcaster $addr with $fund_amount on Anvil Sepolia"
      if cast send "$addr" --value "$fund_amount" --private-key "$funder_pk" \
           --rpc-url "$anvil_rpc" >/dev/null 2>&1; then
        funded=$((funded + 1))
      else
        log_warn "Failed to fund UV broadcaster $addr on Anvil Sepolia"
      fi
    else
      log_info "UV broadcaster $addr already has ETH on Anvil Sepolia: $balance wei"
    fi
  done
  log_ok "UV broadcaster funding done (funded $funded new address(es))"
}

# Sync every EVM vault's TSS_ADDRESS to the current local TSS key so that
# AccessControlUnauthorizedAccount (0xe2517d3f) never blocks outbound txs.
# Also funds the TSS signer on each Anvil chain so it can pay gas.
step_sync_vault_tss_on_anvil() {
  if ! is_local_testing_env; then
    log_info "step_sync_vault_tss_on_anvil: skipping (non-LOCAL environment)"
    return 0
  fi
  require_cmd cast jq python3

  # Derive the TSS EVM address from the on-chain TSS public key.
  # 1. Query compressed secp256k1 pubkey from the utss module.
  # 2. Decompress it using pure Python3 math (stdlib only, no extra packages).
  # 3. keccak256(x || y) via `cast keccak`, last 20 bytes = EVM address.
  local tss_pubkey tss_addr
  tss_pubkey="$("$PUSH_CHAIN_DIR/build/pchaind" query utss current-key \
    --node tcp://127.0.0.1:26657 --output json 2>/dev/null \
    | jq -r '.key.tss_pubkey // empty' 2>/dev/null || true)"

  if [[ -z "$tss_pubkey" ]]; then
    log_warn "step_sync_vault_tss_on_anvil: TSS key not found on chain yet, skipping"
    return 0
  fi

  # Decompress pubkey → 64-byte uncompressed (x||y) hex using Python3 stdlib.
  local uncompressed_hex
  uncompressed_hex="$(python3 -c "
prefix = int('${tss_pubkey:0:2}', 16)
x = int('${tss_pubkey:2}', 16)
p = 0xFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFEFFFFFC2F
y_sq = (pow(x, 3, p) + 7) % p
y = pow(y_sq, (p + 1) // 4, p)
if (y % 2) != (prefix % 2):
    y = p - y
print(format(x, '064x') + format(y, '064x'))
" 2>/dev/null || true)"

  if [[ -z "$uncompressed_hex" ]]; then
    log_warn "step_sync_vault_tss_on_anvil: failed to decompress TSS pubkey, skipping"
    return 0
  fi

  local keccak_hash
  keccak_hash="$(cast keccak "0x$uncompressed_hex" 2>/dev/null || true)"
  tss_addr="0x${keccak_hash: -40}"

  if [[ -z "$tss_addr" || ${#tss_addr} -ne 42 ]]; then
    log_warn "step_sync_vault_tss_on_anvil: failed to derive TSS EVM address, skipping"
    return 0
  fi

  log_info "Syncing vault TSS address to $tss_addr on all local Anvil EVM chains"

  local DEF_ADMIN_ROLE="0x0000000000000000000000000000000000000000000000000000000000000000"
  # Anvil default account 0 — always seeded with 10,000 ETH in every fork
  local funder_pk="0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
  # Known deployer addresses for the forge localSetup scripts — these never change
  # between runs since it is the same forge wallet that deploys the vault contracts.
  local KNOWN_ADMINS=(
    "0x35b84d6848d16415177c64d64504663b998a6ab4"
    "0xe520d4A985A2356Fa615935a822Ce4eFAcA24aB6"
    "0xd854dde7c58ec1b405e6577f48a7cc5b5e6ef317"
  )

  # cfg_name:anvil_rpc pairs — mirrors the Anvil forks started in step_devnet.
  local CHAIN_INFO=(
    "eth_sepolia:${ANVIL_SEPOLIA_HOST_RPC_URL:-http://localhost:9545}"
    "arb_sepolia:${ANVIL_ARBITRUM_HOST_RPC_URL:-http://localhost:9546}"
    "base_sepolia:${ANVIL_BASE_HOST_RPC_URL:-http://localhost:9547}"
    "bsc_testnet:${ANVIL_BSC_HOST_RPC_URL:-http://localhost:9548}"
  )

  for entry in "${CHAIN_INFO[@]}"; do
    local cfg_name="${entry%%:*}"
    local rpc="${entry#*:}"
    local chain_cfg="$TOKENS_CONFIG_DIR/$cfg_name/chain.json"

    if [[ ! -f "$chain_cfg" ]]; then
      log_warn "step_sync_vault_tss_on_anvil: no chain config at $chain_cfg, skipping"
      continue
    fi

    # Fund the TSS signer so it can pay gas for outbound vault txs.
    local tss_bal
    tss_bal="$(cast balance "$tss_addr" --rpc-url "$rpc" 2>/dev/null || echo "0")"
    if [[ "$tss_bal" == "0" ]]; then
      if cast send "$tss_addr" --value "10ether" --private-key "$funder_pk" --rpc-url "$rpc" >/dev/null 2>&1; then
        log_ok "  $cfg_name: funded TSS signer $tss_addr with 10 ETH"
      else
        log_warn "  $cfg_name: failed to fund TSS signer $tss_addr"
      fi
    else
      log_info "  $cfg_name: TSS signer $tss_addr already has ETH (bal=$tss_bal)"
    fi

    local gateway
    gateway="$(jq -r '.gateway_address // empty' "$chain_cfg" 2>/dev/null || true)"
    if [[ -z "$gateway" || "$gateway" == "null" ]]; then
      log_warn "step_sync_vault_tss_on_anvil: no gateway_address in $chain_cfg, skipping"
      continue
    fi

    local vault
    vault="$(cast call "$gateway" 'VAULT()(address)' --rpc-url "$rpc" 2>/dev/null || true)"
    if [[ -z "$vault" || "$vault" == "0x0000000000000000000000000000000000000000" ]]; then
      log_warn "step_sync_vault_tss_on_anvil: VAULT() empty for gateway $gateway ($cfg_name), skipping"
      continue
    fi

    # Skip only if the vault's stored TSS_ADDRESS already matches the current key.
    # Checking TSS_ADDRESS (not just hasRole) ensures we update after every re-keying,
    # because setTSS atomically revokes the old role and grants the new one.
    local vault_tss
    vault_tss="$(cast call "$vault" 'TSS_ADDRESS()(address)' --rpc-url "$rpc" 2>/dev/null || true)"
    if [[ "$(echo "$vault_tss" | tr '[:upper:]' '[:lower:]')" == "$(echo "$tss_addr" | tr '[:upper:]' '[:lower:]')" ]]; then
      log_info "  $cfg_name vault $vault TSS_ADDRESS already matches $tss_addr"
      continue
    fi

    # Find the DEFAULT_ADMIN_ROLE holder among known candidates.
    local vault_admin=""
    for candidate in "${KNOWN_ADMINS[@]}"; do
      local is_admin
      is_admin="$(cast call "$vault" 'hasRole(bytes32,address)(bool)' "$DEF_ADMIN_ROLE" "$candidate" \
        --rpc-url "$rpc" 2>/dev/null || echo "false")"
      if [[ "$is_admin" == "true" ]]; then
        vault_admin="$candidate"
        break
      fi
    done

    if [[ -z "$vault_admin" ]]; then
      log_warn "step_sync_vault_tss_on_anvil: no known admin for vault $vault ($cfg_name), skipping"
      continue
    fi

    # Impersonate the admin on the Anvil fork (no private key needed) and call setTSS.
    cast rpc anvil_impersonateAccount "$vault_admin" --rpc-url "$rpc" >/dev/null 2>&1 || true
    cast rpc anvil_setBalance "$vault_admin" "0x56BC75E2D63100000" --rpc-url "$rpc" >/dev/null 2>&1 || true

    if cast send "$vault" "setTSS(address)" "$tss_addr" \
        --rpc-url "$rpc" \
        --from "$vault_admin" \
        --unlocked >/dev/null 2>&1; then
      log_ok "  $cfg_name vault $vault: TSS updated to $tss_addr"
    else
      log_warn "  step_sync_vault_tss_on_anvil: setTSS failed on vault $vault ($cfg_name)"
    fi
  done

  log_ok "Vault TSS sync complete"
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
  # Keep runtime env stable: avoid re-sourcing .env here because that can
  # reset already-normalized absolute paths (CORE_CONTRACTS_DIR/GATEWAY_DIR/etc).
  FUND_TO_ADDRESS="$COSMOS_ADDRESS"
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
  log_info "Using core contracts repo dir: $CORE_CONTRACTS_DIR"
  clone_or_update_repo "$CORE_CONTRACTS_REPO" "$CORE_CONTRACTS_BRANCH" "$CORE_CONTRACTS_DIR"

  log_info "Running forge build in core contracts"
  (cd "$CORE_CONTRACTS_DIR" && forge build)

  local log_file="$LOG_DIR/core_setup_$(date +%Y%m%d_%H%M%S).log"
  local failed=0
  local resume_attempt=1
  local resume_max_attempts="${CORE_RESUME_MAX_ATTEMPTS:-0}"  # 0 = unlimited

  log_info "Clearing stale forge broadcast cache for fresh deploy"
  rm -rf "$CORE_CONTRACTS_DIR/broadcast/setup.s.sol"

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

  local gateway_repo_dir="$GATEWAY_DIR"
  local sibling_gateway_dir="$PUSH_CHAIN_DIR/../push-chain-gateway-contracts"

  log_info "Using gateway repo dir: $gateway_repo_dir"

  # Some local setups accidentally resolve GATEWAY_DIR under push-chain/ itself.
  # Prefer a repo path that actually contains the localSetup gateway scripts.
  if [[ -d "$sibling_gateway_dir/contracts/evm-gateway" ]]; then
    if [[ ! -d "$gateway_repo_dir/contracts/evm-gateway" || ( ! -f "$gateway_repo_dir/contracts/evm-gateway/script/localSetup/setup.s.sol" && ! -f "$gateway_repo_dir/contracts/evm-gateway/scripts/localSetup/setup.s.sol" && ! -f "$gateway_repo_dir/contracts/evm-gateway/localSetup/setup.s.sol" ) ]]; then
      log_warn "Switching gateway repo dir to sibling path: $sibling_gateway_dir"
      gateway_repo_dir="$sibling_gateway_dir"
    fi
  fi

  clone_or_update_repo "$GATEWAY_REPO" "$GATEWAY_BRANCH" "$gateway_repo_dir"

  log_info "Preparing gateway repo submodules"
  (
    cd "$gateway_repo_dir"
    if [[ -d "contracts/svm-gateway/mock-pyth" ]]; then
      git rm --cached contracts/svm-gateway/mock-pyth || true
      rm -rf contracts/svm-gateway/mock-pyth
    fi
    git submodule update --init --recursive
  )

  local gw_dir="$gateway_repo_dir/contracts/evm-gateway"
  local gw_setup_script=""
  local gw_log="$LOG_DIR/gateway_setup_$(date +%Y%m%d_%H%M%S).log"
  local failed=0
  local resume_attempt=1
  local resume_max_attempts="${GATEWAY_RESUME_MAX_ATTEMPTS:-0}"  # 0 = unlimited

  if [[ -f "$gw_dir/script/localSetup/setup.s.sol" ]]; then
    gw_setup_script="script/localSetup/setup.s.sol"
  elif [[ -f "$gw_dir/scripts/localSetup/setup.s.sol" ]]; then
    gw_setup_script="scripts/localSetup/setup.s.sol"
  elif [[ -f "$gw_dir/localSetup/setup.s.sol" ]]; then
    gw_setup_script="localSetup/setup.s.sol"
  else
    log_err "Gateway setup script not found under $gw_dir/(script|scripts)/localSetup/setup.s.sol or $gw_dir/localSetup/setup.s.sol"
    exit 1
  fi

  log_info "Building gateway evm contracts"
  (cd "$gw_dir" && forge build)

  log_info "Clearing stale forge broadcast cache for gateway deploy"
  rm -rf "$gw_dir/broadcast/$(basename "$gw_setup_script" .s.sol).s.sol"

  log_info "Running gateway local setup script"
  (
    cd "$gw_dir"
    forge script "$gw_setup_script" \
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
        forge script "$gw_setup_script" \
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

  # Ensure canonical local precompile proxy wiring used by SDK tests:
  #   C1 = UniversalGatewayPC proxy, B0 = VaultPC proxy, C0 = UniversalCore.
  # Some gateway repo branches configure B0 only; this post-step self-heals C1.
  local C0="0x00000000000000000000000000000000000000C0"
  local C1="0x00000000000000000000000000000000000000C1"
  local B0="0x00000000000000000000000000000000000000B0"
  local C1_PROXY_ADMIN="0xf2000000000000000000000000000000000000c1"
  local OWNER_ADDR="0x778D3206374f8AC265728E18E3fE2Ae6b93E4ce4"

  log_info "Verifying C1 UniversalGatewayPC wiring"
  if ! cast call "$C1" 'UNIVERSAL_CORE()(address)' --rpc-url "$PUSH_RPC_URL" >/dev/null 2>&1; then
    log_warn "C1.UNIVERSAL_CORE() reverted. Repairing C1 proxy implementation + initialize"

    # Reuse implementation currently behind B0 proxy (same UniversalGatewayPC bytecode family).
    local impl_slot impl_word impl_addr
    impl_slot="0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc"
    impl_word="$(cast storage "$B0" "$impl_slot" --rpc-url "$PUSH_RPC_URL" 2>/dev/null || true)"
    impl_addr="0x$(echo "$impl_word" | sed -E 's/^0x//; s/^.{24}//' | tr -d '\n')"
    if ! validate_eth_address "$impl_addr"; then
      log_err "Failed to resolve UniversalGatewayPC implementation from B0 proxy slot"
      exit 1
    fi

    cast send "$C1_PROXY_ADMIN" 'upgradeAndCall(address,address,bytes)' \
      "$C1" "$impl_addr" "0x" \
      --rpc-url "$PUSH_RPC_URL" \
      --private-key "$PRIVATE_KEY" >/dev/null

    cast send "$C1" 'initialize(address,address,address,address)' \
      "$OWNER_ADDR" "$OWNER_ADDR" "$C0" "$B0" \
      --rpc-url "$PUSH_RPC_URL" \
      --private-key "$PRIVATE_KEY" >/dev/null || true

    cast send "$C0" 'setUniversalGatewayPC(address)' "$C1" \
      --rpc-url "$PUSH_RPC_URL" \
      --private-key "$PRIVATE_KEY" >/dev/null || true
  fi

  local c1_uc c0_ug c1_uc_lc c0_ug_lc c0_lc c1_lc
  c1_uc="$(cast call "$C1" 'UNIVERSAL_CORE()(address)' --rpc-url "$PUSH_RPC_URL" 2>/dev/null || true)"
  c0_ug="$(cast call "$C0" 'universalGatewayPC()(address)' --rpc-url "$PUSH_RPC_URL" 2>/dev/null || true)"

  # If C1 is initialized but C0 is not linked yet, repair linkage explicitly.
  if [[ -n "$c1_uc" && -n "$c0_ug" ]]; then
    local c1_uc_tmp c0_ug_tmp c0_lc_tmp c1_lc_tmp
    c1_uc_tmp="$(echo "$c1_uc" | tr '[:upper:]' '[:lower:]')"
    c0_ug_tmp="$(echo "$c0_ug" | tr '[:upper:]' '[:lower:]')"
    c0_lc_tmp="$(echo "$C0" | tr '[:upper:]' '[:lower:]')"
    c1_lc_tmp="$(echo "$C1" | tr '[:upper:]' '[:lower:]')"

    if [[ "$c1_uc_tmp" == "$c0_lc_tmp" && "$c0_ug_tmp" != "$c1_lc_tmp" ]]; then
      log_warn "C0.universalGatewayPC is not linked to C1. Repairing linkage"
      cast send "$C0" 'setUniversalGatewayPC(address)' "$C1" \
        --rpc-url "$PUSH_RPC_URL" \
        --private-key "$PRIVATE_KEY" >/dev/null || true

      c0_ug="$(cast call "$C0" 'universalGatewayPC()(address)' --rpc-url "$PUSH_RPC_URL" 2>/dev/null || true)"
    fi
  fi

  c1_uc_lc="$(echo "$c1_uc" | tr '[:upper:]' '[:lower:]')"
  c0_ug_lc="$(echo "$c0_ug" | tr '[:upper:]' '[:lower:]')"
  c0_lc="$(echo "$C0" | tr '[:upper:]' '[:lower:]')"
  c1_lc="$(echo "$C1" | tr '[:upper:]' '[:lower:]')"
  if [[ "$c1_uc_lc" != "$c0_lc" || "$c0_ug_lc" != "$c1_lc" ]]; then
    log_err "Gateway wiring invalid after setup: C1.UNIVERSAL_CORE=$c1_uc, C0.universalGatewayPC=$c0_ug"
    exit 1
  fi

  local manager_role has_manager
  manager_role="$(cast keccak 'MANAGER_ROLE')"
  has_manager="$(cast call "$C0" 'hasRole(bytes32,address)(bool)' "$manager_role" "$OWNER_ADDR" --rpc-url "$PUSH_RPC_URL" 2>/dev/null || echo "false")"

  if [[ "$has_manager" != "true" ]]; then
    cast send "$C0" 'grantRole(bytes32,address)' "$manager_role" "$OWNER_ADDR" \
      --rpc-url "$PUSH_RPC_URL" \
      --private-key "$PRIVATE_KEY" >/dev/null || true
  fi

  # Seed gas-token mapping for each deployed gas token PRC20 (p* symbols).
  if [[ -s "$DEPLOY_ADDRESSES_FILE" ]]; then
    while IFS=$'\t' read -r symbol token_addr; do
      [[ -n "$symbol" && -n "$token_addr" ]] || continue
      local chain_ns
      chain_ns="$(cast call "$token_addr" 'SOURCE_CHAIN_NAMESPACE()(string)' --rpc-url "$PUSH_RPC_URL" 2>/dev/null || echo "")"
      [[ -n "$chain_ns" ]] || continue

      cast send "$C0" 'setGasTokenPRC20(string,address)' "$chain_ns" "$token_addr" \
        --rpc-url "$PUSH_RPC_URL" \
        --private-key "$PRIVATE_KEY" >/dev/null || true
    done < <(jq -r '.tokens[]? | select((.symbol // "") | startswith("p")) | [.symbol, .address] | @tsv' "$DEPLOY_ADDRESSES_FILE")
  fi

  # Ensure non-zero base gas limits so sendUniversalTxOutbound(req.gasLimit=0)
  # can resolve a valid fee quote through UniversalCore.
  local base_gas
  base_gas="$(cast call "$C0" 'BASE_GAS_LIMIT()(uint256)' --rpc-url "$PUSH_RPC_URL" 2>/dev/null || echo "")"
  if [[ -z "$base_gas" || "$base_gas" == "0" ]]; then
    log_warn "UniversalCore BASE_GAS_LIMIT is 0. Applying local defaults for outbound chains"

    for ns in "eip155:11155111" "eip155:421614" "eip155:84532" "eip155:97" "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1"; do
      cast send "$C0" 'setBaseGasLimitByChain(string,uint256)' "$ns" 2000000 \
        --rpc-url "$PUSH_RPC_URL" \
        --private-key "$PRIVATE_KEY" >/dev/null || true
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

step_fund_uea_prc20() {
  require_cmd cast jq
  ensure_deploy_file

  local sdk_evm_private_key
  sdk_evm_private_key="${EVM_PRIVATE_KEY:-${PRIVATE_KEY:-}}"
  if [[ -z "$sdk_evm_private_key" ]]; then
    log_warn "No EVM_PRIVATE_KEY found; skipping UEA PRC20 funding"
    return 0
  fi

  local evm_addr
  evm_addr="$(cast wallet address "$sdk_evm_private_key" 2>/dev/null || true)"
  if ! validate_eth_address "$evm_addr"; then
    log_warn "Could not derive EVM address from EVM_PRIVATE_KEY; skipping UEA PRC20 funding"
    return 0
  fi

  local factory_addr="0x00000000000000000000000000000000000000eA"
  local uea_addr
  uea_addr="$(cast call "$factory_addr" "computeUEA((string,string,bytes))(address)" \
    "(eip155,11155111,$evm_addr)" \
    --rpc-url "$PUSH_RPC_URL" 2>/dev/null | grep -Eo '0x[a-fA-F0-9]{40}' | head -1 || true)"

  if ! validate_eth_address "$uea_addr"; then
    log_warn "Could not compute UEA address for $evm_addr; skipping UEA PRC20 funding"
    return 0
  fi

  log_info "Funding UEA $uea_addr (signer: $evm_addr) with PRC20 tokens from deployer"

  local token_count
  token_count="$(jq -r '.tokens | length' "$DEPLOY_ADDRESSES_FILE")"
  if [[ "$token_count" == "0" ]]; then
    log_warn "No tokens in deploy addresses to fund UEA with"
    return 0
  fi

  local token_symbol token_addr token_decimals fund_amount
  while IFS=$'\t' read -r token_symbol token_addr token_decimals; do
    [[ -n "$token_addr" ]] || continue
    # 1e9 for tokens with <=9 decimals (e.g. USDT×1000, pSOL×1), 1e18 for 18-decimal tokens (e.g. 1 pETH)
    if [[ "${token_decimals:-18}" -le 9 ]]; then
      fund_amount="1000000000"
    else
      fund_amount="1000000000000000000"
    fi
    log_info "  Sending $fund_amount of $token_symbol ($token_addr) to UEA $uea_addr"
    cast send --private-key "$PRIVATE_KEY" "$token_addr" \
      "transfer(address,uint256)(bool)" "$uea_addr" "$fund_amount" \
      --rpc-url "$PUSH_RPC_URL" 2>&1 | grep -E "^status" || true
  done < <(jq -r '.tokens[]? | [.symbol, .address, (.decimals // 18)] | @tsv' "$DEPLOY_ADDRESSES_FILE")

  log_ok "UEA PRC20 funding complete"
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

  log_info "Clearing stale forge broadcast cache for configureUniversalCore"
  rm -rf "$CORE_CONTRACTS_DIR/broadcast/configureUniversalCore.s.sol"

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

step_deploy_counter_and_sync_sdk() {
  require_cmd cast perl
  [[ -n "${PRIVATE_KEY:-}" ]] || { log_err "Set PRIVATE_KEY in e2e-tests/.env"; exit 1; }

  local sdk_counter_addr_file="$PUSH_CHAIN_SDK_DIR/packages/core/src/lib/push-chain/helpers/addresses.ts"
  local counter_creation_code="0x6080604052348015600e575f5ffd5b506102068061001c5f395ff3fe608060405260043610610042575f3560e01c806312065fe01461004d5780639b0e94af14610077578063d09de08a146100a1578063d826f88f146100ab57610049565b3661004957005b5f5ffd5b348015610058575f5ffd5b506100616100c1565b60405161006e9190610157565b60405180910390f35b348015610082575f5ffd5b5061008b6100c8565b6040516100989190610157565b60405180910390f35b6100a96100cd565b005b3480156100b6575f5ffd5b506100bf610137565b005b5f47905090565b5f5481565b60015f5f8282546100de919061019d565b925050819055503373ffffffffffffffffffffffffffffffffffffffff165f547fb6aa5bfdc1ab753194658fada8fa1725a667cdea7df54bd400f8bced617dfd4c3460405161012d9190610157565b60405180910390a3565b5f5f81905550565b5f819050919050565b6101518161013f565b82525050565b5f60208201905061016a5f830184610148565b92915050565b7f4e487b71000000000000000000000000000000000000000000000000000000005f52601160045260245ffd5b5f6101a78261013f565b91506101b28361013f565b92508282019050808211156101ca576101c9610170565b5b9291505056fea26469706673582212204acec08331d08192e4797fc12653c602c2ca1574d44468713f91a095fdefe6d564736f6c634300081e0033"

  if [[ ! -f "$sdk_counter_addr_file" ]]; then
    log_err "SDK counter addresses file not found: $sdk_counter_addr_file"
    exit 1
  fi

  log_info "Deploying CounterPayable contract on Push localnet"
  local deploy_out counter_addr
  deploy_out="$(cast send --rpc-url "$PUSH_RPC_URL" --private-key "$PRIVATE_KEY" --create "$counter_creation_code" 2>&1)" || {
    log_err "Counter deployment failed"
    echo "$deploy_out"
    exit 1
  }

  counter_addr="$(echo "$deploy_out" | awk '/contractAddress/ {print $2; exit}')"
  if ! validate_eth_address "$counter_addr"; then
    log_err "Could not parse deployed counter contract address from cast output"
    echo "$deploy_out"
    exit 1
  fi

  ensure_deploy_file
  record_contract "COUNTER_ADDRESS_PAYABLE" "$counter_addr"

  COUNTER_ADDR="$counter_addr" perl -0pi -e '
    if (/COUNTER_ADDRESS_PAYABLE/s) {
      s/0x[a-fA-F0-9]{40}/$ENV{COUNTER_ADDR}/;
    }
  ' "$sdk_counter_addr_file"

  if ! grep -q "$counter_addr" "$sdk_counter_addr_file"; then
    log_err "Failed to sync COUNTER_ADDRESS_PAYABLE in $sdk_counter_addr_file"
    exit 1
  fi

  log_ok "Deployed CounterPayable: $counter_addr"
  log_ok "Synced SDK COUNTER_ADDRESS_PAYABLE in $sdk_counter_addr_file"
}

step_bootstrap_cea_for_sdk_signer() {
  require_cmd node

  local sdk_env_file="$PUSH_CHAIN_SDK_DIR/packages/core/.env"
  if [[ ! -f "$sdk_env_file" ]]; then
    log_warn "SDK env file not found at $sdk_env_file; running setup-sdk first"
    step_setup_push_chain_sdk
  fi

  if [[ ! -d "$PUSH_CHAIN_SDK_DIR" ]]; then
    log_err "SDK repo not found at $PUSH_CHAIN_SDK_DIR"
    exit 1
  fi

  log_info "Bootstrapping CEA deployment for SDK signer on BSC testnet fork"
  if ! (
    cd "$PUSH_CHAIN_SDK_DIR"
    node -r @swc-node/register <<'NODE'
const path = require('path');
require('dotenv').config({ path: path.resolve(process.cwd(), 'packages/core/.env') });

const { PushChain } = require('./packages/core/src');
const { createWalletClient, http, parseEther } = require('viem');
const { privateKeyToAccount } = require('viem/accounts');
const { CHAIN_INFO } = require('./packages/core/src/lib/constants/chain');
const { CHAIN, PUSH_NETWORK } = require('./packages/core/src/lib/constants/enums');
const { getCEAAddress } = require('./packages/core/src/lib/orchestrator/cea-utils');

async function main() {
  const evmPrivateKey = process.env.EVM_PRIVATE_KEY;
  const pushPrivateKey = process.env.PUSH_PRIVATE_KEY;
  if (!evmPrivateKey) {
    throw new Error('EVM_PRIVATE_KEY is missing in packages/core/.env');
  }
  if (!pushPrivateKey) {
    throw new Error('PUSH_PRIVATE_KEY is missing in packages/core/.env');
  }

  // Derive the target UEA account from the EVM key (the same identity used by cea-to-uea tests).
  const evmAccount = privateKeyToAccount(evmPrivateKey);
  const evmWalletClient = createWalletClient({
    account: evmAccount,
    transport: http(CHAIN_INFO[CHAIN.ETHEREUM_SEPOLIA].defaultRPC[0]),
  });

  const evmUniversalSigner = await PushChain.utils.signer.toUniversalFromKeypair(evmWalletClient, {
    chain: CHAIN.ETHEREUM_SEPOLIA,
    library: PushChain.CONSTANTS.LIBRARY.ETHEREUM_VIEM,
  });
  const evmClient = await PushChain.initialize(evmUniversalSigner, {
    network: PUSH_NETWORK.LOCALNET,
    printTraces: false,
  });
  const targetUea = evmClient.universal.account;

  // Use a native Push signer to bootstrap the CEA deployment/funding for that target UEA.
  const pushAccount = privateKeyToAccount(pushPrivateKey);
  const pushWalletClient = createWalletClient({
    account: pushAccount,
    transport: http(CHAIN_INFO[CHAIN.PUSH_LOCALNET].defaultRPC[0]),
  });

  const pushUniversalSigner = await PushChain.utils.signer.toUniversalFromKeypair(pushWalletClient, {
    chain: CHAIN.PUSH_LOCALNET,
    library: PushChain.CONSTANTS.LIBRARY.ETHEREUM_VIEM,
  });
  const pushClient = await PushChain.initialize(pushUniversalSigner, {
    network: PUSH_NETWORK.LOCALNET,
    printTraces: false,
  });

  let ceaResult = await getCEAAddress(targetUea, CHAIN.BNB_TESTNET);
  console.log(`CEA bootstrap pre-check: targetUEA=${targetUea} cea=${ceaResult.cea} deployed=${ceaResult.isDeployed}`);

  if (!ceaResult.isDeployed) {
    const tx = await pushClient.universal.sendTransaction({
      to: { address: ceaResult.cea, chain: CHAIN.BNB_TESTNET },
      value: parseEther('0.00005'),
    });
    const receipt = await tx.wait();
    console.log(`CEA bootstrap tx: hash=${tx.hash} status=${receipt.status} external=${receipt.externalTxHash || 'n/a'}`);

    ceaResult = await getCEAAddress(targetUea, CHAIN.BNB_TESTNET);
    console.log(`CEA bootstrap post-check: deployed=${ceaResult.isDeployed}`);
  }

  if (!ceaResult.isDeployed) {
    throw new Error('CEA is still not deployed after bootstrap transaction');
  }
}

main().catch((err) => {
  const msg = err && err.message ? err.message : String(err);
  console.error(msg);
  process.exit(1);
});
NODE
  ); then
    log_err "CEA bootstrap step failed"

    if docker ps --format '{{.Names}}' | grep -qx 'universal-validator-1'; then
      log_warn "Dumping recent universal-validator-1 logs for diagnosis"
      docker logs --tail 200 universal-validator-1 2>&1 || true
    fi
    exit 1
  fi

  log_ok "CEA bootstrap complete"
}

step_wait_for_gas_oracle() {
  require_cmd cast jq

  local C0="0x00000000000000000000000000000000000000C0"
  # EVM outbound-enabled chain namespaces (Solana is not EVM, skip)
  local namespaces=("eip155:11155111" "eip155:421614" "eip155:84532" "eip155:97")
  local timeout_seconds="${GAS_ORACLE_WAIT_TIMEOUT:-120}"
  local poll_interval=5

  log_info "Waiting for gas oracle to populate UNIVERSAL_CORE gas prices (timeout ${timeout_seconds}s)..."

  local deadline=$(( $(date +%s) + timeout_seconds ))

  while true; do
    local all_ready=true
    for ns in "${namespaces[@]}"; do
      local price
      price="$(cast call "$C0" 'gasPriceByChainNamespace(string)(uint256)' "$ns" \
        --rpc-url "$PUSH_RPC_URL" 2>/dev/null || echo "0")"
      # strip leading zeros / whitespace; treat blank as 0
      price="${price//[[:space:]]/}"
      price="${price#0x}"
      if [[ -z "$price" || "$price" == "0" ]]; then
        all_ready=false
        log_info "  gas price not yet set for $ns, waiting..."
        break
      fi
      log_info "  gas price ready for $ns: $price"
    done

    if $all_ready; then
      log_ok "Gas oracle has voted — all outbound chain gas prices are non-zero"
      return 0
    fi

    if [[ $(date +%s) -ge $deadline ]]; then
      log_warn "Timed out waiting for gas oracle to vote (${timeout_seconds}s). Proceeding anyway — outbound txs may fail if triggered immediately."
      return 0
    fi

    sleep "$poll_interval"
  done
}

cmd_all() {
  step_setup_environment
  (cd "$PUSH_CHAIN_DIR" && make replace-addresses)
  (cd "$PUSH_CHAIN_DIR" && make build)
  step_update_env_fund_to_address
  step_stop_running_nodes
  step_devnet
  step_ensure_tss_key_ready
  step_setup_environment
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
  step_clone_push_chain_sdk
  step_deploy_counter_and_sync_sdk
  sdk_sync_localnet_constants
  step_sync_vault_tss_on_anvil
  step_wait_for_gas_oracle
}

cmd_show_help() {
  cat <<EOF
Usage: $(basename "$0") <command>

Commands:
  setup-environment      Sync universal-validator RPC URLs (LOCAL => anvil localhost RPCs; non-LOCAL => testnet-donut chain public_rpc_url)
  devnet                 Build/start local-multi-validator devnet + uvalidators
  print-genesis          Print first genesis account + mnemonic
  recover-genesis-key    Recover genesis key into local keyring
  fund                   Fund FUND_TO_ADDRESS from genesis key
  setup-core             Clone/build/setup core contracts (auto resume on failure)
  setup-swap             Clone/install/deploy swap AMM contracts
  sync-addresses         Apply deploy_addresses.json into test-addresses.json
  create-pool            Create WPC pools for all deployed core tokens
  fund-uea-prc20         Transfer PRC20 tokens (pETH/pUSDT/pSOL etc.) from deployer to test UEA
  configure-core         Run configureUniversalCore.s.sol (auto --resume retries)
  check-addresses        Check/report deploy addresses (WPC/Factory/QuoterV2/SwapRouter)
  write-core-env         Create core-contracts .env from deploy_addresses.json
  update-token-config    Update eth_sepolia_eth.json contract_address using deployed token
  setup-gateway          Clone/setup gateway repo and run forge localSetup (with --resume retry)
  sync-vault-tss         Grant TSS_ROLE on each Anvil EVM vault to the current local TSS key (LOCAL only)
  bootstrap-cea-sdk      Ensure CEA is deployed for SDK signer on BSC testnet fork (Route 2 bootstrap)
  deploy-counter-sdk     Deploy CounterPayable on Push localnet and sync SDK COUNTER_ADDRESS_PAYABLE
  clone-sdk              Clone/update push-chain-sdk repo only (no env/deps setup)
  setup-sdk              Setup push-chain-sdk (requires clone-sdk first): generate .env, replace TESTNET→LOCALNET in __e2e__ files, install deps
  sdk-test-all           Replace PUSH_NETWORK TESTNET variants with LOCALNET and run all configured SDK E2E tests
  sdk-test-outbound-all  Replace PUSH_NETWORK TESTNET variants with LOCALNET and run all configured SDK outbound E2E tests (TESTING_ENV=LOCAL)
  quick-testing-outbound Run setup-sdk + fund-uea-prc20, then execute cea-to-eoa.spec.ts and cea-to-uea.spec.ts only
  sdk-test-pctx-last-transaction  Run pctx-last-transaction.spec.ts
  sdk-test-send-to-self  Run send-to-self.spec.ts
  sdk-test-progress-hook Run progress-hook-per-tx.spec.ts
  sdk-test-bridge-multicall  Run bridge-multicall.spec.ts
  sdk-test-pushchain     Run pushchain.spec.ts
  sdk-test-bridge-hooks  Run bridge-hooks.spec.ts
  sdk-test-cea-to-eoa    Run cea-to-eoa.spec.ts (outbound Route 3; requires TESTING_ENV=LOCAL)
  add-uregistry-configs  Submit chain + token config txs via local-multi-validator validator1
  record-contract K A    Manually record contract key/address
  record-token N S A     Manually record token name/symbol/address
  all                    Run full setup pipeline
  help                   Show this help

Primary files:
  Env:     $ENV_FILE
  Address: $DEPLOY_ADDRESSES_FILE

Important env:
  TESTING_ENV=LOCAL      Enables local anvil/surfpool startup and localhost RPC rewrites; when not LOCAL, setup-environment uses testnet-donut chain public_rpc_url values for universal validator RPCs
  ANVIL_SEPOLIA_HOST_RPC_URL=http://localhost:9545
  ANVIL_ARBITRUM_HOST_RPC_URL=http://localhost:9546
  ANVIL_BASE_HOST_RPC_URL=http://localhost:9547
  ANVIL_BSC_HOST_RPC_URL=http://localhost:9548
  LOCAL_SEPOLIA_UV_RPC_URL=http://localhost:9545
  LOCAL_ARBITRUM_UV_RPC_URL=http://localhost:9546
  LOCAL_BASE_UV_RPC_URL=http://localhost:9547
  LOCAL_BSC_UV_RPC_URL=http://localhost:9548
  SURFPOOL_SOLANA_HOST_RPC_URL=http://localhost:8899
  LOCAL_SOLANA_UV_RPC_URL=http://localhost:8899
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
    fund-uea-prc20) step_fund_uea_prc20 ;;
    configure-core) step_configure_universal_core ;;
    check-addresses) assert_required_addresses ;;
    write-core-env) step_write_core_env ;;
    update-token-config) step_update_deployed_token_configs ;;
    setup-gateway) step_setup_gateway ;;
    sync-vault-tss) step_sync_vault_tss_on_anvil ;;
    wait-for-gas-oracle) step_wait_for_gas_oracle ;;
    bootstrap-cea-sdk) step_bootstrap_cea_for_sdk_signer ;;
    deploy-counter-sdk) step_deploy_counter_and_sync_sdk ;;
    clone-sdk) step_clone_push_chain_sdk ;;
    setup-sdk) step_setup_push_chain_sdk ;;
    sdk-test-all) step_run_sdk_tests_all ;;
    sdk-test-outbound-all) step_run_sdk_outbound_tests_all ;;
    quick-testing-outbound) step_run_sdk_quick_testing_outbound ;;
    sdk-test-pctx-last-transaction) step_run_sdk_test_file "pctx-last-transaction.spec.ts" ;;
    sdk-test-send-to-self) step_run_sdk_test_file "send-to-self.spec.ts" ;;
    sdk-test-progress-hook) step_run_sdk_test_file "progress-hook-per-tx.spec.ts" ;;
    sdk-test-bridge-multicall) step_run_sdk_test_file "bridge-multicall.spec.ts" ;;
    sdk-test-pushchain) step_run_sdk_test_file "pushchain.spec.ts" ;;
    sdk-test-bridge-hooks) step_run_sdk_test_file "bridge-hooks.spec.ts" ;;
    sdk-test-cea-to-eoa) step_run_sdk_test_file "cea-to-eoa.spec.ts" ;;
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