#!/usr/bin/env bash
# ==============================================================================
# HOW WARREN BUFFETT WOULD USE THIS TOOL: NASDAQ 100 COMPOUNDER SEARCH
# ==============================================================================
# "It's far better to buy a wonderful company at a fair price than a fair
#  company at a wonderful price."
#   — Warren Buffett, Berkshire Hathaway Shareholder Letter (1989)
#
# This script executes the complete 5-Step Warren Buffett Playbook and 3-Gate
# Decision Engine using primary SEC EDGAR facts for the NASDAQ 100 universe.
# ==============================================================================

set -euo pipefail

# ------------------------------------------------------------------------------
# Script Environment & Defaults
# ------------------------------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SEC_BIN="${SEC_BIN:-"${SCRIPT_DIR}/sec"}"
UNIVERSE="${UNIVERSE:-"${SCRIPT_DIR}/universes/nasdaq100.txt"}"
DATA_DIR="${DATA_DIR:-"${SCRIPT_DIR}/data"}"
SEC_USER_AGENT="${SEC_USER_AGENT:-"BuffettResearch contact@berkshirehathaway.com"}"
export SEC_USER_AGENT

# Screening Parameters (Warren Buffett & Berkshire Hathaway Standards)
MIN_REVENUE=100000000        # $100M revenue hurdle
YEARS=6                      # 6 annual periods for 5-year multi-period trend
MIN_SCORE=75                 # Top quartile composite quality (0-100)
MIN_ROIC=15                  # 15% 5Y multi-year ROIC hurdle
MIN_ADJ_ROIC=18              # 18% Intangible-Adjusted ROIC (Peters & Taylor / Damodaran)
MIN_YIELD=3.5                # 3.5% Owner Earnings Yield hurdle
DCF_DISCOUNT=9.0             # Conservative Buffett discount hurdle rate (%)
DCF_TERMINAL=2.5             # Long-term GDP terminal growth rate (%)
DEFAULT_TOP=3                # Default number of candidates to audit in Step 5

# Runtime control flags
SKIP_SYNC=0
SYNC_ONLY=0
STEP_FILTER="all"
INTERACTIVE=0
NO_COLOR=0
REPORT_FILE=""
OUTPUT_DIR=""
declare -a MANUAL_TICKERS=()

# ------------------------------------------------------------------------------
# ANSI Color Codes & UI Formatters
# ------------------------------------------------------------------------------
setup_colors() {
    if [[ "${NO_COLOR}" -eq 1 ]] || [[ ! -t 1 && -z "${FORCE_COLOR:-}" ]]; then
        C_RESET=""
        C_BOLD=""
        C_DIM=""
        C_RED=""
        C_GREEN=""
        C_YELLOW=""
        C_BLUE=""
        C_MAGENTA=""
        C_CYAN=""
        C_WHITE=""
    else
        C_RESET="\033[0m"
        C_BOLD="\033[1m"
        C_DIM="\033[2m"
        C_RED="\033[1;31m"
        C_GREEN="\033[1;32m"
        C_YELLOW="\033[1;33m"
        C_BLUE="\033[1;34m"
        C_MAGENTA="\033[1;35m"
        C_CYAN="\033[1;36m"
        C_WHITE="\033[1;37m"
    fi
}

print_banner() {
    echo -e "${C_CYAN}"
    cat << 'EOF'
 _______________________________________________________________________________
|                                                                               |
|   HOW WARREN BUFFETT WOULD USE THIS TOOL: NASDAQ 100 COMPOUNDER ENGINE        |
|_______________________________________________________________________________|
EOF
    echo -e "${C_YELLOW}  \"It's far better to buy a wonderful company at a fair price"
    echo -e "   than a fair company at a wonderful price.\""
    echo -e "${C_DIM}   — Warren Buffett, Berkshire Hathaway Shareholder Letter (1989)${C_RESET}\n"
}

print_section() {
    local title="$1"
    local subtitle="$2"
    echo -e "\n${C_BOLD}${C_BLUE}══════════════════════════════════════════════════════════════════════════════${C_RESET}"
    echo -e "${C_BOLD}${C_WHITE} ${title}${C_RESET}"
    if [[ -n "${subtitle}" ]]; then
        echo -e "${C_DIM} ${subtitle}${C_RESET}"
    fi
    echo -e "${C_BOLD}${C_BLUE}══════════════════════════════════════════════════════════════════════════════${C_RESET}\n"
}

