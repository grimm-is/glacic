#!/bin/bash
set -e

# Usage: ./build_tests.sh <GOOS> <GOARCH>
GOOS=${1:-linux}
GOARCH=${2:-amd64}

# Output directory: build/tests/<GOOS>/<GOARCH>
OUT_DIR="build/tests/${GOOS}/${GOARCH}"
mkdir -p "$OUT_DIR"

echo "Building test binaries for ${GOOS}/${GOARCH} into ${OUT_DIR}..."

# Find all internal packages
PKGS=$(go list ./internal/...)

# Function to build a single package
build_pkg() {
    local pkg=$1
    local out_dir=$2
    local goos=$3
    local goarch=$4

    local name=$(basename "$pkg")
    local out_path="${out_dir}/${name}.test"

    # We use go test -c which leverages the build cache.
    if ! output=$(CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go test -c -o "$out_path" "$pkg" 2>&1); then
        # Check if failure is due to no test files
        if echo "$output" | grep -q "no test files"; then
            return 0
        fi
        echo ""
        echo "Failed to build $pkg:"
        echo "$output"
        return 1
    fi
    echo -n "."
}

export -f build_pkg

# Use parallel execution
# We need to pass the function and variables to the subshell
# xargs -P 0 uses all available cores
echo "$PKGS" | xargs -P 0 -I {} bash -c "build_pkg '{}' '$OUT_DIR' '$GOOS' '$GOARCH'"

echo ""
echo "Test build complete."
