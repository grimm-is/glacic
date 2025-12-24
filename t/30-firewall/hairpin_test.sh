#!/bin/bash

# Path to the Glacic binary
# Path to the Glacic binary
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
. "$SCRIPT_DIR/../common.sh"
GLACIC_BIN="$APP_BIN"
CONFIG_FILE="t/configs/hairpin.hcl"

echo "Running Hairpin NAT generation test..."

# Generate the ruleset
OUTPUT=$($GLACIC_BIN show "$CONFIG_FILE" 2>&1)
EXIT_CODE=$?

if [ $EXIT_CODE -ne 0 ]; then
  echo "FAIL: glacic command failed with exit code $EXIT_CODE"
  echo "$OUTPUT"
  exit 1
fi

# Check for DNAT reflection (should NOT utilize iifname restriction for hairpin)
# We expect a rule that matches dnat WITHOUT iifname restriction OR implies internal access
# Actually, standard Hairpin implementation:
# 1. DNAT rule for WAN IP from LAN interface (or any interface)
# 2. SNAT (Masquerade) rule for traffic from LAN to Server IP

# Current incorrect behavior (L1): Generates `iifname "eth0" ... dnat`
# Expected behavior (L4):
# - Should see `dnat` rule for port 80 targeting 192.168.1.10:8080 that applies to LAN (eth1)
# - Should see `masquerade` rule for traffic from LAN to 192.168.1.10

FAILURES=0

# check for LAN masquerade
# Implementation uses iifname != "eth0" (WAN) to catch all internal traffic
if echo "$OUTPUT" | grep -q 'iifname != "eth0" ip daddr 192.168.1.10.*masquerade'; then
  echo "PASS: Hairpin masquerade rule found."
else
  echo "FAIL: Hairpin masquerade rule NOT found."
  FAILURES=$((FAILURES + 1))
fi

# check for reflected DNAT
# Expect: iifname != "eth0" ... dnat to ...
if echo "$OUTPUT" | grep -q 'iifname != "eth0" .* dnat to 192.168.1.10:8080'; then
  echo "PASS: Reflected DNAT rule found."
else
  echo "FAIL: Reflected DNAT rule NOT found."
  FAILURES=$((FAILURES + 1))
fi

if [ $FAILURES -gt 0 ]; then
  echo "Test Failed with $FAILURES errors."
  exit 1
else
  echo "ALL PASS"
  exit 0
fi