print_gate_box() {
    local gate_num="$1"
    local gate_name="$2"
    local criteria="$3"
    echo -e "${C_MAGENTA}┌─────────────────────────────────────────────────────────────────────────────┐${C_RESET}"
    echo -e "${C_MAGENTA}│${C_RESET} ${C_BOLD}GATE ${gate_num}: ${gate_name}${C_RESET}"
    echo -e "${C_MAGENTA}│${C_RESET} ${C_DIM}Buffett Rule: ${criteria}${C_RESET}"
    echo -e "${C_MAGENTA}└─────────────────────────────────────────────────────────────────────────────┘${C_RESET}"
}

print_usage() {
    cat << EOF
Usage: $(basename "$0") [OPTIONS]

Automates the 5-Step Warren Buffett Investment Playbook & 3-Gate Decision
Engine on the NASDAQ 100 universe using raw SEC EDGAR facts.

Options:
  -s, --skip-sync        Skip Step 1 (SEC EDGAR data ingest) and use existing DB
  --sync-only            Run Step 1 to download/refresh facts and filings, then exit
  --universe FILE        Universe ticker list file (default: universes/nasdaq100.txt)
  --step <1-5|all>       Run only a specific step (1, 2, 3, 4, 5, or all)
  -t, --ticker SYMBOL    Target specific ticker(s) for Step 5 deep-dive due diligence
                         (can be specified multiple times, e.g. -t NVDA -t ADBE)
  --years N              Number of annual periods for trend ranking (default: 6, or 5)
  --top N                Number of top candidates to audit in Step 5 (default: 3)
  -r, --report FILE      Generate a comprehensive markdown report at FILE
  --output-dir DIR       Directory to store step outputs, logs, and investment memo
  --min-score SCORE      Gate 1 composite quality score threshold (default: 75)
  --min-roic PCT         Gate 1 minimum 5-year average ROIC (default: 15)
  --min-adj-roic PCT     Step 4 minimum Intangible-Adjusted ROIC (default: 18)
  --min-yield PCT        Step 4 minimum Adjusted Owner Earnings Yield (default: 3.5)
  -i, --interactive      Prompt before launching interactive 10-K viewer in Step 5
  --no-color             Disable ANSI terminal colors
  -h, --help             Show this help documentation

The 5-Step Warren Buffett Playbook:
  Step 1: Ingest primary XBRL facts from SEC EDGAR (./sec sync)
  Step 2: Gate 1 Quality Filter: 5Y ROIC ≥ 15%, Consistency = 100% (./sec rank)
  Step 3: Gate 3 Inversion: Reverse DCF & Margin of Safety (./sec arbitrage)
  Step 4: Modern Economic Goodwill & Intangible Arbitrage (./sec gaap-arbitrage)
  Step 5: Gate 2 Dilution Integrity & 10-K Due Diligence (summary, audit, dcf, read)

Portfolio Allocation (The "Punch Card" Model):
  • Tier 1: Core Franchise Anchors (40%-60% of capital; 3-5 positions, 10-20% each)
  • Tier 2: Mispriced Value & GAAP Arbitrage (20%-30% of capital; 4-6 positions, 4-8% each)
  • Tier 3: Cash & Short-Term Treasuries (10%-25% dry powder call option for panics)

EOF
}

