# SEC filing reader

Fetch company facts and SEC filing details, then inspect saved financial data or
browse filings in the terminal.

## Build

```sh
go build -o sec ./cmd/sec
```

Set `SEC_USER_AGENT` to an identifying agent string with a contact email, or
pass the same value with `--ua` when syncing data from SEC EDGAR.

## Sync company data

```sh
./sec sync AAPL
```

The command downloads facts from the `us-gaap`, `ifrs-full`, and `dei`
taxonomies, along with the company's full filing history, including amended
filings. Progress is printed to stderr while SEC requests run. Pass another
storage directory with `--data-dir` or set `SEC_DATABASE_DIR`.

Each company is saved in a separate SQLite database, such as
`data/AAPL.db`. Database files and SQLite sidecars are ignored by Git and stay
local to your machine.

## Summary and exports

Show key annual metrics for the five most recent fiscal years (or choose three
or four):

```sh
./sec summary AAPL
./sec summary AAPL --years 3
```

The summary reads saved data and does not need a SEC User-Agent. Free cash flow
is operating cash flow minus capital expenditure; shares use period-end
outstanding shares when available and otherwise diluted weighted-average
shares.

Export all saved facts as CSV or JSON. CSV is the default format:

```sh
./sec export AAPL --format csv > AAPL-facts.csv
./sec export AAPL --format json > AAPL-facts.json
```

List locally cached companies with:

```sh
./sec list
```

## Open filings in your browser

```sh
./sec read AAPL
```

Choose a filing number to open its official SEC document URL in your default
browser. The browser loads the filing directly from SEC EDGAR; the CLI does not
download or cache filing HTML. Use `n` and `p` to move through the filing list,
`/text` to search, and `q` to quit.

To refresh a company's facts and filing list, run `sec sync TICKER` before
opening the reader. SEC's ticker mapping is cached for 24 hours under the
operating system's user cache directory (or `SEC_CACHE_DIR` when set). The
reader uses `open` on macOS, `xdg-open` on Linux, and the default URL handler on
Windows.

For raw queries, the SQLite schema is documented in
[migrations/schema.sql](migrations/schema.sql).
