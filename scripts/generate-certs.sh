#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
CERTS_DIR="${ROOT_DIR}/certs"

mkdir -p "${CERTS_DIR}"

echo "==> Generating TLS certificates for TunnelEdge (mTLS-ready)"

# ── 1. Certificate Authority ──────────────────────────────────────────────────
echo "--> CA key and self-signed certificate"
openssl genrsa -out "${CERTS_DIR}/ca.key" 4096
openssl req -x509 -new -nodes \
  -key "${CERTS_DIR}/ca.key" \
  -sha256 -days 3650 \
  -out "${CERTS_DIR}/ca.crt" \
  -subj "/CN=TunnelEdge-CA/O=TunnelEdge/C=US"

# ── Helper: sign_cert <name> <san_csv> ───────────────────────────────────────
sign_cert() {
  local name="$1"
  local san="$2"
  echo "--> ${name} certificate"
  openssl genrsa -out "${CERTS_DIR}/${name}.key" 2048
  openssl req -new \
    -key "${CERTS_DIR}/${name}.key" \
    -out "${CERTS_DIR}/${name}.csr" \
    -subj "/CN=${name}/O=TunnelEdge/C=US"
  openssl x509 -req -days 825 \
    -in "${CERTS_DIR}/${name}.csr" \
    -CA "${CERTS_DIR}/ca.crt" \
    -CAkey "${CERTS_DIR}/ca.key" \
    -CAcreateserial \
    -out "${CERTS_DIR}/${name}.crt" \
    -extfile <(printf "subjectAltName=%s\nextendedKeyUsage=serverAuth,clientAuth\n" "${san}")
  rm -f "${CERTS_DIR}/${name}.csr"
}

# ── 2. Gateway server certificate (presented to public clients and agents) ────
sign_cert "gateway" "DNS:localhost,DNS:gateway,DNS:*.localhost,IP:127.0.0.1,IP:::1"

# ── 3. Registry server certificate (gRPC TLS) ────────────────────────────────
sign_cert "registry" "DNS:localhost,DNS:registry,IP:127.0.0.1"

# ── 4. Agent client certificate (mTLS — presented to gateway) ────────────────
sign_cert "agent" "DNS:agent,IP:127.0.0.1"

# ── 5. Gateway-to-Registry client certificate (mTLS) ─────────────────────────
sign_cert "gateway-registry-client" "DNS:gateway,IP:127.0.0.1"

# ── Legacy self-signed cert for quick dev without mTLS ───────────────────────
echo "--> legacy server.key/server.crt (dev, self-signed)"
openssl req -x509 -newkey rsa:2048 \
  -keyout "${CERTS_DIR}/server.key" \
  -out "${CERTS_DIR}/server.crt" \
  -days 365 \
  -nodes \
  -subj "/CN=tunneledge-dev" \
  -addext "subjectAltName=DNS:localhost,DNS:gateway,DNS:*.localhost,IP:127.0.0.1,IP:::1"

echo ""
echo "Certificates generated in ${CERTS_DIR}/"
echo ""
echo "mTLS quick-start:"
echo "  Gateway  : cert=${CERTS_DIR}/gateway.crt key=${CERTS_DIR}/gateway.key clientCA=${CERTS_DIR}/ca.crt"
echo "  Registry : cert=${CERTS_DIR}/registry.crt key=${CERTS_DIR}/registry.key clientCA=${CERTS_DIR}/ca.crt"
echo "  Agent    : cert=${CERTS_DIR}/agent.crt key=${CERTS_DIR}/agent.key ca=${CERTS_DIR}/ca.crt"
