#!/usr/bin/env bash
set -eu

echo "=== DevSpace startup ==="

echo "Downloading Go modules..."
go mod download

echo "Starting agn server..."
exec go run ./cmd/agn serve
