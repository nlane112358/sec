package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// QuoteRecord caches a market quote in SQLite with fetch provenance and timestamp.
type QuoteRecord struct {
	Ticker    string
	Price     float64
	High52    float64
	Low52     float64
	Currency  string
	Source    string
	FetchedAt time.Time
}

// FXRateRecord caches a currency-to-USD FX rate in SQLite with timestamp.
type FXRateRecord struct {
	Currency  string
	RateToUSD float64
	Source    string
	FetchedAt time.Time
}

// CompanyCacheStatus describes whether a ticker is hydrated locally and how fresh it is.
type CompanyCacheStatus struct {
	Ticker             string
	CIK                string
	CompanyName        string
	SIC                string
	UpdatedAt          time.Time
	HasFacts           bool
	FactsCount         int
	FilingsCount       int
	LatestPeriodEnd    string
	LatestFilingDate   string
	LatestFinancialEnd string
	LatestForm         string
	LatestReportDate   string
	Currency           string
}

// DatabaseOverviewStats summarizes local SQLite cache coverage across all tables.
type DatabaseOverviewStats struct {
	Companies     int
	HydratedCount int
	Facts         int
	Filings       int
	Quotes        int
	FXRates       int
	CleanSummaries int
	DirtySummaries int
	Rank5Count    int
	Rank6Count    int
	Arb5Count     int
	CacheEpoch    int64
	OldestSync    time.Time
	NewestSync    time.Time
}

