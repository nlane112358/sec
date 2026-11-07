# SEC filing reader and fundamentals screener

Fetch selected company facts and filing history from SEC EDGAR, store multiple
companies in one local SQLite database, compare annual metrics, and rank a
saved investable universe.

## Build and SEC User-Agent

```sh
go build -o sec ./cmd/sec
export SEC_USER_AGENT="Research App contact@example.com"
```

SEC requests need an identifying User-Agent with contact information. Set
`SEC_USER_AGENT` or pass it with `--ua` to `sync`.

## Sync a universe

Sync one or more tickers into the shared `data/sec.db` database:

```sh
./sec sync AAPL
./sec sync AAPL MSFT GOOGL AMZN NVDA META
./sec sync --universe universe.txt --workers 4
```

For a larger universe, put one ticker per line in a file and run:

```sh
./sec sync --universe universe.txt
```

The sync command uses up to four workers by default (`--workers 1` makes it
sequential), shares the SEC rate limiter across all requests, and reports
progress for each company. It downloads selected facts from the
`us-gaap`, `ifrs-full`, and `dei` taxonomies, plus the complete filing history
including amended filings. `--data-dir DIR` changes the directory containing
`sec.db`; `SEC_DATABASE_DIR` can set the same default.

Database files and SQLite sidecars are ignored by Git and remain local to your
machine.

## Inspect and export facts

Show annual metrics for a company:

```sh
./sec summary AAPL
./sec summary AAPL --years 3
```

The summary includes revenue, operating income, net income, operating cash
flow, capital expenditure, depreciation and amortization, free cash flow, cash,
borrowings, stock-based compensation (SBC), an SBC-adjusted FCF proxy, and
shares. Free cash flow is operating cash flow minus capital expenditure. The
SBC-adjusted proxy subtracts reported SBC from FCF; it is not a GAAP measure and
may be unavailable when a filing does not report a matching fact. Shares use
period-end outstanding shares when available and otherwise diluted
weighted-average shares.

Run a two-stage DCF using annual SEC facts:

```sh
./sec dcf AAPL
./sec dcf AAPL --growth 8 --discount 9 --terminal 2.5 --years 5
./sec dcf GOOGL --mode revenue --growth 9 --beta 1.1 --market-cap 2.5e12
./sec dcf META --mode revenue --capex-mode normalized
./sec dcf PLTR --debt 0
```

Rates are entered as percentages. `--mode fcf` (the default) grows operating
cash flow less CapEx. Its default growth assumption is the three-year
historical FCF CAGR when four consecutive annual periods with positive FCF are
available. With raw CapEx, the model estimates the growth CapEx wedge as CapEx
above D&A and fades that wedge linearly to zero by the final forecast year.
Choose `--capex-mode normalized` or `--normalize-capex` to use D&A as the
CapEx/maintenance-reinvestment proxy throughout the forecast.

`--mode revenue` builds FCFF from forecast revenue, a trailing historical
operating margin, an effective tax rate, D&A, CapEx, and the change in net
working capital. When `--growth` is omitted, this mode uses the three-year
historical revenue CAGR when four consecutive annual periods are available.
The working-capital input is current assets less current liabilities, which is
an SEC-data proxy and can include cash and financing items.

