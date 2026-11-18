# SEC Filing Reader & Fundamentals Screener

A command-line tool and JSON API server to inspect SEC EDGAR company fundamentals, filing diffs, quarterly/TTM inflections, DCF / reverse-DCF / WACC valuations, and expressive multi-factor or formula-based universe screens — powered by a self-healing local SQLite cache.

## Quick Start

```sh
make build
export SEC_USER_AGENT="Research App contact@example.com"

# Just-in-time hydration: fetches 20-F/10-K facts, live FX rates, ADR ratios & quote automatically
./sec inspect ASML
./sec inspect AAPL quarterly
./sec inspect AAPL diff --item 1A

# Valuation
./sec value AAPL
./sec value AAPL reverse-dcf
./sec value ASML wacc --beta 1.15

# Expressive formula screening & built-in strategies
./sec screen @sp500 --where "roic > 0.15 && fcf_yield > 0.05"
./sec screen owner-yield @euro-uk --min-yield 4
```

SEC EDGAR recommends an identifying `User-Agent` with contact information via `SEC_USER_AGENT` or `--ua`.

## Command Grammar

Commands are organized around analyst intent rather than data plumbing:

```text
Company Research
  inspect TICKER [view]       Inspect financials, quarterly/TTM, filing diffs, audits, or filings
  value TICKER [model]        Run DCF, reverse-DCF, or WACC valuation

Idea Discovery
  screen [strategy] [@univ]   Screen with rank, quality-discount, owner-yield, arbitrage,
                              gaap-arbitrage, or custom --where "<formula>" queries

Universes & Data
  universe <list|show|create> Browse, inspect, or create @-prefixed ticker universes
  data <sync|status|prune>    Manage local SQLite cache, exports, and storage

Server & Shell
  serve                       Start the HTTP JSON API server
  completion <bash|zsh|fish>  Generate shell tab-completion scripts
```

## Self-Healing Cache & Freshness

Local SQLite (`data/sec.db`) and derived precomputation act as an invisible, self-healing cache:

- **Just-in-Time Hydration**: Running `sec inspect ASML` or `sec value ASML` when `ASML` is uncached automatically fetches its EDGAR submissions & facts, live FX rate, ADR ratio, and live quote while showing a concise inline spinner, then immediately renders the result.
- **Freshness Indicators**: Every command output includes a metadata header (`Data: Q3 2026 10-Q (synced 4m ago) · Price $228.10 (synced 4m ago) · EUR→USD 1.0950 (live)`). Pass `--refresh` to force fresh pulls, or `--offline` to restrict strictly to local cache.
- **Smart Batch Progress**: Running `sec screen` across large universes (`@russell3000`, `@broad`, `@sp500`) shows a single inline status bar tracking cache hits vs. EDGAR fetches with SEC rate-limit ETA (`8.5 req/s`) and skips unavailable/delisted tickers gracefully without failing mid-run.
- **Ergonomic `@`-Prefixed Universes**: Use short `@`-aliases everywhere (`@sp500`, `@nasdaq100`, `@midcap400`, `@russell2000`, `@russell3000`, `@broad`, `@adr`, `@euro-uk`, `@defense`, `@space-defense`, `@all`) with shell tab-completion.

---

### 1. Inspecting Companies (`sec inspect`)

```sh
./sec inspect AAPL                          # 5-year annual financial summary
./sec inspect ASML --usd                    # Convert reported EUR figures to USD via live FX
./sec inspect AAPL quarterly --quarters 8   # Discrete 10-Q quarterly breakdown (reconstructs Q4)
./sec inspect AAPL ttm                      # Trailing-Twelve-Month + quarterly earnings acceleration
./sec inspect AAPL diff --item 1A           # Redline diff of Risk Factors (or --item 7 for MD&A)
./sec inspect AAPL audit                    # Diagnostic XBRL tag & coverage audit
./sec inspect audit --gaps-only             # Database-wide missing metric audit
./sec inspect AAPL filings --form 10-K      # Browse or open saved SEC filings
```

- **Quarterly 10-Q Extraction & Mathematical Reconstruction**: Isolates discrete 3-month quarterly facts and reconstructs Q4 ($\text{Q4} = \text{Annual 12M} - \text{Q3 YTD 9M}$), Q3, and Q2 automatically when issuers report cumulative YTD flows.
- **Quarterly Inflections & Earnings Acceleration**: Tracks YoY growth ($Q_0 \text{ vs } Q_4$), Prior YoY growth ($Q_1 \text{ vs } Q_5$), and QoQ momentum, flagging **ACCELERATING ▲** or **decelerating ▼**.
- **Filing Diffs (`diff`)**: Fetches and caches 10-K/20-F HTML documents locally (`data/filings/`) and computes paragraph- and word-level redline diffs for Item 1A (Risk Factors) or Item 7 (MD&A).

---

### 2. Valuation (`sec value`)

```sh
./sec value AAPL                            # Forward FCF DCF with auto live quote & WACC
./sec value GOOGL dcf --mode revenue --capex-mode normalized
./sec value AAPL reverse-dcf                # Market-implied 5Y growth from live quote (or --market-cap)
./sec value MSFT reverse-dcf --mode fcf --market-cap 3.0e12
./sec value ASML wacc --beta 1.15           # Full WACC, Ke, after-tax Kd & capital weight breakdown
```

- **Auto Macro & ADR Calibration**: Automatically detects regional macro benchmarks (`us`, `eu`, `uk`, `ch`) from reporting currency (`--profile` to override), converts ordinary Form 20-F shares using ADR ratios (`--adr-ratio` to override), and pulls live share prices unless `--market-cap`, `--price`, or `--no-live-price` is specified.
- **Reverse DCF**: Inverts owner-earnings (`FCF - SBC`), FCF, or revenue-driven FCFF models to find the market-implied CAGR and compares it against historical growth (`Alpha Spread`).

