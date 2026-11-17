package store

const companySchema = `
CREATE TABLE IF NOT EXISTS sec_companies (
    cik                 char(10) PRIMARY KEY,
    company_name        text NOT NULL,
    requested_ticker    text NOT NULL,
    sic                 text NOT NULL DEFAULT '',
    sic_description     text NOT NULL DEFAULT '',
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
    PRIMARY KEY (cik, taxonomy, concept, unit, accession_number, period_start, period_end, form, filing_date, frame)
);
CREATE INDEX IF NOT EXISTS sec_facts_by_concept_period ON sec_facts (cik, concept, taxonomy, period_end DESC);
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
CREATE INDEX IF NOT EXISTS sec_filings_by_date ON sec_filings (filing_date DESC, accession_number DESC);
CREATE INDEX IF NOT EXISTS sec_filings_by_company_date ON sec_filings (cik, filing_date DESC, form);
CREATE TABLE IF NOT EXISTS sec_meta (
    key                 text PRIMARY KEY,
    value               text NOT NULL,
    updated_at          text NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS sec_company_summaries (
    cik                          char(10) PRIMARY KEY REFERENCES sec_companies(cik) ON DELETE CASCADE,
    ticker                       text NOT NULL DEFAULT '',
    company_name                 text NOT NULL DEFAULT '',
    sic                          text NOT NULL DEFAULT '',
    currency                     text NOT NULL DEFAULT 'USD',
    latest_period_end            text NOT NULL DEFAULT '',
    periods_count                integer NOT NULL DEFAULT 0,
    latest_filing_date           text NOT NULL DEFAULT '',
    latest_accession             text NOT NULL DEFAULT '',
    latest_financial_filing_date text NOT NULL DEFAULT '',
    filings_count                integer NOT NULL DEFAULT 0,
    latest_share_count           real NOT NULL DEFAULT 0,
    has_latest_share_count       integer NOT NULL DEFAULT 0,
    summary_json                 text NOT NULL DEFAULT '',
    schema_version               integer NOT NULL DEFAULT 1,
    dirty                        integer NOT NULL DEFAULT 0,
    computed_at                  text NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS sec_company_summaries_dirty ON sec_company_summaries (dirty, cik);
CREATE TABLE IF NOT EXISTS sec_rank_variables (
    cik                          char(10) NOT NULL REFERENCES sec_companies(cik) ON DELETE CASCADE,
    years                        integer NOT NULL,
    ticker                       text NOT NULL DEFAULT '',
    company_name                 text NOT NULL DEFAULT '',
    sic                          text NOT NULL DEFAULT '',
    sector                       text NOT NULL DEFAULT '',
    raw_peer_group               text NOT NULL DEFAULT '',
    peer_group                   text NOT NULL DEFAULT '',
    score                        real NOT NULL DEFAULT 0,
    eligible                     integer NOT NULL DEFAULT 0,
    latest_financial_filing_date text NOT NULL DEFAULT '',
    year                         text NOT NULL DEFAULT '',
    trend_years                  integer NOT NULL DEFAULT 0,
    revenue                      real NOT NULL DEFAULT 0,
    currency                     text NOT NULL DEFAULT 'USD',
    roic                         real NOT NULL DEFAULT 0,
    has_roic                     integer NOT NULL DEFAULT 0,
    roe                          real NOT NULL DEFAULT 0,
    has_roe                      integer NOT NULL DEFAULT 0,
    operating_margin             real NOT NULL DEFAULT 0,
    operating_consistency        real NOT NULL DEFAULT 0,
    has_operating_margin         integer NOT NULL DEFAULT 0,
    fcf_margin                   real NOT NULL DEFAULT 0,
    owner_earnings_margin        real NOT NULL DEFAULT 0,
    has_owner_earnings           integer NOT NULL DEFAULT 0,
    has_sbc_adjustment           integer NOT NULL DEFAULT 0,
    revenue_cagr                 real NOT NULL DEFAULT 0,
    has_revenue_growth           integer NOT NULL DEFAULT 0,
    eps_growth                   real NOT NULL DEFAULT 0,
    has_eps_growth               integer NOT NULL DEFAULT 0,
    balance_sheet_quality        real NOT NULL DEFAULT 0,
    has_balance_sheet            integer NOT NULL DEFAULT 0,
    book_value_growth            real NOT NULL DEFAULT 0,
    has_book_value_growth        integer NOT NULL DEFAULT 0,
    share_reduction              real NOT NULL DEFAULT 0,
    has_share_trend              integer NOT NULL DEFAULT 0,
    split_adjusted               integer NOT NULL DEFAULT 0,
    schema_version               integer NOT NULL DEFAULT 1,
    computed_at                  text NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (cik, years)
);
CREATE INDEX IF NOT EXISTS sec_rank_variables_years_eligible ON sec_rank_variables (years, eligible, cik);
CREATE TABLE IF NOT EXISTS sec_gaap_arbitrage_variables (
    cik                          char(10) NOT NULL REFERENCES sec_companies(cik) ON DELETE CASCADE,
    years                        integer NOT NULL,
    ticker                       text NOT NULL DEFAULT '',
    company_name                 text NOT NULL DEFAULT '',
    sic                          text NOT NULL DEFAULT '',
    sic_description              text NOT NULL DEFAULT '',
    sector                       text NOT NULL DEFAULT '',
    currency                     text NOT NULL DEFAULT 'USD',
    eligible                     integer NOT NULL DEFAULT 0,
    latest_financial_filing_date text NOT NULL DEFAULT '',
    gaap_revenue                 real NOT NULL DEFAULT 0,
    gaap_net_income              real NOT NULL DEFAULT 0,
    gaap_operating_income        real NOT NULL DEFAULT 0,
    gaap_operating_margin        real NOT NULL DEFAULT 0,
    gaap_roic                    real NOT NULL DEFAULT 0,
    has_gaap_roic                integer NOT NULL DEFAULT 0,
    gaap_unprofitable            integer NOT NULL DEFAULT 0,
    intangible_capital           real NOT NULL DEFAULT 0,
    adjusted_ebit                real NOT NULL DEFAULT 0,
    adjusted_ebit_margin         real NOT NULL DEFAULT 0,
    adjusted_invested_cap        real NOT NULL DEFAULT 0,
    adjusted_roic                real NOT NULL DEFAULT 0,
    has_adjusted_roic            integer NOT NULL DEFAULT 0,
    avg_adjusted_roic            real NOT NULL DEFAULT 0,
    roic_delta                   real NOT NULL DEFAULT 0,
    surging_roic                 integer NOT NULL DEFAULT 0,
    owner_earnings               real NOT NULL DEFAULT 0,
    has_owner_earnings           integer NOT NULL DEFAULT 0,
    adjusted_owner_earnings      real NOT NULL DEFAULT 0,
    shares_outstanding           real NOT NULL DEFAULT 0,
    base_arbitrage_score         real NOT NULL DEFAULT 0,
    base_arbitrage_signal        text NOT NULL DEFAULT '',
    schema_version               integer NOT NULL DEFAULT 1,
    computed_at                  text NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (cik, years)
);
CREATE INDEX IF NOT EXISTS sec_gaap_arbitrage_variables_years_eligible ON sec_gaap_arbitrage_variables (years, eligible, cik);
`

