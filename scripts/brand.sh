#!/bin/bash
# Brand helper script - reads brand.json for shell scripts
# Usage: source scripts/brand.sh
#    or: eval "$(scripts/brand.sh)"
#    or: scripts/brand.sh <key>  # Get single value

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
BRAND_FILE="$PROJECT_ROOT/internal/brand/brand.json"

if [[ ! -f "$BRAND_FILE" ]]; then
    echo "Error: brand.json not found at $BRAND_FILE" >&2
    exit 1
fi

# If a key is provided, return just that value
if [[ -n "$1" ]]; then
    jq -r ".$1 // empty" "$BRAND_FILE"
    exit 0
fi

# Otherwise, export all brand variables for sourcing
# Uses BRAND_ prefix to avoid conflicts
cat << EOF
export BRAND_NAME="$(jq -r '.name' "$BRAND_FILE")"
export BRAND_LOWER_NAME="$(jq -r '.lowerName' "$BRAND_FILE")"
export BRAND_VENDOR="$(jq -r '.vendor' "$BRAND_FILE")"
export BRAND_WEBSITE="$(jq -r '.website' "$BRAND_FILE")"
export BRAND_REPOSITORY="$(jq -r '.repository' "$BRAND_FILE")"
export BRAND_DESCRIPTION="$(jq -r '.description' "$BRAND_FILE")"
export BRAND_TAGLINE="$(jq -r '.tagline' "$BRAND_FILE")"
export BRAND_CONFIG_ENV_PREFIX="$(jq -r '.configEnvPrefix' "$BRAND_FILE")"
export BRAND_DEFAULT_CONFIG_DIR="$(jq -r '.defaultConfigDir' "$BRAND_FILE")"
export BRAND_DEFAULT_STATE_DIR="$(jq -r '.defaultStateDir' "$BRAND_FILE")"
export BRAND_DEFAULT_LOG_DIR="$(jq -r '.defaultLogDir' "$BRAND_FILE")"
export BRAND_BINARY_NAME="$(jq -r '.binaryName' "$BRAND_FILE")"
export BRAND_SERVICE_NAME="$(jq -r '.serviceName' "$BRAND_FILE")"
export BRAND_CONFIG_FILE_NAME="$(jq -r '.configFileName' "$BRAND_FILE")"
export BRAND_COPYRIGHT="$(jq -r '.copyright' "$BRAND_FILE")"
export BRAND_LICENSE="$(jq -r '.license' "$BRAND_FILE")"
EOF