---

### 3. Screening & Formula Queries (`sec screen`)

#### Expressive Formula Query Language (`--where`)

Screen any `@universe` or the entire local database directly from the terminal using arithmetic and boolean expressions:

```sh
./sec screen @sp500 --where "roic > 0.15 && fcf_yield > 0.05"
./sec screen @nasdaq100 --where "adj_roic > 20% && market_cap > 10B && share_reduction > 0" --sort fcf_yield
./sec screen @euro-uk --where "operating_margin > 0.20 && net_cash > 0" --format json
./sec screen @broad --where "(fcf - sbc) / market_cap > 0.04 && pe < 25" --top 25
```

- **Three-Valued Lazy Quote Pruning**: Fundamental predicates (e.g., `roic > 0.15`) run against precomputed SQLite summaries first; live market quotes are fetched only for companies not already eliminated.
- **Cross-Currency Normalization**: Monetary amounts (`revenue`, `fcf`, `market_cap`, `ev`, `net_debt`) are normalized to USD using live/cached FX rates and ADR ratios.
- **Supported Identifiers**:
  - *Profitability & Returns*: `roic`, `adj_roic`, `avg_adj_roic`, `gaap_roic`, `roic_delta`, `surging`, `roe`, `gross_margin`, `operating_margin`, `adj_ebit_margin`, `operating_consistency`, `net_margin`, `fcf_margin`, `owner_margin`
  - *Growth & Capital Allocation*: `revenue_cagr`, `eps_growth`, `book_growth`, `share_reduction`, `score`
  - *Valuation & Multiples*: `price`, `market_cap` (`mcap`), `ev`, `pe`, `pb`, `fcf_yield`, `owner_yield`, `standard_owner_yield`, `net_cash_yield`, `ev_fcf`, `ev_ebit`, `ev_sales`
  - *Financials (USD)*: `revenue`, `cost_of_revenue`, `gross_profit`, `operating_income` (`ebit`), `adj_ebit`, `sga`, `rd`, `net_income`, `ocf`, `capex`, `dna`, `fcf`, `sbc`, `sbc_pct_fcf`, `owner_earnings`, `adj_owner_earnings`, `cash`, `debt`, `net_debt`, `net_cash`, `net_cash_assets`, `equity`, `total_assets`, `current_assets`, `current_liabilities`, `nwc`, `intangible_capital`, `intangible_amort`, `adj_invested_capital`, `shares`, `diluted_shares`
  - *Metadata*: `ticker`, `company`, `sector`, `sic`, `peer_group`, `currency`

#### Built-in Screening Strategies

All strategies accept `@universe` tokens, optional `--where` composition, `--top N`, `--prices FILE`, `--refresh`, and `--offline`:

- **`rank`**: Multi-factor peer-group quality ranking within SEC SIC major groups.
  ```sh
  ./sec screen rank @sp500 --exclude-financials --top 25
  ```
- **`quality-discount`** *(QaaD)*: High-moat compounders trading at a DCF margin of safety.
  ```sh
  ./sec screen quality-discount @russell2000 --min-score 70 --min-discount 15
  ```
- **`owner-yield`**: Ranks companies by Intangible-Adjusted Owner Earnings Yield `(FCF - SBC + Net Intangible Investment) / Market Cap` against the `6.84%` hurdle rate (`10Y Treasury + 2%`).
  ```sh
  ./sec screen owner-yield @sp500 --hurdle --min-roic 15
  ```
- **`arbitrage`**: Expectations arbitrage comparing historical/peer growth vs. reverse-DCF market-implied growth.
  ```sh
  ./sec screen arbitrage @sp500 --min-score 75 --min-roic 15 --require-buybacks
  ```
- **`gaap-arbitrage`**: Peters & Taylor / Damodaran intangible-capitalized arbitrage (capitalizing R&D over 5 years and 30% of SG&A over 3 years).
  ```sh
  ./sec screen gaap-arbitrage @nasdaq100 --min-adj-roic 15 --min-yield 3.5 --surging
  ```

---

### 4. Universes & Data Management (`sec universe`, `sec data`)

```sh
# Universes
./sec universe list                                         # List @aliases & local cache coverage
./sec universe show @euro-uk                                # Inspect tickers & sync status
./sec universe create @compounders AAPL MSFT ASML COST      # Create custom @compounders universe

# Data management
./sec data status --list                                    # SQLite stats, cache counts, synced list
./sec data sync @defense --workers 4                        # Pre-warm a universe with batch ETA
./sec data prune --older-than 30d                           # Clean expired quotes, FX, HTML & VACUUM
./sec data export AAPL --format json > aapl_facts.json      # Export raw XBRL facts
./sec data rebuild                                          # Recompute derived summaries & scores
```

---

### 5. HTTP API Server & Shell Tab-Completion

```sh
# Start the JSON API server
./sec serve --port 8080

# Enable shell tab-completion (supports commands, tickers, and @universe aliases)
source <(sec completion zsh)     # Zsh
source <(sec completion bash)    # Bash
sec completion fish | source     # Fish
```

## Configuration

| Variable / Flag | Description | Default |
| :--- | :--- | :--- |
| `SEC_USER_AGENT` / `--ua` | Identifying User-Agent header with contact email | `SECAnalysisApp research@secanalysis.local` |
| `SEC_DATABASE_DIR` / `--data-dir` | Directory storing `sec.db` and `filings/` HTML cache | `data/` |
| `SEC_UNIVERSES_DIR` | Directory storing `@universe` `.txt` files | `universes/` |
| `SEC_CACHE_DIR` | Directory caching SEC's ticker-to-CIK map for 24 hours | OS user cache directory |