# ------------------------------------------------------------------------------
# Argument Parsing
# ------------------------------------------------------------------------------
while [[ $# -gt 0 ]]; do
    case "$1" in
        -s|--skip-sync)
            SKIP_SYNC=1
            shift
            ;;
        --sync-only)
            SYNC_ONLY=1
            shift
            ;;
        --universe)
            UNIVERSE="$2"
            shift 2
            ;;
        --step)
            STEP_FILTER="$2"
            shift 2
            ;;
        -t|--ticker)
            MANUAL_TICKERS+=("$2")
            shift 2
            ;;
        --top)
            DEFAULT_TOP="$2"
            shift 2
            ;;
        --years)
            YEARS="$2"
            shift 2
            ;;
        -r|--report)
            REPORT_FILE="$2"
            shift 2
            ;;
        --output-dir)
            OUTPUT_DIR="$2"
            shift 2
            ;;
        --min-score)
            MIN_SCORE="$2"
            shift 2
            ;;
        --min-roic)
            MIN_ROIC="$2"
            shift 2
            ;;
        --min-adj-roic)
            MIN_ADJ_ROIC="$2"
            shift 2
            ;;
        --min-yield)
            MIN_YIELD="$2"
            shift 2
            ;;
        -i|--interactive)
            INTERACTIVE=1
            shift
            ;;
        --no-color)
            NO_COLOR=1
            shift
            ;;
        -h|--help)
            setup_colors
            print_banner
            print_usage
            exit 0
            ;;
        *)
            echo "Unknown argument: $1" >&2
            echo "Run $(basename "$0") --help for usage." >&2
            exit 1
            ;;
    esac
done

setup_colors

# ------------------------------------------------------------------------------
# Preflight Verification
# ------------------------------------------------------------------------------
if [[ ! -x "${SEC_BIN}" ]]; then
    # Check if we can build it via go
    if command -v go >/dev/null 2>&1 && [[ -f "${SCRIPT_DIR}/cmd/sec/main.go" ]]; then
        echo -e "${C_YELLOW}Notice: sec binary not found. Compiling via 'go build -o sec ./cmd/sec'...${C_RESET}"
        (cd "${SCRIPT_DIR}" && go build -o sec ./cmd/sec)
    else
        echo -e "${C_RED}Error: sec binary not found or not executable at '${SEC_BIN}'.${C_RESET}" >&2
        exit 1
    fi
fi

if [[ ! -f "${UNIVERSE}" ]]; then
    echo -e "${C_RED}Error: Universe file not found at '${UNIVERSE}'.${C_RESET}" >&2
    exit 1
fi

UNIVERSE_COUNT=$(grep -cve '^\s*$' "${UNIVERSE}" || true)

if [[ -n "${OUTPUT_DIR}" ]]; then
    mkdir -p "${OUTPUT_DIR}"
fi

# Temporary files for capturing outputs
TMP_DIR="$(mktemp -d -t buffett_nasdaq.XXXXXX)"
cleanup() {
    rm -rf "${TMP_DIR}"
}
trap cleanup EXIT

RANK_OUT="${TMP_DIR}/rank.txt"
ARBITRAGE_OUT="${TMP_DIR}/arbitrage.txt"
GAAP_OUT="${TMP_DIR}/gaap.txt"

# ------------------------------------------------------------------------------
# STEP 1: Ingest Primary SEC EDGAR Data
# ------------------------------------------------------------------------------
step_1_ingest() {
    print_section "STEP 1: INGEST PRIMARY DATA (SEC EDGAR XBRL)" \
                  "Primary-Source Facts downloaded straight from SEC EDGAR accession archives"

    echo -e "${C_CYAN}Target Universe:${C_RESET} ${UNIVERSE} (${UNIVERSE_COUNT} companies)"
    echo -e "${C_CYAN}SEC User-Agent :${C_RESET} ${SEC_USER_AGENT}"
    echo -e "${C_CYAN}Database Path  :${C_RESET} ${DATA_DIR}/sec.db\n"

    if [[ "${SKIP_SYNC}" -eq 1 ]]; then
        echo -e "${C_YELLOW}» Skipping SEC sync as requested (--skip-sync). Using existing local database.${C_RESET}"
        return 0
    fi

    echo -e "${C_GREEN}» Ingesting raw financial statements and filing histories...${C_RESET}"
    "${SEC_BIN}" sync --universe "${UNIVERSE}" --workers 4 --data-dir "${DATA_DIR}" --ua "${SEC_USER_AGENT}"

    echo -e "${C_GREEN}✔ SEC EDGAR ingestion complete.${C_RESET}"
}

