#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/../.." && pwd)"

echo "Running unit tests..."
cd "${ROOT_DIR}" && go test -count=1 -v ./internal/...

echo ""
echo "All tests complete."
