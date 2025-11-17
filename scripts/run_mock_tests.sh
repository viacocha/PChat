#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

echo "Running mock host tests..."
echo ""
echo "1. Testing handleStream with mock hosts..."
go test ./cmd/pchat -run TestHandleStreamWithMockHost -v

echo ""
echo "2. Testing file transfer between mock hosts..."
go test ./cmd/pchat -run TestSendFileBetweenMockHosts -v

echo ""
echo "3. Testing multi-node DHT/CLI command flow..."
go test ./cmd/pchat -run TestMultiNodeDHTCommandFlow -v

echo ""
echo "All mock host tests completed!"