func parseStoreTime(raw string) time.Time {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return time.Time{}
	}
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		"2006-01-02",
	} {
		if t, err := time.ParseInLocation(layout, trimmed, time.UTC); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

func formatStoreTime(t time.Time) string {
	if t.IsZero() {
		t = time.Now().UTC()
	}
	return t.UTC().Format(time.RFC3339)
}

// SaveQuote upserts a single market quote into sec_quotes.
func (s *Store) SaveQuote(ctx context.Context, q QuoteRecord) error {
	return s.SaveQuotes(ctx, []QuoteRecord{q})
}

// SaveQuotes upserts multiple market quotes in a single transaction.
func (s *Store) SaveQuotes(ctx context.Context, quotes []QuoteRecord) error {
	if len(quotes) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin save quotes: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
INSERT INTO sec_quotes (ticker, price, high_52, low_52, currency, source, fetched_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (ticker) DO UPDATE SET
    price = excluded.price,
    high_52 = excluded.high_52,
    low_52 = excluded.low_52,
    currency = excluded.currency,
    source = excluded.source,
    fetched_at = excluded.fetched_at`)
	if err != nil {
		return fmt.Errorf("prepare save quote: %w", err)
	}
	defer stmt.Close()

	for _, q := range quotes {
		ticker := strings.ToUpper(strings.TrimSpace(q.Ticker))
		if ticker == "" || !finite(q.Price) || q.Price <= 0 {
			continue
		}
		cur := strings.ToUpper(strings.TrimSpace(q.Currency))
		if cur == "" {
			cur = "USD"
		}
		src := strings.TrimSpace(q.Source)
		if src == "" {
			src = "live"
		}
		if _, err := stmt.ExecContext(ctx, ticker, q.Price, q.High52, q.Low52, cur, src, formatStoreTime(q.FetchedAt)); err != nil {
			return fmt.Errorf("save quote for %s: %w", ticker, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit quotes: %w", err)
	}
	return nil
}

// LoadQuote loads a cached quote for ticker if present.
func (s *Store) LoadQuote(ctx context.Context, ticker string) (QuoteRecord, bool, error) {
	var rec QuoteRecord
	var fetchedRaw string
	err := s.db.QueryRowContext(ctx, `
SELECT ticker, price, high_52, low_52, currency, source, fetched_at
FROM sec_quotes WHERE ticker = ? COLLATE NOCASE LIMIT 1`, strings.TrimSpace(ticker)).Scan(
		&rec.Ticker, &rec.Price, &rec.High52, &rec.Low52, &rec.Currency, &rec.Source, &fetchedRaw,
	)
	if err == sql.ErrNoRows {
		return QuoteRecord{}, false, nil
	}
	if err != nil {
		return QuoteRecord{}, false, fmt.Errorf("load quote for %s: %w", ticker, err)
	}
	rec.Ticker = strings.ToUpper(strings.TrimSpace(rec.Ticker))
	rec.FetchedAt = parseStoreTime(fetchedRaw)
	return rec, true, nil
}

// LoadQuotes loads cached quotes for the specified tickers (or all cached quotes if empty).
func (s *Store) LoadQuotes(ctx context.Context, tickers []string) (map[string]QuoteRecord, error) {
	out := make(map[string]QuoteRecord)
	var filterSet map[string]struct{}
	if len(tickers) > 0 {
		filterSet = make(map[string]struct{}, len(tickers))
		for _, t := range tickers {
			clean := strings.ToUpper(strings.TrimSpace(t))
			if clean != "" {
				filterSet[clean] = struct{}{}
			}
		}
		if len(filterSet) == 0 {
			return out, nil
		}
	}

	var query strings.Builder
	var args []any
	query.WriteString(`SELECT ticker, price, high_52, low_52, currency, source, fetched_at FROM sec_quotes`)
	if len(filterSet) > 0 && len(filterSet) <= 400 {
		placeholders := strings.TrimRight(strings.Repeat("?,", len(filterSet)), ",")
		query.WriteString(" WHERE ticker IN (" + placeholders + ")")
		for t := range filterSet {
			args = append(args, t)
		}
	}

	rows, err := s.db.QueryContext(ctx, query.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("load quotes: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var rec QuoteRecord
		var fetchedRaw string
		if err := rows.Scan(&rec.Ticker, &rec.Price, &rec.High52, &rec.Low52, &rec.Currency, &rec.Source, &fetchedRaw); err != nil {
			return nil, fmt.Errorf("scan quote: %w", err)
		}
		rec.Ticker = strings.ToUpper(strings.TrimSpace(rec.Ticker))
		if len(filterSet) > 400 {
			if _, ok := filterSet[rec.Ticker]; !ok {
				continue
			}
		}
		rec.FetchedAt = parseStoreTime(fetchedRaw)
		out[rec.Ticker] = rec
	}
	return out, rows.Err()
}

// PruneQuotes removes cached quotes older than cutoff (or all if cutoff is zero).
func (s *Store) PruneQuotes(ctx context.Context, cutoff time.Time) (int64, error) {
	if cutoff.IsZero() {
		res, err := s.db.ExecContext(ctx, `DELETE FROM sec_quotes`)
		if err != nil {
			return 0, fmt.Errorf("prune all quotes: %w", err)
		}
		return res.RowsAffected()
	}
	rows, err := s.db.QueryContext(ctx, `SELECT ticker, fetched_at FROM sec_quotes`)
	if err != nil {
		return 0, fmt.Errorf("query quotes for prune: %w", err)
	}
	defer rows.Close()

	var toDelete []string
	for rows.Next() {
		var ticker, fetchedRaw string
		if err := rows.Scan(&ticker, &fetchedRaw); err != nil {
			return 0, err
		}
		t := parseStoreTime(fetchedRaw)
		if t.IsZero() || t.Before(cutoff) {
			toDelete = append(toDelete, ticker)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	_ = rows.Close()
	if len(toDelete) == 0 {
		return 0, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `DELETE FROM sec_quotes WHERE ticker = ?`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()
	var deleted int64
	for _, tk := range toDelete {
		res, err := stmt.ExecContext(ctx, tk)
		if err != nil {
			return deleted, err
		}
		n, _ := res.RowsAffected()
		deleted += n
	}
	return deleted, tx.Commit()
}

// SaveFXRate upserts a currency FX rate into sec_fx_rates.
func (s *Store) SaveFXRate(ctx context.Context, rec FXRateRecord) error {
	cur := strings.ToUpper(strings.TrimSpace(rec.Currency))
	if cur == "" || !finite(rec.RateToUSD) || rec.RateToUSD <= 0 {
		return nil
	}
	src := strings.TrimSpace(rec.Source)
	if src == "" {
		src = "live"
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO sec_fx_rates (currency, rate_to_usd, source, fetched_at)
VALUES (?, ?, ?, ?)
ON CONFLICT (currency) DO UPDATE SET
    rate_to_usd = excluded.rate_to_usd,
    source = excluded.source,
    fetched_at = excluded.fetched_at`, cur, rec.RateToUSD, src, formatStoreTime(rec.FetchedAt))
	if err != nil {
		return fmt.Errorf("save FX rate for %s: %w", cur, err)
	}
	return nil
}

// LoadFXRate reads a cached FX rate for currency if present.
func (s *Store) LoadFXRate(ctx context.Context, currency string) (FXRateRecord, bool, error) {
	cur := strings.ToUpper(strings.TrimSpace(currency))
	var rec FXRateRecord
	var fetchedRaw string
	err := s.db.QueryRowContext(ctx, `
SELECT currency, rate_to_usd, source, fetched_at
FROM sec_fx_rates WHERE currency = ? COLLATE NOCASE LIMIT 1`, cur).Scan(
		&rec.Currency, &rec.RateToUSD, &rec.Source, &fetchedRaw,
	)
	if err == sql.ErrNoRows {
		return FXRateRecord{}, false, nil
	}
	if err != nil {
		return FXRateRecord{}, false, fmt.Errorf("load FX rate for %s: %w", cur, err)
	}
	rec.Currency = strings.ToUpper(strings.TrimSpace(rec.Currency))
	rec.FetchedAt = parseStoreTime(fetchedRaw)
	return rec, true, nil
}

// LoadAllFXRates loads all cached FX rates from SQLite.
func (s *Store) LoadAllFXRates(ctx context.Context) ([]FXRateRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT currency, rate_to_usd, source, fetched_at FROM sec_fx_rates ORDER BY currency`)
	if err != nil {
		return nil, fmt.Errorf("load FX rates: %w", err)
	}
	defer rows.Close()
	var out []FXRateRecord
	for rows.Next() {
		var rec FXRateRecord
		var fetchedRaw string
		if err := rows.Scan(&rec.Currency, &rec.RateToUSD, &rec.Source, &fetchedRaw); err != nil {
			return nil, err
		}
		rec.Currency = strings.ToUpper(strings.TrimSpace(rec.Currency))
		rec.FetchedAt = parseStoreTime(fetchedRaw)
		out = append(out, rec)
	}
	return out, rows.Err()
}

// PruneFXRates removes cached FX rates older than cutoff (or all if cutoff is zero).
func (s *Store) PruneFXRates(ctx context.Context, cutoff time.Time) (int64, error) {
	if cutoff.IsZero() {
		res, err := s.db.ExecContext(ctx, `DELETE FROM sec_fx_rates`)
		if err != nil {
			return 0, fmt.Errorf("prune FX rates: %w", err)
		}
		return res.RowsAffected()
	}
	rows, err := s.db.QueryContext(ctx, `SELECT currency, fetched_at FROM sec_fx_rates`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var toDelete []string
	for rows.Next() {
		var cur, fetchedRaw string
		if err := rows.Scan(&cur, &fetchedRaw); err != nil {
			return 0, err
		}
		t := parseStoreTime(fetchedRaw)
		if t.IsZero() || t.Before(cutoff) {
			toDelete = append(toDelete, cur)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	_ = rows.Close()
	if len(toDelete) == 0 {
		return 0, nil
	}
	var deleted int64
	for _, cur := range toDelete {
		res, err := s.db.ExecContext(ctx, `DELETE FROM sec_fx_rates WHERE currency = ?`, cur)
		if err != nil {
			return deleted, err
		}
		n, _ := res.RowsAffected()
		deleted += n
	}
	return deleted, nil
}

// CompanyHydrationStatus checks whether a single ticker is locally hydrated (with facts)
// and collects latest filing/sync metadata for freshness indicators.
func (s *Store) CompanyHydrationStatus(ctx context.Context, ticker string) (CompanyCacheStatus, error) {
	clean := strings.ToUpper(strings.TrimSpace(ticker))
	status := CompanyCacheStatus{Ticker: clean}

	var updatedRaw, summaryCurrency, latestPeriod, latestFiling, latestFinFiling string
	var periodsCount, filingsCount int
	err := s.db.QueryRowContext(ctx, `
SELECT c.cik, c.company_name, t.ticker, c.sic, c.updated_at,
       COALESCE(cs.currency, ''), COALESCE(cs.latest_period_end, ''), COALESCE(cs.periods_count, 0),
       COALESCE(cs.latest_filing_date, ''), COALESCE(cs.latest_financial_filing_date, ''), COALESCE(cs.filings_count, 0)
FROM sec_tickers t
JOIN sec_companies c ON c.cik = t.cik
LEFT JOIN sec_company_summaries cs ON cs.cik = c.cik
WHERE t.ticker = ? COLLATE NOCASE LIMIT 1`, clean).Scan(
		&status.CIK, &status.CompanyName, &status.Ticker, &status.SIC, &updatedRaw,
		&summaryCurrency, &latestPeriod, &periodsCount,
		&latestFiling, &latestFinFiling, &filingsCount,
	)
	if err == sql.ErrNoRows {
		return status, nil
	}
	if err != nil {
		return status, fmt.Errorf("query hydration status for %s: %w", ticker, err)
	}

	status.UpdatedAt = parseStoreTime(updatedRaw)
	status.Currency = summaryCurrency
	status.LatestPeriodEnd = latestPeriod
	status.LatestFilingDate = latestFiling
	status.LatestFinancialEnd = latestFinFiling
	status.FilingsCount = filingsCount

	if periodsCount > 0 {
		status.HasFacts = true
		status.FactsCount = periodsCount
	} else {
		var hasFact int
		if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sec_facts WHERE cik = ? LIMIT 1)`, status.CIK).Scan(&hasFact); err == nil && hasFact != 0 {
			status.HasFacts = true
			status.FactsCount = 1
		}
	}

	// Find most recent primary financial filing form & report date for freshness badge (e.g. "Q3 2026 10-Q").
	_ = s.db.QueryRowContext(ctx, `
SELECT form, COALESCE(NULLIF(report_date, ''), filing_date), filing_date
FROM sec_filings
WHERE cik = ? AND form IN ('10-K', '10-K/A', '10-Q', '10-Q/A', '20-F', '20-F/A', '40-F', '40-F/A')
ORDER BY COALESCE(NULLIF(report_date, ''), filing_date) DESC, filing_date DESC, accession_number DESC
LIMIT 1`, status.CIK).Scan(&status.LatestForm, &status.LatestReportDate, &status.LatestFilingDate)
	if status.LatestForm == "" {
		_ = s.db.QueryRowContext(ctx, `
SELECT form, COALESCE(NULLIF(report_date, ''), filing_date), filing_date
FROM sec_filings
WHERE cik = ? AND form IN ('6-K', '6-K/A')
ORDER BY COALESCE(NULLIF(report_date, ''), filing_date) DESC, filing_date DESC, accession_number DESC
LIMIT 1`, status.CIK).Scan(&status.LatestForm, &status.LatestReportDate, &status.LatestFilingDate)
	}

	return status, nil
}