# ------------------------------------------------------------------------------
# STEP 2: Screen for High-Moat "Wonderful Businesses" (Quality Gate 1)
# ------------------------------------------------------------------------------
step_2_screen() {
    print_section "STEP 2: SCREEN FOR HIGH-MOAT 'WONDERFUL BUSINESSES'" \
                  "Filtering for durable economic moats, margin consistency, and share reduction"

    print_gate_box "1" "THE 'WONDERFUL BUSINESS' QUALITY TEST" \
                  "ROIC ≥ 15%, Operating Margin Consistency = 100%, Composite Score ≥ 70-75"

    echo -e "${C_DIM}Executing: ./sec rank --universe ${UNIVERSE} --years ${YEARS} --min-revenue ${MIN_REVENUE} --exclude-financials${C_RESET}\n"

    "${SEC_BIN}" rank \
        --universe "${UNIVERSE}" \
        --years "${YEARS}" \
        --min-revenue "${MIN_REVENUE}" \
        --exclude-financials \
        --data-dir "${DATA_DIR}" | tee "${RANK_OUT}"

    if [[ -n "${OUTPUT_DIR}" ]]; then
        cp "${RANK_OUT}" "${OUTPUT_DIR}/02_quality_rank.txt"
    fi

    echo -e "\n${C_BOLD}${C_CYAN}What Warren Buffett Inspects in Gate 1:${C_RESET}"
    echo -e "  • ${C_BOLD}Composite Score ≥ 70-75:${C_RESET} Top quartile among industry peers across capital returns, margins, and growth."
    echo -e "  • ${C_BOLD}5Y Multi-Year ROIC ≥ 15-20%:${C_RESET} Confirms an enduring moat, pricing power, and ability to compound capital."
    echo -e "  • ${C_BOLD}Margin Consistency = 100%:${C_RESET} Operating margins maintained or expanded through economic downturns and cycles."
    echo -e "  • ${C_BOLD}5Y Share Reduction > 0%:${C_RESET} Capital allocation test—management is retiring shares (cannibals), not diluting owners."
}

# ------------------------------------------------------------------------------
# STEP 3: Expectations Arbitrage (Reverse DCF & Margin of Safety - Gate 3)
# ------------------------------------------------------------------------------
step_3_expectations_arbitrage() {
    print_section "STEP 3: EXPECTATIONS ARBITRAGE (REVERSE DCF & MARGIN OF SAFETY)" \
                  "\"Invert, Always Invert\" — What does today's market price assume the business must do?"

    print_gate_box "3" "VALUATION VIA INVERSION (EXPECTATIONS ARBITRAGE)" \
                  "Demand positive Alpha Spread (g_hist - g_implied > 0) & share cannibalism"

    echo -e "${C_DIM}Executing: ./sec arbitrage --universe ${UNIVERSE} --fetch-prices --min-score ${MIN_SCORE} --min-roic ${MIN_ROIC} --require-buybacks${C_RESET}\n"

    "${SEC_BIN}" arbitrage \
        --universe "${UNIVERSE}" \
        --fetch-prices \
        --min-score "${MIN_SCORE}" \
        --min-roic "${MIN_ROIC}" \
        --require-buybacks \
        --data-dir "${DATA_DIR}" | tee "${ARBITRAGE_OUT}"

    if [[ -n "${OUTPUT_DIR}" ]]; then
        cp "${ARBITRAGE_OUT}" "${OUTPUT_DIR}/03_expectations_arbitrage.txt"
    fi

    echo -e "\n${C_BOLD}${C_CYAN}How Warren Buffett Interprets Expectations Arbitrage:${C_RESET}"
    echo -e "  • ${C_BOLD}Implied Growth Rate:${C_RESET} High-quality compounders priced for depressed growth (2% - 4%) offer asymmetric upside."
    echo -e "  • ${C_BOLD}Positive Alpha Spread (g_hist - g_implied ≥ 4-6%):${C_RESET} Classic Benjamin Graham Margin of Safety."
    echo -e "    You pay for stagnation or slow growth, while receiving durable economic compounding for free."
    echo -e "  • ${C_BOLD}Require Buybacks (--require-buybacks):${C_RESET} Filters out promotion-heavy firms issuing huge stock options."
}

