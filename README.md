# SEC filing reader

Fetch company facts and SEC filing details, then browse and read filings in
the terminal.

## Build

```sh
go build -o sec ./cmd/sec
```

Set `SEC_USER_AGENT` to an identifying agent string with a contact email, or
pass the same value with `-ua`.

## Save a company

```sh
./sec -ticker AAPL
```

The command downloads company facts from the `us-gaap`, `ifrs-full`, and `dei`
taxonomies, along with the company's complete filing history from SEC EDGAR.
It creates a separate SQLite database for the company at
`data/AAPL.db`. Choose another storage directory with `-data-dir` or
`SEC_DATABASE_DIR`.

The database schema is created automatically. The SQL definition is also
available in [migrations/schema.sql](migrations/schema.sql).

## Read filings in the terminal

```sh
./sec -ticker AAPL -read
```

Choose a filing number to read its main document from SEC EDGAR. Use `n` and
`p` to move through the filing list or document pages, `/text` to search, `g N`
to jump to a document page, and `b` to return to the list. The reader converts
the filing HTML to plain text and keeps the original filing in SEC EDGAR.

To refresh a company’s facts and filing list, run the save command again before
opening the reader.

SEC's ticker mapping is cached for 24 hours under the operating system's user
cache directory (or `SEC_CACHE_DIR` when set). Opened filing HTML is cached at
`<data-dir>/documents/<accession>.html` and reused on later reads.

## Inspect saved facts

```sh
sqlite3 data/AAPL.db
```

For example:

```sql
SELECT taxonomy, concept, label, unit, value, period_start, period_end
FROM sec_facts
ORDER BY concept, period_end DESC
LIMIT 8;
```
