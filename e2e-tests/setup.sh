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
: "${KEYRING_BACKEND:=test}"
: "${GENESIS_KEY_NAME:=genesis-acc-1}"
: "${GENESIS_KEY_HOME:=$PUSH_CHAIN_DIR/local-native/data/validator1/.pchain}"
: "${GENESIS_ACCOUNTS_JSON:=$PUSH_CHAIN_DIR/local-native/data/accounts/genesis_accounts.json}"
: "${FUND_AMOUNT:=1000000000000000000upc}"
: "${GAS_PRICES:=100000000000upc}"

: "${CORE_CONTRACTS_REPO:=https://github.com/pushchain/push-chain-core-contracts.git}"
: "${CORE_CONTRACTS_BRANCH:=e2e-push-node}"
: "${SWAP_AMM_REPO:=https://github.com/pushchain/push-chain-swap-internal-amm-contracts.git}"
: "${SWAP_AMM_BRANCH:=e2e-push-node}"
: "${GATEWAY_REPO:=https://github.com/pushchain/push-chain-gateway-contracts.git}"
: "${GATEWAY_BRANCH:=e2e-push-node}"

: "${E2E_PARENT_DIR:=../}"
: "${CORE_CONTRACTS_DIR:=$E2E_PARENT_DIR/push-chain-core-contracts}"
: "${SWAP_AMM_DIR:=$E2E_PARENT_DIR/push-chain-swap-internal-amm-contracts}"
: "${GATEWAY_DIR:=$E2E_PARENT_DIR/push-chain-gateway-contracts}"
: "${DEPLOY_ADDRESSES_FILE:=$SCRIPT_DIR/deploy_addresses.json}"
: "${LOG_DIR:=$SCRIPT_DIR/logs}"
: "${TEST_ADDRESSES_PATH:=$SWAP_AMM_DIR/test-addresses.json}"
: "${TOKENS_CONFIG_DIR:=./config/testnet-donut/tokens}"
: "${TOKEN_CONFIG_PATH:=./config/testnet-donut/tokens/eth_sepolia_eth.json}"
: "${CHAIN_CONFIG_PATH:=./config/testnet-donut/chains/eth_sepolia_chain_config.json}"

: "${OLD_PUSH_ADDRESS:=push1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20}"
: "${NEW_PUSH_ADDRESS:=push1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20}"
: "${OLD_EVM_ADDRESS:=0x778D3206374f8AC265728E18E3fE2Ae6b93E4ce4}"
: "${NEW_EVM_ADDRESS:=0x778D3206374f8AC265728E18E3fE2Ae6b93E4ce4}"

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
E2E_PARENT_DIR="$(abs_from_root "$E2E_PARENT_DIR")"
CORE_CONTRACTS_DIR="$(abs_from_root "$CORE_CONTRACTS_DIR")"
SWAP_AMM_DIR="$(abs_from_root "$SWAP_AMM_DIR")"
GATEWAY_DIR="$(abs_from_root "$GATEWAY_DIR")"
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
  if [[ ! -f "$DEPLOY_ADDRESSES_FILE" ]]; then
    cat >"$DEPLOY_ADDRESSES_FILE" <<'JSON'
{
  "generatedAt": "",
  "contracts": {},
  "tokens": []
}
JSON
  fi
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
    log_info "Updating repo $(basename "$dest")"
    git -C "$dest" fetch origin
    git -C "$dest" checkout "$resolved_branch"
    git -C "$dest" reset --hard "origin/$resolved_branch"
  else
    log_info "Cloning $(basename "$dest")"
    git clone --branch "$resolved_branch" "$repo_url" "$dest"
  fi
}

step_devnet() {
  require_cmd bash
  log_info "Starting local-native devnet"
  (
    cd "$PUSH_CHAIN_DIR/local-native"
    ./devnet build
    ./devnet start 4
    ./devnet setup-uvalidators
    ./devnet start-uv 4
  )
  log_ok "Devnet is up"
}

step_print_genesis() {
  require_cmd jq
  if [[ ! -f "$GENESIS_ACCOUNTS_JSON" ]]; then
    log_err "Missing genesis accounts file: $GENESIS_ACCOUNTS_JSON"
    exit 1
  fi

  jq -r '.[0] | "Account: \(.name)\nAddress: \(.address)\nMnemonic: \(.mnemonic)"' "$GENESIS_ACCOUNTS_JSON"
}

