package store

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
)

const CacheEpochMetaKey = "cache_epoch"

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// CacheEpoch returns the monotonically increasing invalidation epoch stored in sec_meta.
func (s *Store) CacheEpoch(ctx context.Context) (int64, error) {
	if s == nil || s.db == nil {
		return 0, nil
	}
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM sec_meta WHERE key = ?`, CacheEpochMetaKey).Scan(&raw)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read cache epoch: %w", err)
	}
	epoch, convErr := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if convErr != nil {
		return 0, nil
	}
	return epoch, nil
}

// BumpCacheEpoch increments cache_epoch in sec_meta so external processes
// can detect database updates made by sync or precompute.
func (s *Store) BumpCacheEpoch(ctx context.Context) error {
	if s == nil || s.db == nil {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO sec_meta (key, value, updated_at)
VALUES (?, '1', CURRENT_TIMESTAMP)
ON CONFLICT (key) DO UPDATE SET
    value = CAST(COALESCE(NULLIF(sec_meta.value, ''), '0') AS INTEGER) + 1,
    updated_at = CURRENT_TIMESTAMP`, CacheEpochMetaKey)
	if err != nil {
		return fmt.Errorf("bump cache epoch: %w", err)
	}
	return nil
}

// LatestShareCountByCIK queries sec_facts directly for the most recent valid share count of a CIK.
func (s *Store) LatestShareCountByCIK(ctx context.Context, cik string) (float64, bool) {
	var outstanding, diluted float64
	_ = s.db.QueryRowContext(ctx, `
		SELECT value FROM sec_facts
		WHERE cik = ?
		  AND concept IN (
		      'EntityCommonStockSharesOutstanding',
		      'CommonStockSharesOutstanding',
		      'NumberOfSharesOutstanding'
		  )
		  AND unit = 'shares'
		  AND value > 0
		ORDER BY period_end DESC, filing_date DESC, value DESC
		LIMIT 1`, cik).Scan(&outstanding)

	_ = s.db.QueryRowContext(ctx, `
		SELECT value FROM sec_facts
		WHERE cik = ?
		  AND concept IN (
		      'WeightedAverageNumberOfDilutedSharesOutstanding',
		      'WeightedAverageNumberOfSharesOutstandingDiluted',
		      'DilutedWeightedAverageNumberOfOrdinarySharesOutstanding'
		  )
		  AND unit = 'shares'
		  AND value > 0
		ORDER BY period_end DESC, filing_date DESC, value DESC
		LIMIT 1`, cik).Scan(&diluted)

	hasOutstanding := finite(outstanding) && outstanding > 0
	hasDiluted := finite(diluted) && diluted > 0

	if hasOutstanding && hasDiluted {
		if outstanding < 0.5*diluted {
			return diluted, true
		}
		return outstanding, true
	}
	if hasOutstanding {
		return outstanding, true
	}
	if hasDiluted {
		return diluted, true
	}
	return 0, false
}

// FilingMetadataByCIK reads latest filing & financial filing watermarks for a CIK using the company-date index.
func (s *Store) FilingMetadataByCIK(ctx context.Context, cik string) (latestFilingDate, latestAccession, latestFinancialFilingDate string, filingsCount int, err error) {
	err = s.db.QueryRowContext(ctx, `
SELECT COUNT(*),
       COALESCE(MAX(filing_date), ''),
       COALESCE(MAX(CASE
           WHEN form IN ('10-K', '10-K/A', '10-Q', '10-Q/A', '20-F', '20-F/A', '40-F', '40-F/A', '6-K', '6-K/A')
           THEN filing_date
       END), '')
FROM sec_filings
WHERE cik = ?`, cik).Scan(&filingsCount, &latestFilingDate, &latestFinancialFilingDate)
	if err != nil {
		return "", "", "", 0, fmt.Errorf("read filing metadata for %s: %w", cik, err)
	}
	if filingsCount > 0 {
		err = s.db.QueryRowContext(ctx, `
SELECT accession_number
FROM sec_filings
WHERE cik = ?
ORDER BY filing_date DESC, accession_number DESC
LIMIT 1`, cik).Scan(&latestAccession)
		if err != nil && err != sql.ErrNoRows {
			return "", "", "", 0, fmt.Errorf("read latest accession for %s: %w", cik, err)
		}
	}
	return latestFilingDate, latestAccession, latestFinancialFilingDate, filingsCount, nil
}