# ------------------------------------------------------------------------------
# STEP 4: Modern Economic Goodwill (Modern GAAP Arbitrage)
# ------------------------------------------------------------------------------
step_4_gaap_arbitrage() {
    print_section "STEP 4: EVALUATE ECONOMIC GOODWILL (MODERN GAAP ARBITRAGE)" \
                  "Capitalizing R&D and brand SG&A (Peters & Taylor / Damodaran model) to reveal hidden value"

    echo -e "${C_DIM}Buffett's See's Candies Principle: Accounting rules immediately expense brand marketing and software R&D,"
    echo -e "making franchise compounders look artificially expensive on traditional backward-looking P/E ratios.${C_RESET}\n"

    echo -e "${C_DIM}Executing: ./sec gaap-arbitrage --universe ${UNIVERSE} --fetch-prices --min-adj-roic ${MIN_ADJ_ROIC} --min-yield ${MIN_YIELD} --surging${C_RESET}\n"

    "${SEC_BIN}" gaap-arbitrage \
        --universe "${UNIVERSE}" \
        --fetch-prices \
        --min-adj-roic "${MIN_ADJ_ROIC}" \
        --min-yield "${MIN_YIELD}" \
        --surging \
        --data-dir "${DATA_DIR}" | tee "${GAAP_OUT}"

    if [[ -n "${OUTPUT_DIR}" ]]; then
        cp "${GAAP_OUT}" "${OUTPUT_DIR}/04_gaap_arbitrage.txt"
    fi

    echo -e "\n${C_BOLD}${C_CYAN}Warren Buffett's Modern Economic Goodwill Takeaway:${C_RESET}"
    echo -e "  • ${C_BOLD}Intangible-Adjusted ROIC ≥ 25-30%:${C_RESET} Modern franchises (Apple, Microsoft, Adobe) compound via software R&D and user lock-in."
    echo -e "  • ${C_BOLD}Intangible Owner Yield Hurdle:${C_RESET} Owner Yield ≥ 10Y Treasury (4.84%) + 2.0% equity hurdle ≈ 6.8%-7.0%."
    echo -e "    When Adjusted Owner Yield clears this hurdle, you have found a wonderful business at a fair price."
}

