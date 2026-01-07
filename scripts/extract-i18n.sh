#!/bin/bash
set -e

# Ensure output directory exists
mkdir -p internal/i18n/locales

# 1. Extract Go strings (Backend)
echo "Extracting Go strings..."
# We need to tell gotext where to look. It looks for calls to message.Printer.Printf etc.
# Ensure go is in PATH and disable CGO for extraction to avoid cgo errors
export PATH=$PATH:/usr/local/go/bin
export CGO_ENABLED=0

~/go/bin/gotext -srclang=en update -out=internal/i18n/catalog.go -lang=en,de grimm.is/glacic grimm.is/glacic/internal/i18n grimm.is/glacic/cmd grimm.is/glacic/internal/api

# Fix package name in catalog.go (gotext defaults to main if mixed)
sed -i '' 's/package main/package i18n/' internal/i18n/catalog.go

# 2. Extract Svelte strings (Frontend) - Naive implementation for now
echo "Extracting Svelte strings (naive grep)..."
# This finds unique keys used in $t('key', ...)
grep -r "\$t(['\"]\([^'\"]*\)['\"]" ui/src | sed "s/.*\$t(['\"]\([^'\"]*\)['\"].*/\1/" | sort | uniq > ui/src/locales/keys_found.txt

echo "Done. Backend catalog updated at internal/i18n/catalog.go. Frontend keys listed in ui/src/locales/keys_found.txt"