If `--discount` is omitted, the command estimates WACC from the risk-free rate,
beta, equity risk premium, debt cost, tax rate, and capital weights. The
defaults are a 4.84% risk-free rate (the [US 10-year Treasury yield on Nov. 5,
2026](https://home.treasury.gov/resource-center/data-chart-center/interest-rates/TextView?field_tdr_date_value_month=202611&type=daily_treasury_yield_curve)),
5.5% equity risk premium, 1.0 beta, 5.0% fallback cost of debt, and 21%
fallback tax rate. The risk-free rate is a dated default; update it with
`--risk-free` as market rates change, and use a rate that matches the company's
reporting currency. Historical effective tax and interest
expense divided by average debt are used when the corresponding CLI overrides
are omitted. EDGAR does not report beta or market capitalization: provide
`--beta` and optionally `--market-cap`; otherwise reported book equity is used
as the equity-weight proxy. The debt weight uses reported net debt (borrowings
less cash, floored at zero), while the DCF equity bridge separately adds cash
and subtracts gross debt. Monetary assumptions such as `--market-cap` must use
the company's reported currency units. If a filing confirms a zero debt balance
but EDGAR has no zero-valued XBRL fact, pass `--debt 0`; the override is shown
in the output and is not saved to the database.

The command prints projected cash flows, the EV-to-equity bridge, a per-share
estimate, and a sensitivity table using nearby discount and terminal growth
rates. Shares use period-end outstanding shares when available and otherwise
diluted weighted-average shares. FCF mode uses operating cash flow less CapEx;
that value is a useful FCF proxy, but it is not a complete unlevered FCFF
calculation. Revenue mode provides a more explicit FCFF build, though its
working-capital proxy has the limitation described above. Sync a company again
to import newly added SEC concepts such as interest expense before relying on
the historical cost-of-debt estimate.

List the companies saved in the shared database:

```sh
./sec list
```

Export a company's extracted facts as CSV or JSON:

```sh
./sec export AAPL --format csv > AAPL-facts.csv
./sec export AAPL --format json > AAPL-facts.json
```

## Rank companies

Rank every synced company or restrict the ranking to tickers in a universe file:

```sh
./sec rank
./sec rank --universe universe.txt --years 6 --min-revenue 100000000
./sec rank --exclude-financials
```

The screen requires a recent filing, six annual periods for five-year trends,
latest revenue above the threshold, and positive net income in the latest three
years. Nonfinancial companies also need positive latest operating cash flow;
solvency is checked when cash and debt facts are both available. Run
`./sec rank --years 5` for four-year trends.

Nonfinancial companies use within-industry percentiles for five-year average
ROIC (20%), revenue CAGR (15%), operating margin and profitable-year
consistency (15%), owner-earnings margin proxy (20%), net cash/assets (10%),
EPS per-share growth (10%), and diluted-share reduction (10%). Financial
companies appear in a separate table and use five-year ROE (30%), revenue CAGR
(20%), book value per share growth (20%), EPS per share growth (20%), and
diluted-share reduction (10%). Banks, insurers, investment companies, and real
estate businesses have different accounting; the financial score is a
preliminary screen and does not yet include insurer combined ratios or
investment-return measures.

Peer percentiles use SEC SIC two-digit major groups as a broad industry proxy;
groups with fewer than five companies broaden to the SIC division. EDGAR does
not provide GICS classifications. SIC 6000-6799 is treated as financial,
including real estate. Re-sync older saved companies to populate SIC metadata.
Companies with missing SIC stay in an Unclassified peer group. When a factor is
unavailable, it is omitted and the available factor weights are renormalized. A
missing operating-income fact is shown as `—`, not scored as a zero margin.
Operating income is derived only when the filing provides revenue, cost of
revenue, SG&A, and R&D in matching units; re-sync companies to import these newly
supported concepts.

Share trends use diluted weighted-average shares rather than point-in-time DEI
shares. The rank prefers the latest restated SEC facts and adjusts older values
when adjacent periods differ by nearly an exact common split ratio; adjusted
share-reduction values are marked with `*`. This catches historical splits
outside the comparative periods in a filing, but the SEC company-facts feed
does not provide a definitive split-event series, so review marked adjustments.
Owner-earnings margin is FCF less stock-based compensation when matching SBC
facts are available, otherwise reported FCF. It is not a market FCF yield:
market prices and market capitalization are not part of SEC company facts.
Scores are peer-relative, not intrinsic-value estimates.

The ranking uses the facts available in SEC filings. Companies with missing,
non-USD, or inconsistent concepts may be excluded. Review the underlying
figures and filings before relying on a rank.

## Open SEC filings

```sh
./sec read AAPL
```

Select a filing to open its official SEC URL in your browser. The CLI does not
download or cache filing HTML. Use `n` and `p` to move through the filing list,
`/text` to search, and `q` to quit.

SEC's ticker mapping is cached for 24 hours under the operating system's user
cache directory (or `SEC_CACHE_DIR` when set).
