#!/bin/sh
set -e

# Ensure dependencies
if ! command -v scc >/dev/null 2>&1; then
    echo "Error: 'scc' not found. Please install it (e.g., go install github.com/boyter/scc/v3@latest)"
    exit 1
fi

if ! command -v jq >/dev/null 2>&1; then
    echo "Error: 'jq' not found. Please install it (e.g., brew install jq)"
    exit 1
fi

echo "Scanning..."

# Get Code LOC (excluding tests and vendor)
# We use a temporary file to debug if needed, but direct pipe is fine if tools work
APP_LOC=$(scc . --exclude-dir vendor --exclude-file "_test.go" --format json 2>/dev/null | jq -r '[.[] | select(.Name=="Go") | .Code] | add // 0')

# Get Total LOC (including tests)
TOTAL_LOC=$(scc . --exclude-dir vendor --format json 2>/dev/null | jq -r '[.[] | select(.Name=="Go") | .Code] | add // 0')

# Default to 0 if empty
APP_LOC=${APP_LOC:-0}
TOTAL_LOC=${TOTAL_LOC:-0}

# Calculate Test LOC
TEST_LOC=$((TOTAL_LOC - APP_LOC))

# Calculate Ratio
RATIO_STR="0:0"
if [ "$APP_LOC" -gt 0 ] && [ "$TEST_LOC" -gt 0 ]; then
    # Simple GCD implementation in shell/awk
    GCD=$(awk -v a="$TEST_LOC" -v b="$APP_LOC" 'BEGIN { while(b){t=b; b=a%b; a=t} print a }')
    S_TEST=$((TEST_LOC / GCD))
    S_APP=$((APP_LOC / GCD))
    RATIO_STR="$S_TEST:$S_APP"
elif [ "$APP_LOC" -gt 0 ]; then
    RATIO_STR="0:1"
elif [ "$TEST_LOC" -gt 0 ]; then
    RATIO_STR="1:0"
fi

# Calculate Reading Time (25 lines/min)
TOTAL_MINUTES=$((TOTAL_LOC / 25))
HOURS=$((TOTAL_MINUTES / 60))
MINUTES=$((TOTAL_MINUTES % 60))

TIME_STR="${MINUTES}m"
if [ "$HOURS" -gt 0 ]; then
    TIME_STR="${HOURS}h ${MINUTES}m"
fi

echo "-----------------------------------"
echo "Application LOC: $APP_LOC"
echo "Test LOC:        $TEST_LOC"
echo "Test Ratio:      $RATIO_STR (Test:Code)"
echo "Reading Time:    $TIME_STR (@25 lines/min)"
echo "-----------------------------------"