step_recover_genesis_key() {
  require_cmd "$PUSH_CHAIN_DIR/build/pchaind" jq

  local mnemonic="${GENESIS_MNEMONIC:-}"
  if [[ -z "$mnemonic" ]]; then
    if [[ -f "$GENESIS_ACCOUNTS_JSON" ]]; then
      mnemonic="$(jq -r --arg n "$GENESIS_KEY_NAME" '
        (first(.[] | select(.name == $n) | .mnemonic) // first(.[].mnemonic) // "")
      ' "$GENESIS_ACCOUNTS_JSON")"
    fi
  fi

  if [[ -z "$mnemonic" ]]; then
    log_err "Could not auto-resolve mnemonic from $GENESIS_ACCOUNTS_JSON"
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
      log_err "Missing required address in deploy file: contracts.$key"
      missing=1
    else
      log_ok "contracts.$key=$val"
    fi
  done

  if [[ "$missing" -ne 0 ]]; then
    log_err "Required addresses are missing in $DEPLOY_ADDRESSES_FILE"
    exit 1
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
  for file in "$TOKENS_CONFIG_DIR"/*.json; do
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
  done

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

  [[ -f "$CHAIN_CONFIG_PATH" ]] || { log_err "Missing chain config: $CHAIN_CONFIG_PATH"; exit 1; }
  [[ -d "$TOKENS_CONFIG_DIR" ]] || { log_err "Missing tokens config directory: $TOKENS_CONFIG_DIR"; exit 1; }

  # Ensure all deployed core tokens have updated contract addresses in token config files.
  step_update_deployed_token_configs

  local chain_payload token_payload
  chain_payload="$(jq -c . "$CHAIN_CONFIG_PATH")"

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
          --home data/validator1/.pchain \
          --node tcp://127.0.0.1:26657 \
          --gas-prices "$GAS_PRICES" \
          -y)"
      else
        out="$("$PUSH_CHAIN_DIR/build/pchaind" tx uregistry add-token-config \
          --token-config "$payload" \
          --from "$GENESIS_KEY_NAME" \
          --keyring-backend "$KEYRING_BACKEND" \
          --home data/validator1/.pchain \
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

  log_info "Adding chain config to uregistry"
  (
    cd "$PUSH_CHAIN_DIR/local-native"
    run_registry_tx "chain" "$chain_payload"
  )

  local deployed_addrs token_file token_addr matched_count
  deployed_addrs="$(jq -r '.tokens[]?.address | ascii_downcase' "$DEPLOY_ADDRESSES_FILE")"
  matched_count=0

  while IFS= read -r token_file; do
    [[ -f "$token_file" ]] || continue
    token_addr="$(jq -r '.native_representation.contract_address // "" | ascii_downcase' "$token_file")"
    [[ -n "$token_addr" ]] || continue

    if echo "$deployed_addrs" | grep -Fxq "$token_addr"; then
      token_payload="$(jq -c . "$token_file")"
      log_info "Adding token config to uregistry: $(basename "$token_file")"
      (
        cd "$PUSH_CHAIN_DIR/local-native"
        run_registry_tx "token" "$token_payload"
      )
      matched_count=$((matched_count + 1))
    fi
  done < <(find "$TOKENS_CONFIG_DIR" -maxdepth 1 -type f -name '*.json' | sort)

  if [[ "$matched_count" -eq 0 ]]; then
    log_warn "No deployed tokens matched token config files for uregistry add-token-config"
  else
    log_ok "Registered $matched_count deployed token config(s) in uregistry"
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
  require_cmd node
  ensure_deploy_file

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

step_replace_addresses_everywhere() {
  require_cmd grep perl

  local touched=0
  local file

  while IFS= read -r file; do
    [[ -n "$file" ]] || continue
    perl -0777 -i -pe "s/\Q$OLD_PUSH_ADDRESS\E/$NEW_PUSH_ADDRESS/g; s/\Q$OLD_EVM_ADDRESS\E/$NEW_EVM_ADDRESS/g;" "$file"
    touched=$((touched + 1))
  done < <(
    grep -RIl \
      --exclude-dir=.git \
      --binary-files=without-match \
      -e "$OLD_PUSH_ADDRESS" \
      -e "$OLD_EVM_ADDRESS" \
      "$PUSH_CHAIN_DIR" || true
  )

  if [[ "$touched" -eq 0 ]]; then
    log_warn "No files contained legacy addresses"
  else
    log_ok "Replaced legacy addresses in $touched file(s)"
  fi
}

run_preflight() {
  local cmd="$1"

  case "$cmd" in
    help|--help|-h|replace-addresses)
      return 0
      ;;
  esac

  step_replace_addresses_everywhere
}

cmd_all() {
  step_devnet
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
  devnet                 Build/start local-native devnet + uvalidators
  print-genesis          Print first genesis account + mnemonic
  recover-genesis-key    Recover genesis key into local keyring
  fund                   Fund FUND_TO_ADDRESS from genesis key
  setup-core             Clone/build/setup core contracts (auto resume on failure)
  setup-swap             Clone/install/deploy swap AMM contracts
  sync-addresses         Apply deploy_addresses.json into test-addresses.json
  create-pool            Create WPC pools for all deployed core tokens
  configure-core         Run configureUniversalCore.s.sol (auto --resume retries)
  replace-addresses      Replace legacy push/evm addresses across repo
  check-addresses        Verify required deploy addresses exist (WPC/Factory/QuoterV2/SwapRouter)
  write-core-env         Create core-contracts .env from deploy_addresses.json
  update-token-config    Update eth_sepolia_eth.json contract_address using deployed token
  setup-gateway          Clone/setup gateway repo and run forge localSetup (with --resume retry)
  add-uregistry-configs  Submit chain + token config txs via local-native validator1
  record-contract K A    Manually record contract key/address
  record-token N S A     Manually record token name/symbol/address
  all                    Run full setup pipeline
  help                   Show this help

Primary files:
  Env:     $ENV_FILE
  Address: $DEPLOY_ADDRESSES_FILE
EOF
}

main() {
  local cmd="${1:-help}"
  run_preflight "$cmd"
  case "$cmd" in
    devnet) step_devnet ;;
    print-genesis) step_print_genesis ;;
    recover-genesis-key) step_recover_genesis_key ;;
    fund) step_fund_account ;;
    setup-core) step_setup_core_contracts ;;
    setup-swap) step_setup_swap_amm ;;
    sync-addresses) step_sync_test_addresses ;;
    create-pool) step_create_all_wpc_pools ;;
    configure-core) step_configure_universal_core ;;
    replace-addresses) step_replace_addresses_everywhere ;;
    check-addresses) assert_required_addresses ;;
    write-core-env) step_write_core_env ;;
    update-token-config) step_update_deployed_token_configs ;;
    setup-gateway) step_setup_gateway ;;
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
