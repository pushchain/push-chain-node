#!/bin/bash
set -euo pipefail


SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$SCRIPT_DIR/../../.."

# 🔧 Patch chain ID inside app/app.go
APP_FILE="$ROOT_DIR/universalClient/constant/constant.go"

# Verify the file exists
if [[ ! -f "$APP_FILE" ]]; then
  echo "Error: $APP_FILE not found!"
  exit 1
fi

echo "Patching $APP_FILE ..."

if sed --version >/dev/null 2>&1; then
  SED_INPLACE=(sed -i)
else
  # macOS/BSD sed
  SED_INPLACE=(sed -i '')
fi

# 1) Replace os.ExpandEnv("$HOME/") with "/home/universal/"
"${SED_INPLACE[@]}" 's|os\.ExpandEnv("\$HOME/")|"/home/universal/"|g' "$APP_FILE"

# 2) Remove `import "os"` line if present
"${SED_INPLACE[@]}" '/^[[:space:]]*import[[:space:]]*"os"[[:space:]]*$/d' "$APP_FILE"

echo "Patch complete"
