#!/usr/bin/env bash
set -euo pipefail

HOST="${TE_BENCH_HOST:-agent-2.tunneledge.dev}"
PORT="${TE_BENCH_PORT:-443}"
TIMEOUT_SEC="${TE_BENCH_TIMEOUT:-15}"
CONCURRENT="${TE_BENCH_CONCURRENT:-1}"

echo "TunnelEdge HTTP Benchmark"
echo "========================="
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

run_single() {
    local size=$1
    local tmpfile
    tmpfile=$(mktemp /tmp/tunneledge-bench.XXXXXX)

    dd if=/dev/zero bs="${size}" count=1 2>/dev/null | tr '\0' 'X' > "$tmpfile"

    start=$(date +%s%N)

    result=$(curl -sk -w "\n%{http_code} %{time_total}" \
        --max-time "${TIMEOUT_SEC}" \
        -X POST \
        -H "Content-Type: application/octet-stream" \
        --data-binary @"$tmpfile" \
        "https://${HOST}:${PORT}/bench" 2>/dev/null) || true

    end=$(date +%s%N)
    elapsed=$(( (end - start) / 1000000 ))

    http_code=$(echo "$result" | tail -1 | awk '{print $1}')

    if [ -z "$http_code" ] || [ "$http_code" = "000" ]; then
        echo "000 ${elapsed}"
    else
        echo "${http_code} ${elapsed}"
    fi
    rm -f "$tmpfile"
}

for size in 1024 10240 102400 1048576; do
    size_name=$(numfmt --to=iec-i --suffix=B "$size" 2>/dev/null || echo "${size} bytes")
    echo "--- ${size_name} ---"

    results=()
    for ((i = 0; i < CONCURRENT; i++)); do
        results+=("$(run_single "$size")")
    done

    total_time=0
    ok=0
    fail=0
    for r in "${results[@]}"; do
        code=${r%% *}
        ms=${r##* }
        if [ "$code" = "200" ]; then
            ok=$((ok + 1))
            total_time=$((total_time + ms))
        else
            fail=$((fail + 1))
            echo "  FAIL: HTTP ${code} (${ms}ms)"
        fi
    done

    if [ "$ok" -gt 0 ]; then
        avg=$((total_time / ok))
        throughput=$(echo "scale=2; ${size} / (${avg} / 1000)" | bc 2>/dev/null || echo "N/A")
        echo "  Requests:  ${ok} ok, ${fail} failed"
        echo "  Avg time:  ${avg}ms"
        echo "  Throughput: ${throughput} bytes/s"
    else
        echo "  All requests failed"
    fi
    echo ""
done

echo "--- Latency (1 byte payload, 10 requests) ---"
latencies=()
for ((i = 0; i < 10; i++)); do
    r=$(run_single 1)
    code=${r%% *}
    ms=${r##* }
    if [ "$code" = "200" ]; then
        latencies+=("$ms")
    fi
done

if [ ${#latencies[@]} -gt 0 ]; then
    sorted=$(printf '%s\n' "${latencies[@]}" | sort -n)
    min=$(echo "$sorted" | head -1)
    max=$(echo "$sorted" | tail -1)
    count=${#latencies[@]}
    median=$(echo "$sorted" | sed -n "$(( (count + 1) / 2 ))p")
    echo "  Samples: ${count}"
    echo "  Min:     ${min}ms"
    echo "  Median:  ${median}ms"
    echo "  Max:     ${max}ms"
else
    echo "  All requests failed"
fi