var precomputeTriggers = []string{
	`CREATE TRIGGER IF NOT EXISTS sec_facts_dirty_insert AFTER INSERT ON sec_facts
WHEN EXISTS (SELECT 1 FROM sec_company_summaries WHERE cik = NEW.cik AND dirty = 0)
BEGIN
    UPDATE sec_company_summaries SET dirty = 1 WHERE cik = NEW.cik;
END`,
	`CREATE TRIGGER IF NOT EXISTS sec_facts_dirty_update AFTER UPDATE OF value, unit, period_start, period_end, filing_date, form, taxonomy, concept ON sec_facts
WHEN EXISTS (SELECT 1 FROM sec_company_summaries WHERE cik = NEW.cik AND dirty = 0)
BEGIN
    UPDATE sec_company_summaries SET dirty = 1 WHERE cik = NEW.cik;
END`,
	`CREATE TRIGGER IF NOT EXISTS sec_facts_dirty_delete AFTER DELETE ON sec_facts
WHEN EXISTS (SELECT 1 FROM sec_company_summaries WHERE cik = OLD.cik AND dirty = 0)
BEGIN
    UPDATE sec_company_summaries SET dirty = 1 WHERE cik = OLD.cik;
END`,
	`CREATE TRIGGER IF NOT EXISTS sec_filings_dirty_insert AFTER INSERT ON sec_filings
WHEN EXISTS (SELECT 1 FROM sec_company_summaries WHERE cik = NEW.cik AND dirty = 0)
BEGIN
    UPDATE sec_company_summaries SET dirty = 1 WHERE cik = NEW.cik;
END`,
	`CREATE TRIGGER IF NOT EXISTS sec_filings_dirty_update AFTER UPDATE OF form, filing_date, report_date ON sec_filings
WHEN EXISTS (SELECT 1 FROM sec_company_summaries WHERE cik = NEW.cik AND dirty = 0)
BEGIN
    UPDATE sec_company_summaries SET dirty = 1 WHERE cik = NEW.cik;
END`,
	`CREATE TRIGGER IF NOT EXISTS sec_filings_dirty_delete AFTER DELETE ON sec_filings
WHEN EXISTS (SELECT 1 FROM sec_company_summaries WHERE cik = OLD.cik AND dirty = 0)
BEGIN
    UPDATE sec_company_summaries SET dirty = 1 WHERE cik = OLD.cik;
END`,
	`CREATE TRIGGER IF NOT EXISTS sec_companies_dirty_update AFTER UPDATE OF company_name, sic, sic_description ON sec_companies
WHEN (OLD.company_name IS NOT NEW.company_name OR OLD.sic IS NOT NEW.sic OR OLD.sic_description IS NOT NEW.sic_description)
  AND EXISTS (SELECT 1 FROM sec_company_summaries WHERE cik = NEW.cik AND dirty = 0)
BEGIN
    UPDATE sec_company_summaries SET dirty = 1 WHERE cik = NEW.cik;
END`,
}
