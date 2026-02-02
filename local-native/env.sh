#!/bin/bash
# Environment configuration for local-native setup

# Chain configuration
export CHAIN_ID="localchain_9000-1"
export EVM_CHAIN_ID="9000"
export DENOM="upc"
export KEYRING="test"
export KEYALGO="eth_secp256k1"

# Paths - relative to local-native directory
export LOCAL_NATIVE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
export DATA_DIR="$LOCAL_NATIVE_DIR/data"
export ACCOUNTS_DIR="$DATA_DIR/accounts"

# Binary paths (built from source)
export PCHAIND_BIN="$LOCAL_NATIVE_DIR/../build/pchaind"
export PUNIVERSALD_BIN="$LOCAL_NATIVE_DIR/../build/puniversald"

# Genesis funding amounts
export TWO_BILLION="2000000000000000000000000000"
export ONE_MILLION="1000000000000000000000000"
export VALIDATOR_STAKE="100000000000000000000000"
export HOTKEY_FUNDING="10000000000000000000000"