// LoadCompanySummaryCache loads a clean cached CompanySummaryRecord for a CIK if schema_version matches and dirty=0.
func (s *Store) LoadCompanySummaryCache(ctx context.Context, cik string, schemaVersion int) (CompanySummaryRecord, bool, error) {
	var rec CompanySummaryRecord
	var hasShares int
	err := s.db.QueryRowContext(ctx, `
SELECT cik, ticker, company_name, sic, currency, latest_period_end, periods_count,
       latest_filing_date, latest_accession, latest_financial_filing_date, filings_count,
       latest_share_count, has_latest_share_count, summary_json, schema_version
FROM sec_company_summaries
WHERE cik = ? AND dirty = 0 AND schema_version = ?`, cik, schemaVersion).Scan(
		&rec.CIK, &rec.Ticker, &rec.CompanyName, &rec.SIC, &rec.Currency,
		&rec.LatestPeriodEnd, &rec.PeriodsCount,
		&rec.LatestFilingDate, &rec.LatestAccession, &rec.LatestFinancialFilingDate, &rec.FilingsCount,
		&rec.LatestShareCount, &hasShares, &rec.SummaryJSON, &rec.SchemaVersion,
	)
	if err == sql.ErrNoRows {
		return CompanySummaryRecord{}, false, nil
	}
	if err != nil {
		return CompanySummaryRecord{}, false, fmt.Errorf("load summary cache for %s: %w", cik, err)
	}
	rec.HasLatestShareCount = hasShares != 0
	return rec, true, nil
}

const upsertCompanySummarySQL = `
INSERT INTO sec_company_summaries (
    cik, ticker, company_name, sic, currency, latest_period_end, periods_count,
    latest_filing_date, latest_accession, latest_financial_filing_date, filings_count,
    latest_share_count, has_latest_share_count, summary_json, schema_version, dirty, computed_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, CURRENT_TIMESTAMP)
ON CONFLICT (cik) DO UPDATE SET
    ticker = excluded.ticker,
    company_name = excluded.company_name,
    sic = excluded.sic,
    currency = excluded.currency,
    latest_period_end = excluded.latest_period_end,
    periods_count = excluded.periods_count,
    latest_filing_date = excluded.latest_filing_date,
    latest_accession = excluded.latest_accession,
    latest_financial_filing_date = excluded.latest_financial_filing_date,
    filings_count = excluded.filings_count,
    latest_share_count = excluded.latest_share_count,
    has_latest_share_count = excluded.has_latest_share_count,
    summary_json = excluded.summary_json,
    schema_version = excluded.schema_version,
    dirty = 0,
    computed_at = CURRENT_TIMESTAMP`

const upsertRankVariableSQL = `
INSERT INTO sec_rank_variables (
    cik, years, ticker, company_name, sic, sector, raw_peer_group, peer_group, score, eligible,
    latest_financial_filing_date, year, trend_years, revenue, currency,
    roic, has_roic, roe, has_roe, operating_margin, operating_consistency, has_operating_margin,
    fcf_margin, owner_earnings_margin, has_owner_earnings, has_sbc_adjustment,
    revenue_cagr, has_revenue_growth, eps_growth, has_eps_growth,
    balance_sheet_quality, has_balance_sheet, book_value_growth, has_book_value_growth,
    share_reduction, has_share_trend, split_adjusted, schema_version, computed_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
ON CONFLICT (cik, years) DO UPDATE SET
    ticker = excluded.ticker,
    company_name = excluded.company_name,
    sic = excluded.sic,
    sector = excluded.sector,
    raw_peer_group = excluded.raw_peer_group,
    peer_group = excluded.peer_group,
    score = excluded.score,
    eligible = excluded.eligible,
    latest_financial_filing_date = excluded.latest_financial_filing_date,
    year = excluded.year,
    trend_years = excluded.trend_years,
    revenue = excluded.revenue,
    currency = excluded.currency,
    roic = excluded.roic,
    has_roic = excluded.has_roic,
    roe = excluded.roe,
    has_roe = excluded.has_roe,
    operating_margin = excluded.operating_margin,
    operating_consistency = excluded.operating_consistency,
    has_operating_margin = excluded.has_operating_margin,
    fcf_margin = excluded.fcf_margin,
    owner_earnings_margin = excluded.owner_earnings_margin,
    has_owner_earnings = excluded.has_owner_earnings,
    has_sbc_adjustment = excluded.has_sbc_adjustment,
    revenue_cagr = excluded.revenue_cagr,
    has_revenue_growth = excluded.has_revenue_growth,
    eps_growth = excluded.eps_growth,
    has_eps_growth = excluded.has_eps_growth,
    balance_sheet_quality = excluded.balance_sheet_quality,
    has_balance_sheet = excluded.has_balance_sheet,
    book_value_growth = excluded.book_value_growth,
    has_book_value_growth = excluded.has_book_value_growth,
    share_reduction = excluded.share_reduction,
    has_share_trend = excluded.has_share_trend,
    split_adjusted = excluded.split_adjusted,
    schema_version = excluded.schema_version,
    computed_at = CURRENT_TIMESTAMP`

