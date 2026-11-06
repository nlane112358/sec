-- All synced companies share data/sec.db. The application applies this schema
-- automatically when the shared database is first opened.

CREATE TABLE IF NOT EXISTS sec_companies (
    cik                 char(10) PRIMARY KEY,
    company_name        text NOT NULL,
    requested_ticker    text NOT NULL,
    updated_at          text NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS sec_tickers (
    cik                 char(10) NOT NULL REFERENCES sec_companies(cik) ON DELETE CASCADE,
    ticker              text NOT NULL COLLATE NOCASE UNIQUE,
    PRIMARY KEY (cik, ticker)
);

CREATE TABLE IF NOT EXISTS sec_facts (
    cik                 char(10) NOT NULL REFERENCES sec_companies(cik) ON DELETE CASCADE,
    company_name        text NOT NULL,
    ticker              text NOT NULL,
    taxonomy            text NOT NULL,
    concept             text NOT NULL,
    label               text NOT NULL,
    unit                text NOT NULL,
    value               double precision NOT NULL,
    period_start        text NOT NULL DEFAULT '',
    period_end          text NOT NULL,
    filing_date         text NOT NULL,
    form                text NOT NULL,
    accession_number    text NOT NULL,
    frame               text NOT NULL DEFAULT '',
    saved_at            text NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (
        cik, taxonomy, concept, unit, accession_number, period_start, period_end,
        form, filing_date, frame
    )
);

CREATE INDEX IF NOT EXISTS sec_facts_by_concept_period
    ON sec_facts (cik, concept, taxonomy, period_end DESC);

CREATE TABLE IF NOT EXISTS sec_filings (
    cik                 char(10) NOT NULL REFERENCES sec_companies(cik) ON DELETE CASCADE,
    accession_number    text NOT NULL,
    form                text NOT NULL,
    filing_date         text NOT NULL,
    report_date         text NOT NULL DEFAULT '',
    primary_document    text NOT NULL,
    document_title      text NOT NULL DEFAULT '',
    PRIMARY KEY (cik, accession_number)
);

CREATE INDEX IF NOT EXISTS sec_filings_by_date
    ON sec_filings (filing_date DESC, accession_number DESC);

CREATE INDEX IF NOT EXISTS sec_filings_by_company_date
    ON sec_filings (cik, filing_date DESC, form);
