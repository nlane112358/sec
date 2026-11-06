package sec

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type LocalCompany struct {
	CIK    string
	Name   string
	Ticker string
}

type MigrationReport struct {
	Databases int
	Companies int
	Facts     int
	Filings   int
}

func (s *Store) Companies(ctx context.Context) ([]LocalCompany, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT c.cik, c.company_name, t.ticker
FROM sec_companies c JOIN sec_tickers t ON t.cik = c.cik
ORDER BY t.ticker`)
	if err != nil {
		return nil, fmt.Errorf("load saved companies: %w", err)
	}
	defer rows.Close()
	companies := make([]LocalCompany, 0)
	for rows.Next() {
		var company LocalCompany
		if err := rows.Scan(&company.CIK, &company.Name, &company.Ticker); err != nil {
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

// MigrateCompanyDatabases merges existing per-ticker databases into the
// shared database. The source databases remain intact so migration is safe to
// rerun if interrupted.
func MigrateCompanyDatabases(ctx context.Context, dataDir, sharedPath string) (MigrationReport, error) {
	var report MigrationReport
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return report, nil
		}
		return report, err
	}
	sharedAbs, err := filepath.Abs(sharedPath)
	if err != nil {
		return report, err
	}
	shared, err := OpenStore(ctx, sharedPath)
	if err != nil {
		return report, err
	}
	defer shared.Close()

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".db") {
			continue
		}
		sourcePath := filepath.Join(dataDir, entry.Name())
		sourceAbs, err := filepath.Abs(sourcePath)
		if err != nil {
			return report, err
		}
		if sourceAbs == sharedAbs {
			continue
		}
		if err := ctx.Err(); err != nil {
			return report, err
		}
		source, err := OpenStore(ctx, sourcePath)
		if err != nil {
			return report, fmt.Errorf("open source database %s: %w", entry.Name(), err)
		}
		companies, err := source.Companies(ctx)
		if err != nil {
			source.Close()
			return report, fmt.Errorf("read source database %s: %w", entry.Name(), err)
		}
		for _, company := range companies {
			if err := shared.SaveCompany(ctx, company.CIK, company.Name, company.Ticker); err != nil {
				source.Close()
				return report, err
			}
			facts, err := source.LoadFacts(ctx, company.Ticker)
			if err != nil {
				source.Close()
				return report, err
			}
			filings, err := source.LoadFilings(ctx, company.Ticker)
			if err != nil {
				source.Close()
				return report, err
			}
			if err := shared.SaveFacts(ctx, company.Ticker, facts); err != nil {
				source.Close()
				return report, err
			}
			if err := shared.SaveFilings(ctx, filings); err != nil {
				source.Close()
				return report, err
			}
			report.Companies++
			report.Facts += len(facts)
			report.Filings += len(filings)
		}
		if err := source.Close(); err != nil {
			return report, err
		}
		if len(companies) > 0 {
			report.Databases++
		}
	}
	return report, nil
}
