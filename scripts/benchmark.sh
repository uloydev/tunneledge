#!/usr/bin/env bash
set -euo pipefail

HOST="${TE_BENCH_HOST:-api.agent-1.tunneledge.dev}"
PORT="${TE_BENCH_PORT:-443}"
TIMEOUT_SEC="${TE_BENCH_TIMEOUT:-360}"   # Increased from 15 to 30 for concurrent load
CONCURRENT="${TE_BENCH_CONCURRENT:-500}"

if ! command -v k6 &>/dev/null; then
    echo "error: 'k6' is not installed — install from https://k6.io/docs/get-started/installation/"
    exit 1
fi

echo "TunnelEdge HTTP Benchmark (k6)"
echo "================================"
echo "Target:   https://${HOST}:${PORT}"
echo "Workers:  ${CONCURRENT}"
echo ""

echo "--- Connectivity Check ---"
check=$(curl -sk -o /dev/null -w "%{http_code}" --max-time 5 "https://${HOST}:${PORT}/" 2>&1) || true
if echo "$check" | grep -qE "^[23]|404|405"; then
    echo "  OK (HTTP ${check})"
elif echo "$check" | grep -q "000"; then
    echo "  FAILED — cannot reach ${HOST}:${PORT}"
    echo ""
    echo "  Troubleshooting:"
    echo "    1. Are all services running? (make run-registry, make run-gateway, make run-agent)"
    echo "    2. Is http-echo running on the agent's local_addr? (go run ./cmd/http-echo)"
    echo "    3. Does ${HOST} resolve? (grep ${HOST} /etc/hosts)"
    echo ""
    echo "  Raw curl output:"
    curl -skv --max-time 5 "https://${HOST}:${PORT}/" 2>&1 | tail -5 || true
    exit 1
else
    echo "  HTTP ${check}"
fi
echo ""

# ── Generate k6 Test Script ─────────────────────────────────────────
K6_SCRIPT=$(mktemp /tmp/tunneledge-bench.XXXXXX.js)
cat <<'K6EOF' > "$K6_SCRIPT"
import http from 'k6/http';
import { check } from 'k6';

export const options = {
    insecureSkipTLSVerify: true,
    // Use a scenario that gradually ramps up to prevent overwhelming the 
    // server instantly (Thundering Herd) which causes request timeouts.
    scenarios: {
        benchmark: {
            executor: 'ramping-vus',
            startVUs: 0,
            stages: [
                { duration: '10s', target: parseInt(__ENV.CONCURRENT) }, // Ramp up
                { duration: '15s', target: parseInt(__ENV.CONCURRENT) }, // Sustain peak load
            ],
            gracefulStop: '5s',
        },
    },
};

const size = parseInt(__ENV.PAYLOAD_SIZE);
const payload = 'X'.repeat(size);

export default function () {
    const params = {
        headers: { 'Content-Type': 'application/octet-stream' },
        timeout: `${__ENV.TIMEOUT_SEC}s`,
    };

    const res = http.post(`https://${__ENV.HOST}:${__ENV.PORT}/bench`, payload, params);
    
    check(res, {
        'is status 200': (r) => r.status === 200,
    });
}
K6EOF

# ── Throughput tests ────────────────────────────────────────────────
for size in 1024 10240 102400 1048576; do
    size_name=$(numfmt --to=iec-i --suffix=B "$size" 2>/dev/null || echo "${size} bytes")
    echo "--- ${size_name} ---"

    k6 run "$K6_SCRIPT" \
        --no-color \
        -e HOST="$HOST" \
        -e PORT="$PORT" \
        -e CONCURRENT="$CONCURRENT" \
        -e TIMEOUT_SEC="$TIMEOUT_SEC" \
        -e PAYLOAD_SIZE="$size" || true

    echo ""
done

rm -f "$K6_SCRIPT"