# ------------------------------------------------------------------------------
# STEP 5: Read the 10-K Cover to Cover & Due Diligence (Gate 2 Check)
# ------------------------------------------------------------------------------
step_5_due_diligence() {
    print_section "STEP 5: DUE DILIGENCE & READ THE 10-K COVER TO COVER" \
                  "\"I read 500 pages like this every day.\" — Verifying disclosures, SBC dilution, and maintenance CapEx"

    print_gate_box "2" "CAPITAL ALLOCATION & DILUTION INTEGRITY" \
                  "Share count reduction (Cannibals) & SBC Dilution Check (SBC < 20-25% of FCF)"

    local -a candidates=()

    if [[ ${#MANUAL_TICKERS[@]} -gt 0 ]]; then
        candidates=("${MANUAL_TICKERS[@]}")
    else
        # Collect unique candidates from Step 3 and Step 4 outputs
        local raw_tickers=()

        if [[ -f "${ARBITRAGE_OUT}" ]]; then
            while IFS= read -r ticker; do
                [[ -n "${ticker}" ]] && raw_tickers+=("${ticker}")
            done < <(awk '$1 ~ /^[0-9]+$/ {print $2}' "${ARBITRAGE_OUT}")
        fi

        if [[ -f "${GAAP_OUT}" ]]; then
            while IFS= read -r ticker; do
                [[ -n "${ticker}" ]] && raw_tickers+=("${ticker}")
            done < <(awk '$1 ~ /^[0-9]+$/ {print $2}' "${GAAP_OUT}")
        fi

        # Combine uniquely up to DEFAULT_TOP
        if [[ ${#raw_tickers[@]} -gt 0 ]]; then
            for t in "${raw_tickers[@]}"; do
                [[ -z "$t" ]] && continue
                local already=0
                if [[ ${#candidates[@]} -gt 0 ]]; then
                    for existing in "${candidates[@]}"; do
                        if [[ "${existing}" == "$t" ]]; then
                            already=1
                            break
                        fi
                    done
                fi
                if [[ "$already" -eq 0 ]]; then
                    candidates+=("$t")
                    if [[ ${#candidates[@]} -ge "${DEFAULT_TOP}" ]]; then
                        break
                    fi
                fi
            done
        fi
    fi

    if [[ ${#candidates[@]} -eq 0 ]]; then
        echo -e "${C_YELLOW}Notice: No candidate tickers found in screen results. Defaulting to franchise compounder benchmark AAPL.${C_RESET}"
        candidates=("AAPL")
    fi

    echo -e "${C_BOLD}Selected Candidate Dossiers for Step 5 Due Diligence:${C_RESET} ${candidates[*]}\n"

    for ticker in "${candidates[@]}"; do
        echo -e "${C_BOLD}${C_YELLOW}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${C_RESET}"
        echo -e "${C_BOLD}${C_WHITE} PRIMARY DUE DILIGENCE DOSSIER: ${ticker}${C_RESET}"
        echo -e "${C_BOLD}${C_YELLOW}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${C_RESET}\n"

        # 1. 5-Year Financial Summary & Gate 2 Check
        echo -e "${C_CYAN}▶ 1. Multi-Year Historical Trajectory (./sec summary ${ticker} --years 5)${C_RESET}"
        local summary_file="${TMP_DIR}/summary_${ticker}.txt"
        "${SEC_BIN}" summary "${ticker}" --years 5 --data-dir "${DATA_DIR}" | tee "${summary_file}"

        # Gate 2 Analysis: SBC Dilution Check
        echo -e "\n${C_BOLD}${C_MAGENTA}--- Gate 2 Dilution & Cannibal Integrity Check for ${ticker} ---${C_RESET}"
        local fcf_line sbc_line shares_line
        fcf_line=$(grep "^Free Cash Flow" "${summary_file}" | head -n 1 || true)
        sbc_line=$(grep "^Stock-Based Compensation" "${summary_file}" | head -n 1 || true)
        shares_line=$(grep "^Shares Outstanding" "${summary_file}" | head -n 1 || true)

        if [[ -n "${fcf_line}" && -n "${sbc_line}" ]]; then
            echo -e "  ${C_BOLD}Free Cash Flow:${C_RESET}           ${fcf_line#Free Cash Flow}"
            echo -e "  ${C_BOLD}Stock-Based Compensation:${C_RESET} ${sbc_line#Stock-Based Compensation}"
            echo -e "  ${C_DIM}Rule: SBC must be < 20-25% of Free Cash Flow to prevent hidden options dilution.${C_RESET}"
        fi
        if [[ -n "${shares_line}" ]]; then
            echo -e "  ${C_BOLD}5Y Share Count:${C_RESET}           ${shares_line#Shares Outstanding}"
            echo -e "  ${C_DIM}Rule: Five-year share count must be declining (e.g. retiring 2-4% shares/year).${C_RESET}"
        fi

        # 2. Accounting Diagnostic Audit
        echo -e "\n${C_CYAN}▶ 2. Primary-Source Disclosure Audit (./sec audit ${ticker})${C_RESET}"
        local audit_file="${TMP_DIR}/audit_${ticker}.txt"
        "${SEC_BIN}" audit "${ticker}" --data-dir "${DATA_DIR}" > "${audit_file}"
        head -n 35 "${audit_file}"
        echo -e "${C_DIM}  (Full diagnostic checks XBRL concept integrity, revenue recognition, and lease liabilities)${C_RESET}"

        # 3. Normalized DCF Valuation
        echo -e "\n${C_CYAN}▶ 3. Conservative Normalized DCF Valuation (./sec dcf ${ticker} --mode fcf --capex-mode normalized --discount ${DCF_DISCOUNT} --terminal ${DCF_TERMINAL})${C_RESET}"
        local dcf_file="${TMP_DIR}/dcf_${ticker}.txt"
        "${SEC_BIN}" dcf "${ticker}" \
            --mode fcf \
            --capex-mode normalized \
            --discount "${DCF_DISCOUNT}" \
            --terminal "${DCF_TERMINAL}" \
            --data-dir "${DATA_DIR}" | tee "${dcf_file}"

        if [[ -n "${OUTPUT_DIR}" ]]; then
            cp "${summary_file}" "${OUTPUT_DIR}/summary_${ticker}.txt"
            cp "${audit_file}" "${OUTPUT_DIR}/audit_${ticker}.txt"
            cp "${dcf_file}" "${OUTPUT_DIR}/dcf_${ticker}.txt"
        fi

        # 4. SEC EDGAR Primary Filings Browser
        echo -e "\n${C_CYAN}▶ 4. Armchair Reading: Primary 10-K & 10-Q Browser${C_RESET}"
        echo -e "  To read the MD&A, notes, and debt schedule directly from SEC EDGAR, run:"
        echo -e "  ${C_BOLD}${C_GREEN}./sec read ${ticker}${C_RESET}\n"

        if [[ "${INTERACTIVE}" -eq 1 && -t 0 ]]; then
            read -r -p "Would you like to launch the interactive filing reader for ${ticker} now? [y/N]: " launch_read
            if [[ "${launch_read}" =~ ^[Yy]$ ]]; then
                "${SEC_BIN}" read "${ticker}" --data-dir "${DATA_DIR}"
            fi
        fi
    done
}

# ------------------------------------------------------------------------------
# STEP 6: Synthesis & Portfolio Allocation ("The Punch Card Model")
# ------------------------------------------------------------------------------
step_6_synthesis() {
    print_section "INVESTMENT DECISION ENGINE: SYNTHESIS & PORTFOLIO ALLOCATION" \
                  "The 'Punch Card' Model & The 3 Justifiable Sell Triggers"

    cat << 'EOF'
┌─────────────────────────────────────────────────────────────┐
│  Tier 1: Core Franchise Anchors (40% - 60% of capital)      │
│  • 3 to 5 positions (10% - 20% each)                        │
│  • Elite ROIC (>20%), pristine balance sheet, cannibals     │
├─────────────────────────────────────────────────────────────┤
│  Tier 2: Mispriced Value & Modern GAAP Arbitrage (20% - 30%)│
│  • 4 to 6 positions (4% - 8% each)                          │
│  • Large Alpha Spread, temporary pessimism, brand/R&D power │
├─────────────────────────────────────────────────────────────┤
│  Tier 3: Cash & Short-Term Treasuries (10% - 25%)           │
│  • Dry powder call option for panics                        │
└─────────────────────────────────────────────────────────────┘
EOF

    echo -e "\n${C_BOLD}${C_WHITE}Holding Periods & The Sell Discipline:${C_RESET}"
    echo -e "  \"When we own portions of outstanding businesses with outstanding managements,"
    echo -e "   our favorite holding period is forever.\" — Warren Buffett (1988)"
    echo -e "\n${C_BOLD}${C_RED}The Only 3 Justifiable Sell Triggers:${C_RESET}"
    echo -e "  ${C_BOLD}1. Permanent Moat Decay:${C_RESET} 3-year trailing ROIC drops below cost of capital, operating"
    echo -e "     margins compress multi-year, or SBC balloons without revenue growth. (Thesis broken: Sell completely)."
    echo -e "  ${C_BOLD}2. Grotesque Market Overvaluation:${C_RESET} Reverse DCF indicates Mr. Market is pricing in sustained"
    echo -e "     ≥ 25-30% annual growth for a mega-cap, requiring mathematically impossible market share. (Trim)."
    echo -e "  ${C_BOLD}3. Superior Opportunity Cost (Munger Rule):${C_RESET} You discover another wonderful business meeting"
    echo -e "     all 3 Gates with double the expected Owner Yield and margin of safety. (Reallocate)."
}

# ------------------------------------------------------------------------------
# Report Generation (Markdown Export)
# ------------------------------------------------------------------------------
generate_markdown_report() {
    local target_report="$1"
    echo -e "\n${C_GREEN}» Writing comprehensive investment report to: ${target_report}${C_RESET}"

    cat << EOF > "${target_report}"
# How Warren Buffett Would Use This Tool: NASDAQ 100 Analysis

> *"It's far better to buy a wonderful company at a fair price than a fair company at a wonderful price."*  
> — **Warren Buffett**, Berkshire Hathaway Shareholder Letter (1989)

**Execution Date:** $(date -u +"%Y-%m-%d %H:%M:%SZ")  
**Universe:** NASDAQ 100 (\`${UNIVERSE}\`, ${UNIVERSE_COUNT} companies)  
**Primary Engine:** SEC EDGAR XBRL Primary Facts Database

---

## 1. Executive Summary & The 3-Gate Decision Filter

Buffett and Munger search for **durable compounders**: businesses with wide economic moats, high returns on capital without excessive debt, shareholder-aligned capital allocation, and honest cash generation.

\`Raw EDGAR Universe\` ➔ **Gate 1: Quality Filter** (\`sec rank\`) ➔ **Gate 2: Capital Allocation** (\`sec summary\`) ➔ **Gate 3: Margin of Safety** (\`sec arbitrage\`) ➔ **Actionable Buy Candidate**

---

## 2. Gate 1: High-Moat Quality Rankings (\`sec rank\`)

Screened nonfinancial companies with at least \$100M revenue over 5-year trends:
\`\`\`text
$(if [[ -f "${RANK_OUT}" ]]; then cat "${RANK_OUT}"; else echo "N/A"; fi)
\`\`\`

---

## 3. Gate 3: Expectations Arbitrage & Margin of Safety (\`sec arbitrage\`)

Reversing the DCF to find what growth the market implies at current prices:
\`\`\`text
$(if [[ -f "${ARBITRAGE_OUT}" ]]; then cat "${ARBITRAGE_OUT}"; else echo "N/A"; fi)
\`\`\`

---

## 4. Modern Economic Goodwill: Intangible-Adjusted Arbitrage (\`sec gaap-arbitrage\`)

Peters & Taylor / Damodaran capitalized R&D and brand SG&A revealing hidden economic franchises:
\`\`\`text
$(if [[ -f "${GAAP_OUT}" ]]; then cat "${GAAP_OUT}"; else echo "N/A"; fi)
\`\`\`

---

## 5. Portfolio Allocation: The "Punch Card" Model

| Tier | Role | Target Allocation | Position Count & Size | Criteria |
| :--- | :--- | :--- | :--- | :--- |
| **Tier 1** | Core Franchise Anchors | 40% – 60% | 3 to 5 positions (10%–20% each) | Score ≥ 80, ROIC > 20%, fortress balance sheet, share cannibals |
| **Tier 2** | Mispriced Value & GAAP Arbitrage | 20% – 30% | 4 to 6 positions (4%–8% each) | Large Alpha Spread, depressed market expectations, surging intangible ROIC |
| **Tier 3** | Cash & Short-Term Treasuries | 10% – 25% | Liquid dry powder | Call option on market crashes and panic valuations |

---

## 6. The Only 3 Justifiable Sell Triggers

1. **Permanent Moat Decay:** 3-year trailing ROIC falls below cost of capital, margins collapse, or SBC balloons without revenue growth.
2. **Grotesque Market Overvaluation:** Reverse DCF implies sustained ≥ 25–30% growth for a mega-cap, requiring mathematically impossible market share.
3. **Superior Opportunity Cost:** A wonderful business meeting all 3 Gates offers double the expected Owner Yield and margin of safety.
EOF
}

# ------------------------------------------------------------------------------
# Main Dispatcher
# ------------------------------------------------------------------------------
main() {
    print_banner

    if [[ "${SYNC_ONLY}" -eq 1 ]]; then
        step_1_ingest
        echo -e "${C_GREEN}Sync-only complete. Exiting.${C_RESET}"
        exit 0
    fi

    case "${STEP_FILTER}" in
        1)
            step_1_ingest
            ;;
        2)
            step_2_screen
            ;;
        3)
            step_3_expectations_arbitrage
            ;;
        4)
            step_4_gaap_arbitrage
            ;;
        5)
            step_5_due_diligence
            ;;
        all)
            step_1_ingest
            step_2_screen
            step_3_expectations_arbitrage
            step_4_gaap_arbitrage
            step_5_due_diligence
            step_6_synthesis
            ;;
        *)
            echo -e "${C_RED}Error: Invalid --step '${STEP_FILTER}'. Choose 1, 2, 3, 4, 5, or all.${C_RESET}" >&2
            exit 1
            ;;
    esac

    # Handle reporting
    if [[ -n "${REPORT_FILE}" ]]; then
        generate_markdown_report "${REPORT_FILE}"
    elif [[ -n "${OUTPUT_DIR}" ]]; then
        generate_markdown_report "${OUTPUT_DIR}/buffett_nasdaq100_report.md"
    fi

    echo -e "\n${C_BOLD}${C_GREEN}Playbook execution finished successfully.${C_RESET}\n"
}

main "$@"
