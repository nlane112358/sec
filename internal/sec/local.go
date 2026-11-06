package sec

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"sec/internal/sqlite"
)

type LocalCompany struct {
	CIK          string
	Name         string
	Ticker       string
	DatabasePath string
}

// ListCompanies returns companies with a saved SEC database in dataDir.
func ListCompanies(ctx context.Context, dataDir string) ([]LocalCompany, error) {
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []LocalCompany{}, nil
		}
		return nil, err
	}
	companies := make([]LocalCompany, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".db") {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		path := filepath.Join(dataDir, entry.Name())
		db, err := sqlite.Open(path)
		if err != nil {
			continue
		}
		var company LocalCompany
		company.DatabasePath = path
		err = db.QueryRowContext(ctx, `SELECT cik, company_name, requested_ticker
FROM sec_companies LIMIT 1`).Scan(&company.CIK, &company.Name, &company.Ticker)
		closeErr := db.Close()
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) || ctx.Err() != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				continue
			}
			// Ignore unrelated SQLite files that happen to share the directory.
			continue
		}
		if closeErr != nil {
			return nil, closeErr
		}
		companies = append(companies, company)
	}
	sort.Slice(companies, func(i, j int) bool {
		if companies[i].Ticker != companies[j].Ticker {
			return companies[i].Ticker < companies[j].Ticker
		}
		return companies[i].CIK < companies[j].CIK
	})
	return companies, nil
}
