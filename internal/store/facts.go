package store

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"
)

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func (s *Store) SaveCompany(ctx context.Context, cik, name, ticker string, metadata ...string) error {
	sic, sicDescription := "", ""
	if len(metadata) > 0 {
		sic = strings.TrimSpace(metadata[0])
	}
	if len(metadata) > 1 {
		sicDescription = strings.TrimSpace(metadata[1])
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin SEC company transaction: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO sec_companies (cik, company_name, requested_ticker, sic, sic_description)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT (cik) DO UPDATE SET company_name = excluded.company_name,
	requested_ticker = excluded.requested_ticker,
	sic = CASE WHEN excluded.sic <> '' THEN excluded.sic ELSE sec_companies.sic END,
	sic_description = CASE WHEN excluded.sic_description <> '' THEN excluded.sic_description ELSE sec_companies.sic_description END,
	updated_at = CURRENT_TIMESTAMP`, cik, name, ticker, sic, sicDescription); err != nil {
		return fmt.Errorf("save SEC company: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO sec_tickers (cik, ticker) VALUES (?, ?)
ON CONFLICT (ticker) DO UPDATE SET cik = excluded.cik`, cik, ticker); err != nil {
		return fmt.Errorf("save SEC ticker mapping: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit SEC company: %w", err)
	}
	return nil
}

