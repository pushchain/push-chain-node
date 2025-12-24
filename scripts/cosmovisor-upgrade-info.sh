#!/bin/bash
# Generate Cosmovisor upgrade info JSON with checksums for GitHub releases
#
# Usage:
#   ./scripts/cosmovisor-upgrade-info.sh <version> <github-release-base-url>
#
# Example:
#   ./scripts/cosmovisor-upgrade-info.sh v1.1.0 https://github.com/pushchain/push-chain/releases/download/v1.1.0
#
# This will output JSON that can be used in the upgrade proposal's "info" field.
# The script downloads each archive, calculates its SHA256 checksum, and formats
# the output for Cosmovisor's auto-download feature.
#
# GoReleaser creates archives named: push-chain_<version>_<os>_<arch>.tar.gz
# Cosmovisor will automatically extract tar.gz files.

set -e

VERSION=${1:-""}
RELEASE_URL=${2:-""}
PROJECT_NAME=${PROJECT_NAME:-"push-chain"}

# Strip 'v' prefix for archive filename (GoReleaser doesn't include it)
VERSION_NO_V=${VERSION#v}

if [ -z "$VERSION" ] || [ -z "$RELEASE_URL" ]; then
    echo "Usage: $0 <version> <github-release-base-url>"
    echo ""
    echo "Example:"
    echo "  $0 v1.1.0 https://github.com/pushchain/push-chain/releases/download/v1.1.0"
    echo ""
    echo "Environment variables:"
    echo "  PROJECT_NAME - Name of the project (default: push-chain)"
    exit 1
fi

# Platforms to generate checksums for (matching GoReleaser targets)
# Format: "cosmovisor_platform:goreleaser_suffix"
# Note: darwin/amd64 not supported (no GitHub-hosted macOS Intel runners)
PLATFORMS=(
    "linux/amd64:linux_amd64"
    "linux/arm64:linux_arm64"
    "darwin/arm64:darwin_arm64"
)

# Create temp directory
TEMP_DIR=$(mktemp -d)
trap "rm -rf $TEMP_DIR" EXIT

echo "Generating upgrade info for $VERSION..." >&2
echo "Release URL: $RELEASE_URL" >&2
echo "" >&2

# Start JSON output
echo "{"
echo '  "binaries": {'

FIRST=true
for PLATFORM_MAPPING in "${PLATFORMS[@]}"; do
    COSMO_PLATFORM=$(echo $PLATFORM_MAPPING | cut -d':' -f1)
    GORELEASER_SUFFIX=$(echo $PLATFORM_MAPPING | cut -d':' -f2)

    # GoReleaser archive filename: push-chain_1.0.0_linux_amd64.tar.gz (no 'v' prefix)
    ARCHIVE_FILE="${PROJECT_NAME}_${VERSION_NO_V}_${GORELEASER_SUFFIX}.tar.gz"
    ARCHIVE_URL="${RELEASE_URL}/${ARCHIVE_FILE}"

    echo "Checking $ARCHIVE_FILE..." >&2

    # Download archive to temp location
    TEMP_FILE="$TEMP_DIR/$ARCHIVE_FILE"

    if curl -sL --fail -o "$TEMP_FILE" "$ARCHIVE_URL" 2>/dev/null; then
        # Calculate SHA256
        if command -v sha256sum &> /dev/null; then
            CHECKSUM=$(sha256sum "$TEMP_FILE" | cut -d' ' -f1)
        else
            # macOS uses shasum
            CHECKSUM=$(shasum -a 256 "$TEMP_FILE" | cut -d' ' -f1)
        fi

        echo "  ✓ $COSMO_PLATFORM: $CHECKSUM" >&2

        # Output JSON line
        if [ "$FIRST" = true ]; then
            FIRST=false
        else
            echo ","
        fi
        printf '    "%s": "%s?checksum=sha256:%s"' "$COSMO_PLATFORM" "$ARCHIVE_URL" "$CHECKSUM"
    else
        echo "  ✗ $COSMO_PLATFORM: Not found (skipping)" >&2
    fi
done

echo ""
echo "  }"
echo "}"

echo "" >&2
echo "Done! Copy the JSON above into your upgrade proposal's 'info' field." >&2
echo "" >&2
echo "=== EXAMPLE UPGRADE PROPOSAL ===" >&2
echo "" >&2
cat >&2 << 'EXAMPLE'
Create a file: upgrade-proposal.json

{
  "messages": [{
    "@type": "/cosmos.upgrade.v1beta1.MsgSoftwareUpgrade",
    "authority": "push10d07y265gmmuvt4z0w9aw880jnsr700jzqqyzm",
    "plan": {
      "name": "YOUR_UPGRADE_NAME",
      "height": "TARGET_BLOCK_HEIGHT",
      "info": "<PASTE THE JSON OUTPUT HERE - ESCAPED>"
    }
  }],
  "deposit": "10000000upc",
  "title": "Upgrade to VERSION",
  "summary": "Description of the upgrade"
}

Then submit:
  pchaind tx gov submit-proposal upgrade-proposal.json --from <key> --chain-id <chain-id> --gas auto --gas-prices 1000000000upc --yes
EXAMPLE
