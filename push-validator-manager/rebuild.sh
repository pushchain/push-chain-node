#!/usr/bin/env bash
# Simple rebuild script for push-validator

set -e

echo "Building push-validator..."
CGO_ENABLED=0 go build -a -o build/push-validator ./cmd/push-validator

echo "✓ Built: build/push-validator"

# Automatically create wrapper script in system location
# This works around a macOS PATH-execution issue where the binary is killed with SIGKILL
mkdir -p ~/.local/bin

# Get the absolute path to the build directory
BUILD_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/build"

cat > ~/.local/bin/push-validator << EOF
#!/bin/bash
# Wrapper script for push-validator
# Works around macOS PATH-execution issue (SIGKILL when running binary directly from PATH)
exec "$BUILD_DIR/push-validator" "\$@"
EOF

chmod +x ~/.local/bin/push-validator
echo "✓ Created wrapper script at ~/.local/bin/push-validator"
echo ""
echo "You can now run: push-validator dashboard"
