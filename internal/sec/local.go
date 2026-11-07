package sec

import (
	"context"
	"fmt"
)

type LocalCompany struct {
	CIK            string
	Name           string
	Ticker         string
	SIC            string
	SICDescription string
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

func ListCompanies(ctx context.Context, databasePath string) ([]LocalCompany, error) {
	store, err := OpenStore(ctx, databasePath)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.Companies(ctx)
}
