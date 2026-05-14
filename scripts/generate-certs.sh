#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
CERTS_DIR="${ROOT_DIR}/certs"

mkdir -p "${CERTS_DIR}"

echo "Generating self-signed TLS certificates..."

openssl req -x509 -newkey rsa:2048 \
  -keyout "${CERTS_DIR}/server.key" \
  -out "${CERTS_DIR}/server.crt" \
  -days 365 \
  -nodes \
  -subj "/CN=tunneledge-dev" \
  -addext "subjectAltName=DNS:localhost,DNS:gateway,DNS:*.localhost,IP:127.0.0.1,IP:::1"

echo "Certificates generated in ${CERTS_DIR}/"
