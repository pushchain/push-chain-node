#!/usr/bin/env bash

set -euo pipefail

OLD_BECH32="push1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20"
NEW_BECH32="push1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20"
OLD_HEX="0x778D3206374f8AC265728E18E3fE2Ae6b93E4ce4"
NEW_HEX="0x778D3206374f8AC265728E18E3fE2Ae6b93E4ce4"

ROOT_DIR="$(cd -P "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

if ! command -v git >/dev/null 2>&1; then
  echo "git command not found" >&2
  exit 1
fi

if ! command -v perl >/dev/null 2>&1; then
  echo "perl command not found" >&2
  exit 1
fi

git ls-files -z | xargs -0 perl -pi -e "s/\Q$OLD_BECH32\E/$NEW_BECH32/g; s/\Q$OLD_HEX\E/$NEW_HEX/g"

echo "Address replacement completed in tracked files."