func (s *Store) SaveFacts(ctx context.Context, ticker string, facts []FinancialFact) error {
	if len(facts) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin SEC data transaction: %w", err)
	}
	defer tx.Rollback()

	statement, err := tx.PrepareContext(ctx, `INSERT INTO sec_facts (
    cik, company_name, ticker, taxonomy, concept, label, unit, value,
    period_start, period_end, filing_date, form, accession_number, frame
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (cik, taxonomy, concept, unit, accession_number, period_start, period_end, form, filing_date, frame)
DO UPDATE SET company_name = excluded.company_name, ticker = excluded.ticker,
    label = excluded.label, value = excluded.value`)
	if err != nil {
		return fmt.Errorf("prepare SEC fact insert: %w", err)
	}
	defer statement.Close()

	for _, fact := range facts {
		if _, err := statement.ExecContext(ctx,
			fact.CIK, fact.Company, ticker, fact.Taxonomy, fact.Concept, fact.Label, fact.Unit,
			fact.Value, fact.Start, fact.End, fact.FilingDate, fact.Form,
			fact.Accession, fact.Frame,
		); err != nil {
			return fmt.Errorf("save SEC fact %s (%s): %w", fact.Concept, fact.End, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit SEC data: %w", err)
	}
	return nil
}

func (s *Store) SaveFilings(ctx context.Context, filings []Filing) error {
	if len(filings) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin filing transaction: %w", err)
	}
	defer tx.Rollback()

	statement, err := tx.PrepareContext(ctx, `INSERT INTO sec_filings (
    cik, accession_number, form, filing_date, report_date, primary_document, document_title
) VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (cik, accession_number) DO UPDATE SET
    form = excluded.form, filing_date = excluded.filing_date, report_date = excluded.report_date,
    primary_document = excluded.primary_document, document_title = excluded.document_title`)
	if err != nil {
		return fmt.Errorf("prepare filing insert: %w", err)
	}
	defer statement.Close()
	for _, filing := range filings {
		if _, err := statement.ExecContext(ctx, filing.CIK, filing.Accession, filing.Form,
			filing.FilingDate, filing.ReportDate, filing.PrimaryDocument, filing.DocumentTitle); err != nil {
			return fmt.Errorf("save SEC filing %s: %w", filing.Accession, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit filing data: %w", err)
	}
	return nil
}

func (s *Store) LoadCompany(ctx context.Context, ticker ...string) (string, string, error) {
	var name, savedTicker string
	var err error
	if len(ticker) > 0 && strings.TrimSpace(ticker[0]) != "" {
		err = s.db.QueryRowContext(ctx, `SELECT c.company_name, t.ticker
FROM sec_tickers t JOIN sec_companies c ON c.cik = t.cik
WHERE t.ticker = ? COLLATE NOCASE LIMIT 1`, ticker[0]).Scan(&name, &savedTicker)
	} else {
		err = s.db.QueryRowContext(ctx, `SELECT c.company_name, t.ticker
FROM sec_tickers t JOIN sec_companies c ON c.cik = t.cik
ORDER BY t.ticker LIMIT 1`).Scan(&name, &savedTicker)
	}
	if err != nil {
		return "", "", fmt.Errorf("load company: %w", err)
	}
	return name, savedTicker, nil
}

func (s *Store) CompanyForTicker(ctx context.Context, ticker string) (string, string, string, error) {
	var cik, name, savedTicker string
	err := s.db.QueryRowContext(ctx, `SELECT c.cik, c.company_name, t.ticker
FROM sec_tickers t JOIN sec_companies c ON c.cik = t.cik
WHERE t.ticker = ? COLLATE NOCASE LIMIT 1`, ticker).Scan(&cik, &name, &savedTicker)
	if err != nil {
		return "", "", "", fmt.Errorf("load company %s: %w", ticker, err)
	}
	return cik, name, savedTicker, nil
}

func (s *Store) LoadFilings(ctx context.Context, ticker ...string) ([]Filing, error) {
	query := `SELECT cik, accession_number, form, filing_date, report_date, primary_document, document_title
FROM sec_filings ORDER BY filing_date DESC, accession_number DESC`
	args := []any{}
	if len(ticker) > 0 && strings.TrimSpace(ticker[0]) != "" {
		cik, _, _, err := s.CompanyForTicker(ctx, ticker[0])
		if err != nil {
			return nil, err
		}
		query = `SELECT cik, accession_number, form, filing_date, report_date, primary_document, document_title
FROM sec_filings WHERE cik = ? ORDER BY filing_date DESC, accession_number DESC`
		args = append(args, cik)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("load SEC filings: %w", err)
	}
	defer rows.Close()
	filings := make([]Filing, 0)
	for rows.Next() {
		var filing Filing
		if err := rows.Scan(&filing.CIK, &filing.Accession, &filing.Form, &filing.FilingDate,
			&filing.ReportDate, &filing.PrimaryDocument, &filing.DocumentTitle); err != nil {
			return nil, fmt.Errorf("read SEC filing: %w", err)
		}
		filings = append(filings, filing)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read SEC filings: %w", err)
	}
	return filings, nil
}

func (s *Store) LoadFacts(ctx context.Context, ticker ...string) ([]FinancialFact, error) {
	query := `SELECT company_name, cik, ticker, taxonomy, concept,
    label, unit, value, period_start, period_end, filing_date, form, accession_number, frame
FROM sec_facts ORDER BY ticker, period_end DESC, taxonomy, concept, filing_date DESC`
	args := []any{}
	if len(ticker) > 0 && strings.TrimSpace(ticker[0]) != "" {
		cik, _, _, err := s.CompanyForTicker(ctx, ticker[0])
		if err != nil {
			return nil, err
		}
		query = `SELECT company_name, cik, ticker, taxonomy, concept,
    label, unit, value, period_start, period_end, filing_date, form, accession_number, frame
FROM sec_facts WHERE cik = ? ORDER BY period_end DESC, taxonomy, concept, filing_date DESC`
		args = append(args, cik)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("load SEC facts: %w", err)
	}
	defer rows.Close()
	facts := make([]FinancialFact, 0)
	for rows.Next() {
		var fact FinancialFact
		if err := rows.Scan(&fact.Company, &fact.CIK, &fact.Ticker, &fact.Taxonomy, &fact.Concept,
			&fact.Label, &fact.Unit, &fact.Value, &fact.Start, &fact.End, &fact.FilingDate,
			&fact.Form, &fact.Accession, &fact.Frame); err != nil {
			return nil, fmt.Errorf("read SEC fact: %w", err)
		}
		if len(ticker) > 0 && strings.TrimSpace(ticker[0]) != "" {
			fact.Ticker = ticker[0]
		}
		facts = append(facts, fact)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read SEC facts: %w", err)
	}
	return facts, nil
}

func (s *Store) LoadFiling(ctx context.Context, accession string) (Filing, error) {
	var filing Filing
	err := s.db.QueryRowContext(ctx, `SELECT cik, accession_number, form, filing_date, report_date, primary_document, document_title
FROM sec_filings WHERE accession_number = ?`, accession).Scan(&filing.CIK, &filing.Accession,
		&filing.Form, &filing.FilingDate, &filing.ReportDate, &filing.PrimaryDocument, &filing.DocumentTitle)
	if err != nil {
		return Filing{}, fmt.Errorf("load SEC filing %s: %w", accession, err)
	}
	return filing, nil
}

func (s *Store) Companies(ctx context.Context) ([]LocalCompany, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT c.cik, c.company_name, t.ticker, c.sic, c.sic_description
FROM sec_companies c JOIN sec_tickers t ON t.cik = c.cik
ORDER BY t.ticker`)
	if err != nil {
		return nil, fmt.Errorf("load saved companies: %w", err)
	}
	defer rows.Close()
	companies := make([]LocalCompany, 0)
	for rows.Next() {
		var company LocalCompany
		if err := rows.Scan(&company.CIK, &company.Name, &company.Ticker, &company.SIC, &company.SICDescription); err != nil {
			return nil, fmt.Errorf("read saved company: %w", err)
		}
		companies = append(companies, company)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read saved companies: %w", err)
	}
	return companies, nil
}

func (s *Store) LatestShareCount(ctx context.Context, ticker string) (float64, bool) {
	cik, _, _, err := s.CompanyForTicker(ctx, ticker)
	if err != nil {
		return 0, false
	}
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

func (s *Store) HasRecentFinancialFiling(ctx context.Context, ticker string, cutoff time.Time) (bool, error) {
	cik, _, _, err := s.CompanyForTicker(ctx, ticker)
	if err != nil {
		return false, err
	}
	var exists int
	err = s.db.QueryRowContext(ctx, `SELECT EXISTS(
    SELECT 1 FROM sec_filings
    WHERE cik = ? AND filing_date >= ?
      AND form IN ('10-K', '10-K/A', '10-Q', '10-Q/A', '20-F', '20-F/A', '40-F', '40-F/A', '6-K', '6-K/A')
)`, cik, cutoff.Format("2006-01-02")).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check recent filing for %s: %w", ticker, err)
	}
	return exists != 0, nil
}
