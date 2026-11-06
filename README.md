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
```

For a larger universe, put one ticker per line in a file and run:

```sh
./sec sync --universe universe.txt
```

The sync command processes the list in one process, shares the SEC rate limiter,
and reports progress for each company. It downloads selected facts from the
`us-gaap`, `ifrs-full`, and `dei` taxonomies, plus the complete filing history
including amended filings. `--data-dir DIR` changes the directory containing
`sec.db`; `SEC_DATABASE_DIR` can set the same default.

Database files and SQLite sidecars are ignored by Git and remain local to your
machine.

### Merge existing per-company databases

If you have older files such as `data/AAPL.db`, merge them into the shared
database with:

```sh
./sec migrate
```

This copies the saved companies, facts, and filings into `data/sec.db`. It is
safe to rerun; it leaves the source databases in place.

## Inspect and export facts

Show annual metrics for a company:

```sh
./sec summary AAPL
./sec summary AAPL --years 3
```

The summary includes revenue, operating income, net income, operating cash
flow, capital expenditure, free cash flow, cash, borrowings, and shares. Free
cash flow is operating cash flow minus capital expenditure. Shares use
period-end outstanding shares when available and otherwise diluted
weighted-average shares.

Run a two-stage DCF using the latest annual free cash flow, cash, debt, and
shares in the shared database:

```sh
./sec dcf AAPL
./sec dcf AAPL --growth 8 --discount 9 --terminal 2.5 --years 5
```

Rates are entered as percentages. If `--growth` is omitted, the model uses the
three-year historical FCF CAGR when four consecutive annual periods with
positive FCF are available. Otherwise it reports that a growth assumption is
required. The command prints projected cash flows, the EV-to-equity bridge, a
per-share estimate, and a sensitivity table using nearby discount and terminal
growth rates. It requires positive latest annual FCF and same-currency cash and
debt facts. The model uses SEC-reported diluted weighted-average shares when
period-end shares are unavailable; it does not fetch a market price. The
cash-flow input is operating cash flow minus CapEx, a useful FCF proxy but not a
complete unlevered FCFF calculation, so interpret the EV-to-equity bridge with
that limitation in mind.

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
./sec rank --universe universe.txt --years 5 --min-revenue 100000000
```

The first-pass screen requires a 10-K, 10-Q, 20-F, 40-F, or 6-K within the past
15 months; at least four annual periods with USD facts; latest revenue above the
threshold; positive operating cash flow and net income; and a basic
debt-to-free-cash-flow solvency check. Eligible companies are ranked by
percentile across three-year average free-cash-flow margin, three-year revenue
CAGR, cash conversion, and three-year share-count reduction. Weights are 35%,
35%, 15%, and 15%, respectively. The score ranks historical financial quality
and growth; it does not include market valuation or a share price, and it is
not an intrinsic-value estimate.

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
cache directory (or `SEC_CACHE_DIR` when set). The SQLite schema is documented
in [migrations/schema.sql](migrations/schema.sql).
