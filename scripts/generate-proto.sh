#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
PROTO_DIR="${ROOT_DIR}/proto"

echo "Generating Go code from protobuf..."

protoc \
  --go_out="${ROOT_DIR}" \
  --go_opt=paths=source_relative \
  --go-grpc_out="${ROOT_DIR}" \
  --go-grpc_opt=paths=source_relative \
  -I "${ROOT_DIR}" \
  "${PROTO_DIR}/registry/v1/registry.proto"

echo "Done."
