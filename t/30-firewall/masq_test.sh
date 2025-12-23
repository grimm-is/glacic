#!/bin/bash
# set -e removed to handle errors manually

# Path to the Glacic binary
GLACIC_BIN="${GLACIC_BIN:-/mnt/glacic/build/glacic}"
CONFIG_FILE="t/configs/masq_match.hcl"

echo "Running masquerade generation test..."

# Generate the ruleset (show command creates a dry-run from config)
OUTPUT=$($GLACIC_BIN show "$CONFIG_FILE" 2>&1)
EXIT_CODE=$?

if [ $EXIT_CODE -ne 0 ]; then
  echo "FAIL: glacic command failed with exit code $EXIT_CODE"
  echo "$OUTPUT"
  exit 1
fi

if echo "$OUTPUT" | grep -q 'oifname "eth0" masquerade'; then
  echo "PASS: Masquerade rule found for eth0."
else
  echo "FAIL: Masquerade rule NOT found for eth0."
  echo "Ruleset output:"
  echo "$OUTPUT"
  exit 1
fi
