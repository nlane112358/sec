#!/usr/bin/env bash
# ==============================================================================
# buffett_smallcap_10k.sh
# Warren Buffett Small-Cap Compounder Screener & £10,000 Portfolio Allocator
# Built for the sec-main-4 EDGAR CLI Engine
# ==============================================================================

set -euo pipefail

# --- 1. Capital & FX Configuration ---
TOTAL_GBP=10000
GBP_USD_RATE="${GBP_USD_RATE:-1.32385}" # Current GBP/USD rate (override via env if needed)
TOTAL_USD=$(awk -v gbp="$TOTAL_GBP" -v fx="$GBP_USD_RATE" 'BEGIN { printf "%.2f", gbp * fx }')

# Buffett concentrated portfolio settings (5 to 8 high-conviction small-caps)
MAX_POSITIONS="${MAX_POSITIONS:-5}"
UNIVERSE_FILE="${UNIVERSE_FILE:-universes/russell2000.txt}"
OUTPUT_DIR="data/buffett_smallcap_10k"
 mkdir -p "$OUTPUT_DIR"

echo "=================================================================="
echo "  WARREN BUFFETT SMALL-CAP SCREENER (£${TOTAL_GBP} / \$${TOTAL_USD} USD)"
echo "  Universe : ${UNIVERSE_FILE}"
echo "  Positions: Top ${MAX_POSITIONS} Equal/Conviction-Weighted Small-Caps"
echo "=================================================================="

# --- 2. Build the SEC CLI Binary if Missing or Older Than Its Sources ---
NEEDS_BUILD=0
if [ ! -f "./sec" ] || [ go.mod -nt ./sec ] || [ go.sum -nt ./sec ] || \
    [ -n "$(find cmd internal -type f -name '*.go' -newer ./sec -print -quit)" ]; then
    NEEDS_BUILD=1
fi

if [ "$NEEDS_BUILD" -eq 1 ]; then
    echo "[1/5] Building sec CLI binary from updated ./cmd and ./internal sources..."
    go build -o sec ./cmd/sec
else
    echo "[1/5] Using existing ./sec binary..."
fi

# --- 3. Sync Small-Cap Universe Filings & Market Prices ---
# Note: Pass SYNC_LIMIT (e.g., SYNC_LIMIT=200) if you want a fast test run first
SYNC_ARGS=("--universe" "$UNIVERSE_FILE")
if [ -n "${SYNC_LIMIT:-}" ]; then
    SYNC_ARGS+=("--limit" "$SYNC_LIMIT")
fi

echo "[2/5] Using cached SEC fundamentals for ${UNIVERSE_FILE}..."
#./sec sync "${SYNC_ARGS[@]}" || echo "Warning: Using existing cached filings in data/..."

# --- 4. Run Buffett Quality & GAAP-to-Economic Arbitrage Screens ---
# Small-caps often look expensive on GAAP net income due to upfront R&D/customer
# acquisition expensing. We run both standard rank and GAAP arbitrage rank.
echo "[3/5] Ranking small-cap candidates by ROIC, growth & owner-earnings quality..."
./sec rank --universe "$UNIVERSE_FILE" --top "$MAX_POSITIONS" 2>&1 | tee "${OUTPUT_DIR}/top_ranked.txt"

echo "[4/5] Checking GAAP-to-Economic earnings arbitrage on small-cap universe..."
./sec gaap-arbitrage --universe "$UNIVERSE_FILE" --fetch-prices 2>&1 | tee "${OUTPUT_DIR}/gaap_arbitrage.txt"

# --- 5. Run DCF Valuation & Compute £10,000 Allocation Table ---
TOP_TICKERS=$(awk '$1 ~ /^[0-9]+$/ && $2 ~ /^[A-Z0-9.-]+$/ { print $2 }' "${OUTPUT_DIR}/top_ranked.txt" | \
    awk '!seen[$0]++' | head -n "$MAX_POSITIONS" || true)

if [ -z "$TOP_TICKERS" ]; then
    echo "[5/5] No ranked candidates; skipping DCF valuation and position sizing."
    exit 0
fi

echo "[5/5] Generating £${TOTAL_GBP} (£) / \$${TOTAL_USD} ($) Position Sizing..."

ALLOC_GBP_PER_STOCK=$(awk -v tot="$TOTAL_GBP" -v n="$MAX_POSITIONS" 'BEGIN { printf "%.2f", tot / n }')
ALLOC_USD_PER_STOCK=$(awk -v tot="$TOTAL_USD" -v n="$MAX_POSITIONS" 'BEGIN { printf "%.2f", tot / n }')

printf "\n%-8s | %-12s | %-12s | %-28s\n" "TICKER" "ALLOC (£)" "ALLOC ($)" "VALUATION & AUDIT ACTION"
printf -- "---------+--------------+--------------+-----------------------------\n"

for TICKER in $TOP_TICKERS; do
    # Run individual DCF & Forensic Audit into output folder
    ./sec dcf "$TICKER" 2>&1 | tee "${OUTPUT_DIR}/dcf_${TICKER}.txt" || true
    ./sec audit "$TICKER" 2>&1 | tee "${OUTPUT_DIR}/audit_${TICKER}.txt" || true

    printf "%-8s | £%-11s | \$%-11s | Saved DCF & Audit -> %s/\n" \
        "$TICKER" "$ALLOC_GBP_PER_STOCK" "$ALLOC_USD_PER_STOCK" "$OUTPUT_DIR"
done

echo "=================================================================="
echo "Done! Full screening logs and DCF tear sheets saved to: ${OUTPUT_DIR}/"