// CompanyHydrationStatuses checks local hydration state for a batch of tickers in minimal queries.
func (s *Store) CompanyHydrationStatuses(ctx context.Context, tickers []string) (map[string]CompanyCacheStatus, error) {
	out := make(map[string]CompanyCacheStatus, len(tickers))
	if len(tickers) == 0 {
		return out, nil
	}
	wanted := make(map[string]struct{}, len(tickers))
	for _, tk := range tickers {
		clean := strings.ToUpper(strings.TrimSpace(tk))
		if clean != "" {
			wanted[clean] = struct{}{}
		}
	}
	if len(wanted) == 0 {
		return out, nil
	}

	// First check sec_tickers joined with sec_companies and sec_company_summaries.
	var query strings.Builder
	var args []any
	query.WriteString(`
SELECT t.ticker, c.cik, c.company_name, c.sic, c.updated_at,
       COALESCE(cs.currency, ''), COALESCE(cs.latest_period_end, ''), COALESCE(cs.periods_count, 0),
       COALESCE(cs.latest_filing_date, ''), COALESCE(cs.latest_financial_filing_date, ''), COALESCE(cs.filings_count, 0)
FROM sec_tickers t
JOIN sec_companies c ON c.cik = t.cik
LEFT JOIN sec_company_summaries cs ON cs.cik = c.cik`)
	useSQLFilter := len(wanted) <= 400
	if useSQLFilter {
		placeholders := strings.TrimRight(strings.Repeat("?,", len(wanted)), ",")
		query.WriteString(" WHERE t.ticker IN (" + placeholders + ")")
		for tk := range wanted {
			args = append(args, tk)
		}
	}

	rows, err := s.db.QueryContext(ctx, query.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("query batch hydration status: %w", err)
	}
	defer rows.Close()

	needFactCheck := make(map[string]string) // cik -> ticker
	for rows.Next() {
		var st CompanyCacheStatus
		var updatedRaw string
		var periodsCount int
		if err := rows.Scan(
			&st.Ticker, &st.CIK, &st.CompanyName, &st.SIC, &updatedRaw,
			&st.Currency, &st.LatestPeriodEnd, &periodsCount,
			&st.LatestFilingDate, &st.LatestFinancialEnd, &st.FilingsCount,
		); err != nil {
			return nil, fmt.Errorf("scan batch hydration status: %w", err)
		}
		upper := strings.ToUpper(strings.TrimSpace(st.Ticker))
		if !useSQLFilter {
			if _, ok := wanted[upper]; !ok {
				continue
			}
		}
		st.Ticker = upper
		st.UpdatedAt = parseStoreTime(updatedRaw)
		if periodsCount > 0 {
			st.HasFacts = true
			st.FactsCount = periodsCount
		} else {
			needFactCheck[st.CIK] = upper
		}
		out[upper] = st
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	_ = rows.Close()

	// For any ticker with a company row but no populated summary yet, check sec_facts existence.
	if len(needFactCheck) > 0 {
		for cik, ticker := range needFactCheck {
			var exists int
			if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sec_facts WHERE cik = ? LIMIT 1)`, cik).Scan(&exists); err == nil && exists != 0 {
				st := out[ticker]
				st.HasFacts = true
				st.FactsCount = 1
				out[ticker] = st
			}
		}
	}

	return out, nil
}

// DatabaseOverviewStats returns aggregate counts and sync freshness across the database.
func (s *Store) DatabaseOverviewStats(ctx context.Context, summarySchema, rankSchema, arbSchema int) (DatabaseOverviewStats, error) {
	var stats DatabaseOverviewStats
	pStats, err := s.PrecomputeStats(ctx, summarySchema, rankSchema, arbSchema)
	if err != nil {
		return stats, err
	}
	stats.Companies = pStats.TotalCompanies
	stats.CleanSummaries = pStats.CleanSummaries
	stats.DirtySummaries = pStats.DirtySummaries
	stats.Rank5Count = pStats.Rank5Count
	stats.Rank6Count = pStats.Rank6Count
	stats.Arb5Count = pStats.Arb5Count

	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM (SELECT DISTINCT cik FROM sec_facts)`).Scan(&stats.HydratedCount)
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sec_facts`).Scan(&stats.Facts)
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sec_filings`).Scan(&stats.Filings)
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sec_quotes`).Scan(&stats.Quotes)
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sec_fx_rates`).Scan(&stats.FXRates)
	stats.CacheEpoch, _ = s.CacheEpoch(ctx)

	var minUpdated, maxUpdated string
	_ = s.db.QueryRowContext(ctx, `SELECT COALESCE(MIN(updated_at), ''), COALESCE(MAX(updated_at), '') FROM sec_companies`).Scan(&minUpdated, &maxUpdated)
	stats.OldestSync = parseStoreTime(minUpdated)
	stats.NewestSync = parseStoreTime(maxUpdated)
	return stats, nil
}

// PruneEmptyCompanies deletes company records that have zero facts and zero filings.
func (s *Store) PruneEmptyCompanies(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
DELETE FROM sec_companies
WHERE NOT EXISTS (SELECT 1 FROM sec_facts f WHERE f.cik = sec_companies.cik)
  AND NOT EXISTS (SELECT 1 FROM sec_filings fl WHERE fl.cik = sec_companies.cik)`)
	if err != nil {
		return 0, fmt.Errorf("prune empty companies: %w", err)
	}
	return res.RowsAffected()
}

// Vacuum compacts the SQLite database file after pruning.
func (s *Store) Vacuum(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `VACUUM`)
	if err != nil {
		return fmt.Errorf("vacuum SQLite database: %w", err)
	}
	return nil
}