const upsertGAAPArbVariableSQL = `
INSERT INTO sec_gaap_arbitrage_variables (
    cik, years, ticker, company_name, sic, sic_description, sector, currency, eligible,
    latest_financial_filing_date, gaap_revenue, gaap_net_income, gaap_operating_income,
    gaap_operating_margin, gaap_roic, has_gaap_roic, gaap_unprofitable,
    intangible_capital, adjusted_ebit, adjusted_ebit_margin, adjusted_invested_cap,
    adjusted_roic, has_adjusted_roic, avg_adjusted_roic, roic_delta, surging_roic,
    owner_earnings, has_owner_earnings, adjusted_owner_earnings, shares_outstanding,
    base_arbitrage_score, base_arbitrage_signal, schema_version, computed_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
ON CONFLICT (cik, years) DO UPDATE SET
    ticker = excluded.ticker,
    company_name = excluded.company_name,
    sic = excluded.sic,
    sic_description = excluded.sic_description,
    sector = excluded.sector,
    currency = excluded.currency,
    eligible = excluded.eligible,
    latest_financial_filing_date = excluded.latest_financial_filing_date,
    gaap_revenue = excluded.gaap_revenue,
    gaap_net_income = excluded.gaap_net_income,
    gaap_operating_income = excluded.gaap_operating_income,
    gaap_operating_margin = excluded.gaap_operating_margin,
    gaap_roic = excluded.gaap_roic,
    has_gaap_roic = excluded.has_gaap_roic,
    gaap_unprofitable = excluded.gaap_unprofitable,
    intangible_capital = excluded.intangible_capital,
    adjusted_ebit = excluded.adjusted_ebit,
    adjusted_ebit_margin = excluded.adjusted_ebit_margin,
    adjusted_invested_cap = excluded.adjusted_invested_cap,
    adjusted_roic = excluded.adjusted_roic,
    has_adjusted_roic = excluded.has_adjusted_roic,
    avg_adjusted_roic = excluded.avg_adjusted_roic,
    roic_delta = excluded.roic_delta,
    surging_roic = excluded.surging_roic,
    owner_earnings = excluded.owner_earnings,
    has_owner_earnings = excluded.has_owner_earnings,
    adjusted_owner_earnings = excluded.adjusted_owner_earnings,
    shares_outstanding = excluded.shares_outstanding,
    base_arbitrage_score = excluded.base_arbitrage_score,
    base_arbitrage_signal = excluded.base_arbitrage_signal,
    schema_version = excluded.schema_version,
    computed_at = CURRENT_TIMESTAMP`

// SaveCompanySummaryCache persists a single company's cached annual summary and marks dirty=0.
func (s *Store) SaveCompanySummaryCache(ctx context.Context, rec CompanySummaryRecord) error {
	_, err := s.db.ExecContext(ctx, upsertCompanySummarySQL,
		rec.CIK, rec.Ticker, rec.CompanyName, rec.SIC, rec.Currency,
		rec.LatestPeriodEnd, rec.PeriodsCount,
		rec.LatestFilingDate, rec.LatestAccession, rec.LatestFinancialFilingDate, rec.FilingsCount,
		rec.LatestShareCount, boolToInt(rec.HasLatestShareCount), rec.SummaryJSON, rec.SchemaVersion,
	)
	if err != nil {
		return fmt.Errorf("save summary cache for %s: %w", rec.CIK, err)
	}
	return nil
}

