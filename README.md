# SEC Filing Reader & Fundamentals Screener

A command-line tool to fetch SEC EDGAR company facts and filing history into a local SQLite database, inspect multi-year fundamentals, run forward and reverse DCF valuations, and rank an investable universe.

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

Reports revenue, operating income, SG&A, R&D, intangible amortization, intangible-adjusted operating income, net income, operating cash flow (OCF), CapEx, D&A, free cash flow (`OCF - CapEx`), stock-based compensation (SBC), an SBC-adjusted FCF proxy, cash, borrowings, stockholders' equity, intangible capital, adjusted invested capital, intangible-adjusted ROIC, and share counts (period-end outstanding when available, otherwise diluted weighted-average).

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
  - `fcf` *(default)*: Grows `OCF - CapEx` from the 3-year historical FCF CAGR (when 4 consecutive positive annual periods exist). Raw CapEx fades the growth wedge (`CapEx - D&A`) linearly to zero by the terminal year; `--capex-mode normalized` (or `--normalize-capex`) sets CapEx equal to D&A throughout.
  - `revenue`: Builds unlevered FCFF from forecast revenue (defaulting to the 3-year revenue CAGR), trailing operating margin, effective tax rate, D&A, CapEx, and change in net working capital (current assets less current liabilities).
- **WACC & Capital Structure**:
  - When `--discount` is omitted, WACC is estimated from `--risk-free` (default `4.84%`, [Nov 5, 2026 US 10Y Treasury](https://home.treasury.gov/resource-center/data-chart-center/interest-rates/TextView?field_tdr_date_value_month=202611&type=daily_treasury_yield_curve)), `--erp` (`5.5%`), `--beta` (`1.0`), `--cost-debt` (historical interest over average debt, fallback `5.0%`), and `--tax` (historical effective rate, fallback `21%`).
  - Because EDGAR does not report market prices, pass `--beta` and `--market-cap` (in reported currency units); otherwise book equity and net debt (`max(0, debt - cash)`) weight WACC while the equity bridge adds cash and subtracts gross debt.
  - Pass `--debt 0` when a filing confirms zero debt without an explicit zero-valued XBRL fact.

### Reverse DCF & Expectations Arbitrage

Invert the DCF model to solve for the five-year growth rate implied by a market capitalization. The default reverse mode uses the SEC summary's SBC-adjusted FCF proxy, with FCF and revenue-driven FCFF also available:

```sh
./sec dcf AAPL --reverse --market-cap 3400000000000
./sec dcf MSFT --reverse --mode fcf --market-cap 3000000000000
./sec dcf GOOGL --reverse --mode revenue --market-cap 2200000000000
```

The inverse uses the same WACC inputs, effective tax-rate estimate, debt-cost estimate, CapEx normalization, and terminal-growth convention as the forward DCF. Market capitalization must use the currency units reported in the SEC facts.

Screen the rank universe for high-quality nonfinancial companies, then rank candidates by historical growth less market-implied growth:

```sh
./sec arbitrage --prices quotes.csv --min-score 75 --min-roic 15
./sec arbitrage --fetch-prices --min-score 75 --min-roic 15
./sec arbitrage --prices valuations.json --universe sp500.txt
```

`--prices` accepts CSV/TSV with `TICKER,PRICE` or `TICKER,MARKET_CAP` headers, or JSON ticker/value records. A legacy `TICKER,VALUE` column treats values below $1 million as share prices and larger values as market caps. `--fetch-prices` requests Yahoo Finance v8 chart prices, with Stooq quotes for symbols Yahoo does not return. Share prices are multiplied by SEC period-end shares outstanding, then diluted weighted-average shares, with a direct SEC facts lookup if summary share counts are unavailable. The table includes separate historical-company and SIC-peer-median alpha spreads; the ranked historical growth falls back to revenue CAGR when cash-flow history is unavailable.

### Intangible-Capitalized Value ("Modern GAAP Arbitrage")

Standard GAAP accounting—and therefore retail screeners—treats physical CapEx as an asset, but treats foundational R&D (`ResearchAndDevelopmentExpense`) and brand/customer acquisition (`SellingGeneralAndAdministrativeExpense`) as immediate 100% operating expenses. This artificially depresses Operating Income, Book Equity, and Invested Capital for software, biotech, and high-moat consumer firms.

The **Peters & Taylor Total Q / Damodaran Intangible Capitalization model** is implemented directly in `summary` and `gaap-arbitrage`:

- **Capitalize R&D & 30% of SG&A**:
  - R&D is capitalized and amortized linearly over 5 years (20% per year).
  - 30% of SG&A is capitalized as brand/organizational capital and amortized linearly over 3 years (33.33% per year).
- **Core Formulas**:
  $$\text{Intangible Capital}_t = \sum_{k=0}^4 \left(1 - \frac{k}{5}\right) \text{R\&D}_{t-k} + \sum_{k=0}^2 \left(1 - \frac{k}{3}\right) (0.30 \times \text{SG\&A}_{t-k})$$
  $$\text{Amortization}_t = \frac{1}{5} \sum_{k=1}^5 \text{R\&D}_{t-k} + \frac{1}{3} \sum_{k=1}^3 (0.30 \times \text{SG\&A}_{t-k})$$
  $$\text{Adjusted EBIT}_t = \text{Operating Income}_t + \text{R\&D}_t + 0.30 \times \text{SG\&A}_t - \text{Amortization}_t$$
  $$\text{Adjusted Invested Capital}_t = \text{Stockholders' Equity}_t + \text{Debt}_t - \text{Cash}_t + \text{Intangible Capital}_t$$
  $$\text{Intangible-Adjusted ROIC}_t = \frac{\text{Adjusted EBIT}_t \times (1 - \text{taxRate})}{\frac{\text{Adjusted Invested Capital}_t + \text{Adjusted Invested Capital}_{t-1}}{2}}$$

Inspect any company's intangible adjustments across 3 to 5 years:

```sh
./sec summary AAPL
./sec summary MSFT
```

Screen for companies that look unprofitable or "expensive" on standard GAAP metrics (and are ignored by traditional value funds), but exhibit surging Intangible-Adjusted ROIC and high Intangible-Adjusted Owner Earnings Yield:

```sh
./sec gaap-arbitrage --fetch-prices --min-adj-roic 15 --min-yield 3.5
./sec gaap-arbitrage --prices quotes.csv --surging --screen-expensive
./sec gaap-arbitrage --min-yield 0 --sort roic --universe sp500.txt
```

- **Screener Options**:
  - `--min-adj-roic PCT`: Minimum Intangible-Adjusted ROIC (default `15%`).
  - `--min-yield PCT`: Minimum Intangible-Adjusted Owner Earnings Yield (`(FCF - SBC + Net Intangible Investment) / Market Cap`, default `3.5%`; pass `0` to screen without valuation).
  - `--surging`: Require expanding Adjusted ROIC (latest > prior year or 3Y average, or ROIC spread $\ge 5\%$).
  - `--screen-expensive`: Require company to appear expensive or unprofitable under GAAP (GAAP P/E $\ge 25$, negative P/E / GAAP loss, or ROIC spread $\ge 5\%$).
  - `--max-gaap-roic PCT`: Cap standard GAAP ROIC to isolate firms where GAAP expensing creates the widest distortion.
  - `--sort yield|roic|spread|score`: Sort table by Owner Yield, Adjusted ROIC, ROIC Delta spread, or composite arbitrage score.
  - `--prices FILE` / `--fetch-prices`: Supply valuation file or pull live market prices from Yahoo Finance / Stooq.

### Rank Companies

Screen and rank synced companies by within-industry percentiles:

```sh
./sec rank
./sec rank --universe sp500.txt --years 6 --min-revenue 100000000
./sec rank --exclude-financials
```

- **Eligibility**: Requires a recent filing, 6 annual periods for 5-year trends (`--years 5` for 4-year trends), latest USD revenue ≥ `--min-revenue` (default `$100M`), and positive net income over the last 3 years. Nonfinancials also require positive latest OCF and pass a solvency check when cash and debt facts are present.
- **Peer Grouping**: Uses 2-digit SEC SIC major groups (broadening to SIC divisions when a group has fewer than 5 companies). Missing metrics are shown as `—` and remaining weights are renormalized.

| Model | Scope | Factor Weights |
| :--- | :--- | :--- |
| **Nonfinancial** | Standard | **20%** ROIC · **20%** Owner-earnings margin (`FCF - SBC`) · **15%** Revenue CAGR · **15%** Operating margin & consistency · **10%** Net cash/assets · **10%** EPS CAGR · **10%** Share reduction |
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
