# SEC Filing Reader & Fundamentals Screener

A command-line tool to fetch SEC EDGAR company facts and filing history into a local SQLite database, inspect multi-year fundamentals, run two-stage DCF valuations, and rank an investable universe.

## Quick Start

```sh
go build -o sec ./cmd/sec
export SEC_USER_AGENT="Research App contact@example.com"
```

SEC EDGAR requires an identifying `User-Agent` with contact information via `SEC_USER_AGENT` or `--ua`.

## Commands

### Sync Universe

Download `us-gaap`, `ifrs-full`, and `dei` facts plus complete filing histories (including amendments) into `data/sec.db`:

```sh
./sec sync AAPL MSFT GOOGL AMZN NVDA META
./sec sync --universe sp500.txt --workers 4
./sec sync --universe all-tickers.txt --workers 4
```

`sync` runs up to 4 concurrent workers (`--workers 1` for sequential execution) sharing an SEC-compliant rate limiter.

### Financial Summary

Display 3 to 5 years of annual metrics:

```sh
./sec summary AAPL
./sec summary AAPL --years 3
```

Reports revenue, operating income, net income, operating cash flow (OCF), CapEx, D&A, free cash flow ($\text{OCF} - \text{CapEx}$), stock-based compensation (SBC), an SBC-adjusted FCF proxy, cash, borrowings, and share counts (period-end outstanding when available, otherwise diluted weighted-average).

### Discounted Cash Flow (`dcf`)

Run a two-stage DCF model with projected cash flows, an EV-to-equity bridge, per-share value, and a discount/terminal-growth sensitivity matrix:

```sh
./sec dcf AAPL
./sec dcf AAPL --growth 8 --discount 9 --terminal 2.5 --years 5
./sec dcf GOOGL --mode revenue --growth 9 --beta 1.1 --market-cap 2.5e12
./sec dcf META --mode revenue --capex-mode normalized
./sec dcf PLTR --debt 0
```

- **Forecast Modes**:
  - `fcf` *(default)*: Grows $\text{OCF} - \text{CapEx}$ from the 3-year historical FCF CAGR (when 4 consecutive positive annual periods exist). Raw CapEx fades the growth wedge ($\text{CapEx} - \text{D\&A}$) linearly to zero by the terminal year; `--capex-mode normalized` (or `--normalize-capex`) sets CapEx equal to D&A throughout.
  - `revenue`: Builds unlevered FCFF from forecast revenue (defaulting to the 3-year revenue CAGR), trailing operating margin, effective tax rate, D&A, CapEx, and change in net working capital (current assets less current liabilities).
- **WACC & Capital Structure**:
  - When `--discount` is omitted, WACC is estimated from `--risk-free` (default `4.84%`, [Nov 5, 2026 US 10Y Treasury](https://home.treasury.gov/resource-center/data-chart-center/interest-rates/TextView?field_tdr_date_value_month=202611&type=daily_treasury_yield_curve)), `--erp` (`5.5%`), `--beta` (`1.0`), `--cost-debt` (historical interest over average debt, fallback `5.0%`), and `--tax` (historical effective rate, fallback `21%`).
  - Because EDGAR does not report market prices, pass `--beta` and `--market-cap` (in reported currency units); otherwise book equity and net debt ($\max(0, \text{debt} - \text{cash})$) weight WACC while the equity bridge adds cash and subtracts gross debt.
  - Pass `--debt 0` when a filing confirms zero debt without an explicit zero-valued XBRL fact.

### Rank Companies

Screen and rank synced companies by within-industry percentiles:

```sh
./sec rank
./sec rank --universe sp500.txt --years 6 --min-revenue 100000000
./sec rank --exclude-financials
```

- **Eligibility**: Requires a recent filing, 6 annual periods for 5-year trends (`--years 5` for 4-year trends), latest USD revenue $\ge$ `--min-revenue` (default `$100M`), and positive net income over the last 3 years. Nonfinancials also require positive latest OCF and pass a solvency check when cash and debt facts are present.
- **Peer Grouping**: Uses 2-digit SEC SIC major groups (broadening to SIC divisions when a group has fewer than 5 companies). Missing metrics are shown as `—` and remaining weights are renormalized.

| Model | Scope | Factor Weights |
| :--- | :--- | :--- |
| **Nonfinancial** | Standard | **20%** ROIC · **20%** Owner-earnings margin ($\text{FCF} - \text{SBC}$) · **15%** Revenue CAGR · **15%** Operating margin & consistency · **10%** Net cash/assets · **10%** EPS CAGR · **10%** Share reduction |
| **Financial** | SIC 6000–6799 | **30%** ROE · **20%** Revenue CAGR · **20%** Book value/share CAGR · **20%** EPS CAGR · **10%** Share reduction |

> [!NOTE]
> Share trends use diluted weighted-average shares and automatically adjust for common split ratios outside a filing's comparative periods (marked with `*`). Rankings are peer-relative screens based on reported XBRL facts, not market-price valuations.

### Inspect, List & Export

```sh
./sec read AAPL                            # Browse filings and open SEC URLs (n/p move, /text search, q quit)
./sec list                                 # List synced tickers, company names, and CIKs
./sec export AAPL --format csv > aapl.csv  # Export extracted facts as CSV or JSON
```

## Configuration

| Variable / Flag | Description | Default |
| :--- | :--- | :--- |
| `SEC_USER_AGENT` / `--ua` | Identifying User-Agent header with contact email | *Required for `sync`* |
| `SEC_DATABASE_DIR` / `--data-dir` | Directory storing the local `sec.db` SQLite database | `data/` |
| `SEC_CACHE_DIR` | Directory caching SEC's ticker-to-CIK map for 24 hours | OS user cache directory |