// SaveCompanyPrecomputedBatch saves summaries, rank variables, and GAAP arbitrage
// variables atomically inside a single transaction.
func (s *Store) SaveCompanyPrecomputedBatch(ctx context.Context, bundles []CompanyPrecomputedBundle) error {
	if len(bundles) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin precompute batch: %w", err)
	}
	defer tx.Rollback()

	sumStmt, err := tx.PrepareContext(ctx, upsertCompanySummarySQL)
	if err != nil {
		return fmt.Errorf("prepare summary upsert: %w", err)
	}
	defer sumStmt.Close()

	rankStmt, err := tx.PrepareContext(ctx, upsertRankVariableSQL)
	if err != nil {
		return fmt.Errorf("prepare rank variable upsert: %w", err)
	}
	defer rankStmt.Close()

	arbStmt, err := tx.PrepareContext(ctx, upsertGAAPArbVariableSQL)
	if err != nil {
		return fmt.Errorf("prepare arbitrage variable upsert: %w", err)
	}
	defer arbStmt.Close()

	for _, b := range bundles {
		if b.Summary != nil {
			rec := b.Summary
			if _, err := sumStmt.ExecContext(ctx,
				rec.CIK, rec.Ticker, rec.CompanyName, rec.SIC, rec.Currency,
				rec.LatestPeriodEnd, rec.PeriodsCount,
				rec.LatestFilingDate, rec.LatestAccession, rec.LatestFinancialFilingDate, rec.FilingsCount,
				rec.LatestShareCount, boolToInt(rec.HasLatestShareCount), rec.SummaryJSON, rec.SchemaVersion,
			); err != nil {
				return fmt.Errorf("upsert summary for %s: %w", rec.CIK, err)
			}
		}
		for _, r := range b.RankVars {
			if _, err := rankStmt.ExecContext(ctx,
				r.CIK, r.Years, r.Ticker, r.CompanyName, r.SIC, r.Sector, r.RawPeerGroup, r.PeerGroup, r.Score, boolToInt(r.Eligible),
				r.LatestFinancialFilingDate, r.Year, r.TrendYears, r.Revenue, r.Currency,
				r.ROIC, boolToInt(r.HasROIC), r.ROE, boolToInt(r.HasROE),
				r.OperatingMargin, r.OperatingConsistency, boolToInt(r.HasOperatingMargin),
				r.FCFMargin, r.OwnerEarningsMargin, boolToInt(r.HasOwnerEarnings), boolToInt(r.HasSBCAdjustment),
				r.RevenueCAGR, boolToInt(r.HasRevenueGrowth), r.EPSGrowth, boolToInt(r.HasEPSGrowth),
				r.BalanceSheetQuality, boolToInt(r.HasBalanceSheet), r.BookValueGrowth, boolToInt(r.HasBookValueGrowth),
				r.ShareReduction, boolToInt(r.HasShareTrend), boolToInt(r.SplitAdjusted), r.SchemaVersion,
			); err != nil {
				return fmt.Errorf("upsert rank variables for %s (%dy): %w", r.CIK, r.Years, err)
			}
		}
		for _, a := range b.ArbVars {
			if _, err := arbStmt.ExecContext(ctx,
				a.CIK, a.Years, a.Ticker, a.CompanyName, a.SIC, a.SICDescription, a.Sector, a.Currency, boolToInt(a.Eligible),
				a.LatestFinancialFilingDate, a.GAAPRevenue, a.GAAPNetIncome, a.GAAPOperatingIncome,
				a.GAAPOperatingMargin, a.GAAPROIC, boolToInt(a.HasGAAPROIC), boolToInt(a.GAAPUnprofitable),
				a.IntangibleCapital, a.AdjustedEBIT, a.AdjustedEBITMargin, a.AdjustedInvestedCap,
				a.AdjustedROIC, boolToInt(a.HasAdjustedROIC), a.AvgAdjustedROIC, a.ROICDelta, boolToInt(a.SurgingROIC),
				a.OwnerEarnings, boolToInt(a.HasOwnerEarnings), a.AdjustedOwnerEarnings, a.SharesOutstanding,
				a.BaseArbitrageScore, a.BaseArbitrageSignal, a.SchemaVersion,
			); err != nil {
				return fmt.Errorf("upsert gaap arbitrage variables for %s (%dy): %w", a.CIK, a.Years, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit precompute batch: %w", err)
	}
	return nil
}

// UpdateRankScoresBatch writes full-universe peer_group and score back to sec_rank_variables.
func (s *Store) UpdateRankScoresBatch(ctx context.Context, years int, schemaVersion int, updates []RankScoreUpdate) error {
	if len(updates) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin rank score update: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
UPDATE sec_rank_variables
SET peer_group = ?, score = ?, computed_at = CURRENT_TIMESTAMP
WHERE cik = ? AND years = ? AND schema_version = ?`)
	if err != nil {
		return fmt.Errorf("prepare rank score update: %w", err)
	}
	defer stmt.Close()

	for _, u := range updates {
		if _, err := stmt.ExecContext(ctx, u.PeerGroup, u.Score, u.CIK, years, schemaVersion); err != nil {
			return fmt.Errorf("update rank score for %s: %w", u.CIK, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit rank score update: %w", err)
	}
	return nil
}

// StaleCompanyCIKs returns unique CIK companies that need precomputation because
// they are missing, dirty, on an older schema_version, or missing any requested
// rank/arbitrage year window.
func (s *Store) StaleCompanyCIKs(
	ctx context.Context,
	summarySchema, rankSchema, arbSchema int,
	rankYears, arbYears []int,
	tickers []string,
	force bool,
) ([]LocalCompany, error) {
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
			return nil, nil
		}
	}

	var queryBuilder strings.Builder
	args := make([]any, 0, 8+len(rankYears)*2+len(arbYears)*2+len(filterSet))

	queryBuilder.WriteString(`
SELECT c.cik,
       c.company_name,
       COALESCE(NULLIF(c.requested_ticker, ''), COALESCE((SELECT MIN(t.ticker) FROM sec_tickers t WHERE t.cik = c.cik), '')),
       c.sic,
       c.sic_description
FROM sec_companies c
LEFT JOIN sec_company_summaries cs ON cs.cik = c.cik
WHERE 1 = 1`)

	if len(filterSet) > 0 && len(filterSet) <= 400 {
		placeholders := strings.TrimRight(strings.Repeat("?,", len(filterSet)), ",")
		queryBuilder.WriteString(" AND c.cik IN (SELECT cik FROM sec_tickers WHERE ticker IN (" + placeholders + "))")
		for t := range filterSet {
			args = append(args, t)
		}
	} else if len(filterSet) > 400 {
		// Resolve large ticker lists via one lightweight table scan of sec_tickers.
		targetCIKs, err := s.ciksForTickers(ctx, filterSet)
		if err != nil {
			return nil, err
		}
		if len(targetCIKs) == 0 {
			return nil, nil
		}
		if !force {
			stale, err := s.StaleCompanyCIKs(ctx, summarySchema, rankSchema, arbSchema, rankYears, arbYears, nil, false)
			if err != nil {
				return nil, err
			}
			out := make([]LocalCompany, 0, len(stale))
			for _, c := range stale {
				if _, ok := targetCIKs[c.CIK]; ok {
					out = append(out, c)
				}
			}
			return out, nil
		}
		all, err := s.StaleCompanyCIKs(ctx, summarySchema, rankSchema, arbSchema, rankYears, arbYears, nil, true)
		if err != nil {
			return nil, err
		}
		out := make([]LocalCompany, 0, len(targetCIKs))
		for _, c := range all {
			if _, ok := targetCIKs[c.CIK]; ok {
				out = append(out, c)
			}
		}
		return out, nil
	}

	if !force {
		var conditions []string
		conditions = append(conditions, "cs.cik IS NULL", "cs.dirty != 0", "cs.schema_version != ?")
		args = append(args, summarySchema)
		for _, y := range rankYears {
			conditions = append(conditions, "NOT EXISTS (SELECT 1 FROM sec_rank_variables rv WHERE rv.cik = c.cik AND rv.years = ? AND rv.schema_version = ?)")
			args = append(args, y, rankSchema)
		}
		for _, y := range arbYears {
			conditions = append(conditions, "NOT EXISTS (SELECT 1 FROM sec_gaap_arbitrage_variables gv WHERE gv.cik = c.cik AND gv.years = ? AND gv.schema_version = ?)")
			args = append(args, y, arbSchema)
		}
		queryBuilder.WriteString(" AND (" + strings.Join(conditions, " OR ") + ")")
	}

	queryBuilder.WriteString(" ORDER BY c.cik")

	rows, err := s.db.QueryContext(ctx, queryBuilder.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("query stale companies: %w", err)
	}
	defer rows.Close()

	var companies []LocalCompany
	for rows.Next() {
		var c LocalCompany
		if err := rows.Scan(&c.CIK, &c.Name, &c.Ticker, &c.SIC, &c.SICDescription); err != nil {
			return nil, fmt.Errorf("scan stale company: %w", err)
		}
		companies = append(companies, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate stale companies: %w", err)
	}
	return companies, nil
}

func (s *Store) ciksForTickers(ctx context.Context, tickerSet map[string]struct{}) (map[string]struct{}, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT cik, ticker FROM sec_tickers`)
	if err != nil {
		return nil, fmt.Errorf("load tickers for filter: %w", err)
	}
	defer rows.Close()
	ciks := make(map[string]struct{})
	for rows.Next() {
		var cik, ticker string
		if err := rows.Scan(&cik, &ticker); err != nil {
			return nil, fmt.Errorf("scan ticker mapping: %w", err)
		}
		if _, ok := tickerSet[strings.ToUpper(strings.TrimSpace(ticker))]; ok {
			ciks[cik] = struct{}{}
		}
	}
	return ciks, rows.Err()
}

// LoadRankVariables loads clean precomputed rank variables joined with sec_tickers
// so multi-ticker CIKs behave identically to scanning s.Companies().
func (s *Store) LoadRankVariables(ctx context.Context, years int, rankSchema int, tickers []string) ([]RankVariableRecord, error) {
	var tickerSet map[string]struct{}
	if len(tickers) > 0 {
		tickerSet = make(map[string]struct{}, len(tickers))
		for _, t := range tickers {
			clean := strings.ToUpper(strings.TrimSpace(t))
			if clean != "" {
				tickerSet[clean] = struct{}{}
			}
		}
		if len(tickerSet) == 0 {
			return nil, nil
		}
	}

	var queryBuilder strings.Builder
	args := make([]any, 0, 2+len(tickerSet))
	queryBuilder.WriteString(`
SELECT rv.cik, rv.years, t.ticker, rv.company_name, rv.sic, rv.sector,
       rv.raw_peer_group, rv.peer_group, rv.score, rv.eligible,
       rv.latest_financial_filing_date, rv.year, rv.trend_years, rv.revenue, rv.currency,
       rv.roic, rv.has_roic, rv.roe, rv.has_roe,
       rv.operating_margin, rv.operating_consistency, rv.has_operating_margin,
       rv.fcf_margin, rv.owner_earnings_margin, rv.has_owner_earnings, rv.has_sbc_adjustment,
       rv.revenue_cagr, rv.has_revenue_growth, rv.eps_growth, rv.has_eps_growth,
       rv.balance_sheet_quality, rv.has_balance_sheet,
       rv.book_value_growth, rv.has_book_value_growth,
       rv.share_reduction, rv.has_share_trend, rv.split_adjusted,
       rv.schema_version
FROM sec_rank_variables rv
JOIN sec_tickers t ON t.cik = rv.cik
JOIN sec_company_summaries cs ON cs.cik = rv.cik
WHERE rv.years = ? AND rv.schema_version = ? AND cs.dirty = 0`)
	args = append(args, years, rankSchema)

	useSQLTickerFilter := len(tickerSet) > 0 && len(tickerSet) <= 400
	if useSQLTickerFilter {
		placeholders := strings.TrimRight(strings.Repeat("?,", len(tickerSet)), ",")
		queryBuilder.WriteString(" AND t.ticker IN (" + placeholders + ")")
		for t := range tickerSet {
			args = append(args, t)
		}
	}
	queryBuilder.WriteString(" ORDER BY t.ticker")

	rows, err := s.db.QueryContext(ctx, queryBuilder.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("load rank variables: %w", err)
	}
	defer rows.Close()

	var result []RankVariableRecord
	for rows.Next() {
		var r RankVariableRecord
		var eligible, hasROIC, hasROE, hasOpMargin, hasOwner, hasSBC int
		var hasRevGrowth, hasEPSGrowth, hasBal, hasBook, hasShare, splitAdj int
		if err := rows.Scan(
			&r.CIK, &r.Years, &r.Ticker, &r.CompanyName, &r.SIC, &r.Sector,
			&r.RawPeerGroup, &r.PeerGroup, &r.Score, &eligible,
			&r.LatestFinancialFilingDate, &r.Year, &r.TrendYears, &r.Revenue, &r.Currency,
			&r.ROIC, &hasROIC, &r.ROE, &hasROE,
			&r.OperatingMargin, &r.OperatingConsistency, &hasOpMargin,
			&r.FCFMargin, &r.OwnerEarningsMargin, &hasOwner, &hasSBC,
			&r.RevenueCAGR, &hasRevGrowth, &r.EPSGrowth, &hasEPSGrowth,
			&r.BalanceSheetQuality, &hasBal,
			&r.BookValueGrowth, &hasBook,
			&r.ShareReduction, &hasShare, &splitAdj,
			&r.SchemaVersion,
		); err != nil {
			return nil, fmt.Errorf("scan rank variable: %w", err)
		}
		if len(tickerSet) > 0 && !useSQLTickerFilter {
			if _, ok := tickerSet[strings.ToUpper(r.Ticker)]; !ok {
				continue
			}
		}
		r.Eligible = eligible != 0
		r.HasROIC = hasROIC != 0
		r.HasROE = hasROE != 0
		r.HasOperatingMargin = hasOpMargin != 0
		r.HasOwnerEarnings = hasOwner != 0
		r.HasSBCAdjustment = hasSBC != 0
		r.HasRevenueGrowth = hasRevGrowth != 0
		r.HasEPSGrowth = hasEPSGrowth != 0
		r.HasBalanceSheet = hasBal != 0
		r.HasBookValueGrowth = hasBook != 0
		r.HasShareTrend = hasShare != 0
		r.SplitAdjusted = splitAdj != 0
		result = append(result, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rank variables: %w", err)
	}
	return result, nil
}

// LoadGAAPArbitrageVariables loads clean precomputed GAAP arbitrage variables joined with sec_tickers.
func (s *Store) LoadGAAPArbitrageVariables(ctx context.Context, years int, arbSchema int, tickers []string) ([]GAAPArbitrageVariableRecord, error) {
	var tickerSet map[string]struct{}
	if len(tickers) > 0 {
		tickerSet = make(map[string]struct{}, len(tickers))
		for _, t := range tickers {
			clean := strings.ToUpper(strings.TrimSpace(t))
			if clean != "" {
				tickerSet[clean] = struct{}{}
			}
		}
		if len(tickerSet) == 0 {
			return nil, nil
		}
	}

	var queryBuilder strings.Builder
	args := make([]any, 0, 2+len(tickerSet))
	queryBuilder.WriteString(`
SELECT gv.cik, gv.years, t.ticker, gv.company_name, gv.sic, gv.sic_description, gv.sector, gv.currency, gv.eligible,
       gv.latest_financial_filing_date, gv.gaap_revenue, gv.gaap_net_income, gv.gaap_operating_income,
       gv.gaap_operating_margin, gv.gaap_roic, gv.has_gaap_roic, gv.gaap_unprofitable,
       gv.intangible_capital, gv.adjusted_ebit, gv.adjusted_ebit_margin, gv.adjusted_invested_cap,
       gv.adjusted_roic, gv.has_adjusted_roic, gv.avg_adjusted_roic, gv.roic_delta, gv.surging_roic,
       gv.owner_earnings, gv.has_owner_earnings, gv.adjusted_owner_earnings, gv.shares_outstanding,
       gv.base_arbitrage_score, gv.base_arbitrage_signal, gv.schema_version
FROM sec_gaap_arbitrage_variables gv
JOIN sec_tickers t ON t.cik = gv.cik
JOIN sec_company_summaries cs ON cs.cik = gv.cik
WHERE gv.years = ? AND gv.schema_version = ? AND cs.dirty = 0`)
	args = append(args, years, arbSchema)

	useSQLTickerFilter := len(tickerSet) > 0 && len(tickerSet) <= 400
	if useSQLTickerFilter {
		placeholders := strings.TrimRight(strings.Repeat("?,", len(tickerSet)), ",")
		queryBuilder.WriteString(" AND t.ticker IN (" + placeholders + ")")
		for t := range tickerSet {
			args = append(args, t)
		}
	}
	queryBuilder.WriteString(" ORDER BY t.ticker")

	rows, err := s.db.QueryContext(ctx, queryBuilder.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("load gaap arbitrage variables: %w", err)
	}
	defer rows.Close()

	var result []GAAPArbitrageVariableRecord
	for rows.Next() {
		var a GAAPArbitrageVariableRecord
		var eligible, hasGAAPROIC, gaapUnprof, hasAdjROIC, surging, hasOwner int
		if err := rows.Scan(
			&a.CIK, &a.Years, &a.Ticker, &a.CompanyName, &a.SIC, &a.SICDescription, &a.Sector, &a.Currency, &eligible,
			&a.LatestFinancialFilingDate, &a.GAAPRevenue, &a.GAAPNetIncome, &a.GAAPOperatingIncome,
			&a.GAAPOperatingMargin, &a.GAAPROIC, &hasGAAPROIC, &gaapUnprof,
			&a.IntangibleCapital, &a.AdjustedEBIT, &a.AdjustedEBITMargin, &a.AdjustedInvestedCap,
			&a.AdjustedROIC, &hasAdjROIC, &a.AvgAdjustedROIC, &a.ROICDelta, &surging,
			&a.OwnerEarnings, &hasOwner, &a.AdjustedOwnerEarnings, &a.SharesOutstanding,
			&a.BaseArbitrageScore, &a.BaseArbitrageSignal, &a.SchemaVersion,
		); err != nil {
			return nil, fmt.Errorf("scan gaap arbitrage variable: %w", err)
		}
		if len(tickerSet) > 0 && !useSQLTickerFilter {
			if _, ok := tickerSet[strings.ToUpper(a.Ticker)]; !ok {
				continue
			}
		}
		a.Eligible = eligible != 0
		a.HasGAAPROIC = hasGAAPROIC != 0
		a.GAAPUnprofitable = gaapUnprof != 0
		a.HasAdjustedROIC = hasAdjROIC != 0
		a.SurgingROIC = surging != 0
		a.HasOwnerEarnings = hasOwner != 0
		result = append(result, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate gaap arbitrage variables: %w", err)
	}
	return result, nil
}

// PrecomputeStats returns counts of total, clean, dirty, missing summaries and precomputed variable rows.
func (s *Store) PrecomputeStats(ctx context.Context, summarySchema, rankSchema, arbSchema int) (PrecomputeStats, error) {
	var stats PrecomputeStats
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sec_companies`).Scan(&stats.TotalCompanies); err != nil {
		return stats, fmt.Errorf("count sec_companies: %w", err)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sec_company_summaries WHERE dirty = 0 AND schema_version = ?`, summarySchema).Scan(&stats.CleanSummaries); err != nil {
		return stats, fmt.Errorf("count clean summaries: %w", err)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sec_company_summaries WHERE dirty != 0 OR schema_version != ?`, summarySchema).Scan(&stats.DirtySummaries); err != nil {
		return stats, fmt.Errorf("count dirty summaries: %w", err)
	}
	stats.MissingSummaries = stats.TotalCompanies - stats.CleanSummaries - stats.DirtySummaries
	if stats.MissingSummaries < 0 {
		stats.MissingSummaries = 0
	}
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sec_rank_variables WHERE years = 5 AND schema_version = ?`, rankSchema).Scan(&stats.Rank5Count)
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sec_rank_variables WHERE years = 6 AND schema_version = ?`, rankSchema).Scan(&stats.Rank6Count)
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sec_gaap_arbitrage_variables WHERE years = 5 AND schema_version = ?`, arbSchema).Scan(&stats.Arb5Count)
	return stats, nil
}
